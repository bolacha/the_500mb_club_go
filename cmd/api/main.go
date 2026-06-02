package main

import (
	"cmp"
	"context"
	"encoding/binary"
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/bolacha/the_500mb_club_go/internal/handler"
	"github.com/bolacha/the_500mb_club_go/internal/middleware"
	"github.com/bolacha/the_500mb_club_go/internal/redis"
)

func main() {
	// ── configuration ──────────────────────────────────
	instanceID := cmp.Or(os.Getenv("INSTANCE_ID"), "api-dev")
	redisAddr := cmp.Or(os.Getenv("REDIS_ADDR"), "localhost:6379")
	port := cmp.Or(os.Getenv("PORT"), "8000")
	gomemlimit := cmp.Or(os.Getenv("GOMEMLIMIT"), "70MiB")
	gomaxprocs := cmp.Or(os.Getenv("GOMAXPROCS"), "0")

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// ── CPU alignment ─────────────────────────────────
	// Match GOMAXPROCS to the container's CPU quota to avoid CFS throttling.
	if p, err := strconv.Atoi(gomaxprocs); err == nil && p > 0 {
		runtime.GOMAXPROCS(p)
	}

	// ── GC tuning ──────────────────────────────────────
	// GOMEMLIMIT caps heap; GOGC=25 triggers frequent small GCs.
	// More responsive than GOGC=-1 + manual GC, less tail latency variance.
	memLimit := parseMemLimit(gomemlimit)
	debug.SetMemoryLimit(memLimit)
	debug.SetGCPercent(25)

	logger.Info("starting",
		"instance", instanceID,
		"redis", redisAddr,
		"port", port,
		"gomemlimit", gomemlimit,
		"gomaxprocs", gomaxprocs,
		"gogc", "25",
	)

	// ── Redis client ───────────────────────────────────
	redisClient, err := redis.NewClient(redisAddr)
	if err != nil {
		logger.Error("redis connect failed", "err", err)
		os.Exit(1)
	}
	defer redisClient.Close()

	// ── Redis warm-up ──────────────────────────────────
	// Cycle through all pool connections with PINGs to verify
	// each is active and complete TCP handshake / buffer setup.
	warmCtx := context.Background()
	for i := range 16 {
		if err := redisClient.Ping(warmCtx); err != nil {
			logger.Warn("redis warm-up ping failed", "attempt", i, "err", err)
		}
	}

	// Pre-populate a warm-up key to prime Redis's sorted-set
	// skiplist allocator and response buffers. Query it once
	// via ZREVRANGE to warm the read path.
	warmKey := "telemetry:warmup"
	for i := range 256 {
		score := int64((255 - i) * 100)
		member := make([]byte, 56)
		binary.LittleEndian.PutUint64(member[0:8], uint64(score))
		_ = redisClient.ZADD(warmCtx, warmKey, score, member)
	}
	_, _ = redisClient.ZREVRANGE(warmCtx, warmKey, 0, 255)

	logger.Info("redis warm-up complete", "pings", 16)

	// ── HTTP server ────────────────────────────────────
	h := handler.New(redisClient, logger)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Wire pprof handlers for profiling (not exposed through LB, direct access only).
	mux.Handle("/debug/", http.DefaultServeMux)
	var srv http.Handler = mux
	srv = middleware.InstanceID(instanceID)(srv)
	srv = middleware.RequestLogger(logger)(srv)

	server := &http.Server{
		Addr:           ":" + port,
		Handler:        srv,
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   10 * time.Second,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 4096,
	}

	// ── graceful shutdown ──────────────────────────────
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("listening", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down...")

	// Flush any buffered writes before stopping.
	h.FlushBuffer()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", "err", err)
	}
	logger.Info("stopped")
}

// parseMemLimit converts a string like "70MiB" to bytes.
func parseMemLimit(s string) int64 {
	s = strings.TrimSuffix(s, "iB")
	s = strings.TrimSuffix(s, "ib")
	s = strings.TrimSuffix(s, "B")
	s = strings.TrimSuffix(s, "b")
	mult := int64(1)
	switch {
	case strings.HasSuffix(s, "Mi"):
		mult = 1 << 20
		s = s[:len(s)-2]
	case strings.HasSuffix(s, "Gi"):
		mult = 1 << 30
		s = s[:len(s)-2]
	case strings.HasSuffix(s, "Ki"):
		mult = 1 << 10
		s = s[:len(s)-2]
	case strings.HasSuffix(s, "M"):
		mult = 1 << 20
		s = s[:len(s)-1]
	case strings.HasSuffix(s, "G"):
		mult = 1 << 30
		s = s[:len(s)-1]
	case strings.HasSuffix(s, "K"):
		mult = 1 << 10
		s = s[:len(s)-1]
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 70 << 20 // default 70 MiB
	}
	return n * mult
}
