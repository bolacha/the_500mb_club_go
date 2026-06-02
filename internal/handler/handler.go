// Package handler implements the HTTP API handlers for the 500MB Club challenge.
// Uses Go 1.22+ enhanced ServeMux for method-based routing with path parameters.
package handler

import (
	"log/slog"
	"net/http"

	"github.com/bolacha/the_500mb_club_go/internal/redis"
	"github.com/bolacha/the_500mb_club_go/internal/telemetry"
)

// Handler holds all dependencies for HTTP handlers.
type Handler struct {
	redis   *redis.Client
	store   *telemetry.Store
	logger  *slog.Logger
	metrics *metrics
}

// metrics tracks simple counters for the /metrics endpoint.
type metrics struct {
	requests *counter
}

type counter struct {
	value int64
}

func (c *counter) inc() { c.value++ }
func (c *counter) val() int64 { return c.value }

// New creates a new Handler with the given Redis client.
func New(redisClient *redis.Client, logger *slog.Logger) *Handler {
	return &Handler{
		redis:  redisClient,
		store:  telemetry.NewStore(redisClient),
		logger: logger,
		metrics: &metrics{
			requests: &counter{},
		},
	}
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
