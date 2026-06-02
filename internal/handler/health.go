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

// handleHealthzTop is a top-level liveness endpoint (used directly, bypasses the Handler struct).
func handleHealthzTop(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}
