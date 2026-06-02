// Package middleware provides HTTP middleware for the API.
package middleware

import (
	"log/slog"
	"net/http"
	"time"
)

// InstanceID injects the X-Instance-Id header into every response.
// The challenge requires this header on all upstream responses.
func InstanceID(id string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Instance-Id", id)
			next.ServeHTTP(w, r)
		})
	}
}

// RequestLogger logs each request with method, path, status, and duration.
func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			wr := &responseWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(wr, r)
			logger.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", wr.status,
				"duration", time.Since(start).String(),
			)
		})
	}
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}
