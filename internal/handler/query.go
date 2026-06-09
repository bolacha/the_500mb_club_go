package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/bolacha/the_500mb_club_go/internal/anomaly"
	"github.com/bolacha/the_500mb_club_go/internal/telemetry"
)

// queryResponse is the JSON structure for GET /telemetry.
type queryResponse struct {
	Points     []telemetry.TelemetryPoint `json:"points"`
	NextCursor *string                     `json:"next_cursor,omitzero"`
}

// anomalyError is the JSON structure for 404 anomaly responses.
type anomalyError struct {
	ZScore    float64 `json:"z_score"`
	Samples   int     `json:"samples"`
	Anomalous bool    `json:"anomalous"`
}

func (h *Handler) handleQuery(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validDeviceID(id) {
		http.Error(w, `{"error":"invalid device id"}`, http.StatusBadRequest)
		return
	}

	q := r.URL.Query()

	fromStr := q.Get("from")
	toStr := q.Get("to")
	if fromStr == "" || toStr == "" {
		http.Error(w, `{"error":"from and to are required"}`, http.StatusBadRequest)
		return
	}

	from, err := strconv.ParseInt(fromStr, 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid from"}`, http.StatusBadRequest)
		return
	}
	to, err := strconv.ParseInt(toStr, 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid to"}`, http.StatusBadRequest)
		return
	}
	if from > to {
		http.Error(w, `{"error":"from must be <= to"}`, http.StatusBadRequest)
		return
	}

	limit := 100
	if ls := q.Get("limit"); ls != "" {
		l, err := strconv.Atoi(ls)
		if err != nil || l < 1 || l > 500 {
			http.Error(w, `{"error":"limit must be 1-500"}`, http.StatusBadRequest)
			return
		}
		limit = l
	}

	// Parse tie-safe cursor: "timestamp:offset" or legacy plain timestamp.
	var cursorTS int64
	var cursorOffset int
	if cs := q.Get("cursor"); cs != "" {
		if idx := strings.IndexByte(cs, ':'); idx >= 0 {
			c, err := strconv.ParseInt(cs[:idx], 10, 64)
			if err != nil {
				http.Error(w, `{"error":"invalid cursor"}`, http.StatusBadRequest)
				return
			}
			o, err := strconv.Atoi(cs[idx+1:])
			if err != nil {
				http.Error(w, `{"error":"invalid cursor"}`, http.StatusBadRequest)
				return
			}
			cursorTS = c
			cursorOffset = o
		} else {
			// Legacy: plain timestamp cursor — start after it.
			c, err := strconv.ParseInt(cs, 10, 64)
			if err != nil {
				http.Error(w, `{"error":"invalid cursor"}`, http.StatusBadRequest)
				return
			}
			cursorTS = c + 1 // exclusive
		}
	}

	points, err := h.store.Query(r.Context(), id, from, to, limit+1, cursorTS)
	if err != nil {
		h.logger.Error("query failed", "device", id, "err", err)
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}

	// Apply offset: skip points at cursorTS that were already returned.
	if cursorOffset > 0 && len(points) > 0 {
		skip := cursorOffset
		for i, p := range points {
			if p.TS != cursorTS || skip == 0 {
				points = points[i:]
				break
			}
			skip--
		}
	}

	h.queryCount.Add(1)

	// Determine next_cursor with tie-safe encoding.
	var nextCursor *string
	if len(points) > limit {
		last := points[limit-1]
		// Count how many points at this timestamp are in the page.
		sameTS := 0
		for i := limit - 1; i >= 0 && points[i].TS == last.TS; i-- {
			sameTS++
		}
		// Count total points with this TS in the full result.
		totalSameTS := sameTS
		for i := limit; i < len(points) && points[i].TS == last.TS; i++ {
			totalSameTS++
		}
		enc := fmt.Sprintf("%d:%d", last.TS, totalSameTS)
		nextCursor = &enc
		points = points[:limit]
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	writeJSON(w, queryResponse{Points: points, NextCursor: nextCursor})
}

func (h *Handler) handleAnomaly(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validDeviceID(id) {
		http.Error(w, `{"error":"invalid device id"}`, http.StatusBadRequest)
		return
	}

	// Fetch last 256 raw bytes — zero alloc, no struct decode.
	rawPoints, err := h.store.LastNRaw(r.Context(), id, 256)
	if err != nil {
		h.logger.Error("anomaly fetch failed", "device", id, "err", err)
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}

	result, err := anomaly.ComputeBinary(rawPoints)
	if err != nil {
		// Not enough samples — return 404.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, anomalyError{Samples: result.Samples})
		return
	}

	h.anomalyCount.Add(1)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	writeJSON(w, result)
}
