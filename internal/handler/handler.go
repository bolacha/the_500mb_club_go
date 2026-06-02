// Package handler implements the HTTP API handlers for the 500MB Club challenge.
// Uses Go 1.22+ enhanced ServeMux for method-based routing with path parameters.
package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"

	"github.com/bolacha/the_500mb_club_go/internal/redis"
	"github.com/bolacha/the_500mb_club_go/internal/telemetry"
)

// Handler holds all dependencies for HTTP handlers.
type Handler struct {
	redis        *redis.Client
	store        *telemetry.Store
	writeBuf     *telemetry.WriteBuffer
	logger       *slog.Logger
	postCount    atomic.Int64
	batchCount   atomic.Int64
	queryCount   atomic.Int64
	anomalyCount atomic.Int64
}

// New creates a new Handler with the given Redis client.
func New(redisClient *redis.Client, logger *slog.Logger) *Handler {
	store := telemetry.NewStore(redisClient)
	return &Handler{
		redis:    redisClient,
		store:    store,
		writeBuf: telemetry.NewWriteBuffer(store),
		logger:   logger,
	}
}

// FlushBuffer drains any buffered writes. Call during graceful shutdown.
func (h *Handler) FlushBuffer() {
	h.writeBuf.Close()
}

// writeJSON encodes v to w using a pooled buffer.
func writeJSON(w http.ResponseWriter, v any) {
	buf := jsonBufPool.Get().(*bytes.Buffer)
	buf.Reset()
	json.NewEncoder(buf).Encode(v)
	w.Write(buf.Bytes())
	jsonBufPool.Put(buf)
}

// RegisterRoutes sets up all routes on the given ServeMux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", handleHealthzTop)

	// Device telemetry endpoints.
	mux.HandleFunc("POST /devices/{id}/telemetry", h.handlePostSingle)
	mux.HandleFunc("POST /devices/{id}/telemetry/batch", h.handlePostBatch)
	mux.HandleFunc("GET /devices/{id}/telemetry", h.handleQuery)
	mux.HandleFunc("GET /devices/{id}/anomaly", h.handleAnomaly)

	// Operational endpoints.
	mux.HandleFunc("GET /readyz", h.handleReadyz)
	mux.HandleFunc("GET /metrics", h.handleMetrics)
}

// handleMetrics serves Prometheus metrics in text/plain format.
func (h *Handler) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	w.WriteHeader(http.StatusOK)

	fmt.Fprintf(w, "# HELP http_requests_total Total HTTP requests by operation.\n")
	fmt.Fprintf(w, "# TYPE http_requests_total counter\n")
	fmt.Fprintf(w, "http_requests_total{op=\"post\"} %d\n", h.postCount.Load())
	fmt.Fprintf(w, "http_requests_total{op=\"batch\"} %d\n", h.batchCount.Load())
	fmt.Fprintf(w, "http_requests_total{op=\"range\"} %d\n", h.queryCount.Load())
	fmt.Fprintf(w, "http_requests_total{op=\"anomaly\"} %d\n", h.anomalyCount.Load())
}
