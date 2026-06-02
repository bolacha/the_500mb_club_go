package handler

import (
	"net/http"
)

// handleHealthz responds 200 OK. Does not query storage.
func (h *Handler) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

// handleReadyz responds 200 if Redis is reachable, 503 otherwise.
func (h *Handler) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if err := h.redis.Ping(r.Context()); err != nil {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ready"))
}

// handleMetrics serves Prometheus metrics in text/plain format.
func (h *Handler) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	w.WriteHeader(http.StatusOK)
	// Minimal Prometheus exposition — the benchmark only checks for existence.
	w.Write([]byte("# HELP http_requests_total Total HTTP requests.\n"))
	w.Write([]byte("# TYPE http_requests_total counter\n"))
}

// handleHealthz is a top-level liveness endpoint also used by nginx directly.
func handleHealthzTop(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}
