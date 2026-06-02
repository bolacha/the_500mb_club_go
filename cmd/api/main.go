package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/bolacha/the_500mb_club_go/internal/handler"
	"github.com/bolacha/the_500mb_club_go/internal/middleware"
	"github.com/bolacha/the_500mb_club_go/internal/redis"
)

func main() {
	// ── configuration ──────────────────────────────────
	instanceID := os.Getenv("INSTANCE_ID")
	redisAddr := os.Getenv("REDIS_ADDR")
	port := os.Getenv("PORT")

	if instanceID == "" {
		instanceID = "api-dev"
	}
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	if port == "" {
		port = "8000"
	}

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// ── GC tuning ──────────────────────────────────────
	// Cap memory at 70 MiB per instance. Disable automatic GC; we trigger manually.
	gcInterval := 5 * time.Second
	debug.SetMemoryLimit(70 << 20) // 70 MiB
	debug.SetGCPercent(-1)         // disable automatic GC

	go func() {
		ticker := time.NewTicker(gcInterval)
		defer ticker.Stop()
		for range ticker.C {
			runtime.GC()
			debug.FreeOSMemory() // return unused memory to the OS
		}
	}()

	logger.Info("starting",
		"instance", instanceID,
		"redis", redisAddr,
		"port", port,
		"gomemlimit", "70MiB",
		"gogc", "off",
	)

	// ── Redis client ───────────────────────────────────
	redisClient, err := redis.NewClient(redisAddr)
	if err != nil {
		logger.Error("redis connect failed", "err", err)
		os.Exit(1)
	}
	defer redisClient.Close()

	// ── HTTP server ────────────────────────────────────
	h := handler.New(redisClient, logger)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Apply middleware: InstanceID (required) + request logging.
	var srv http.Handler = mux
	srv = middleware.InstanceID(instanceID)(srv)
	srv = middleware.RequestLogger(logger)(srv)

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      srv,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
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

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", "err", err)
	}
	logger.Info("stopped")
}
