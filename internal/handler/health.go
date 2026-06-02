package handler

import (
	"net/http"
)

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

// handleHealthzTop is the liveness endpoint. Does not query storage.
func handleHealthzTop(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}
