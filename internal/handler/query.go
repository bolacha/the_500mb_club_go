package handler

import (
	"net/http"
	"strconv"

	"github.com/bolacha/the_500mb_club_go/internal/anomaly"
)

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

	cursor := int64(0)
	if cs := q.Get("cursor"); cs != "" {
		c, err := strconv.ParseInt(cs, 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid cursor"}`, http.StatusBadRequest)
			return
		}
		cursor = c
	}

	points, err := h.store.Query(r.Context(), id, from, to, limit, cursor)
	if err != nil {
		h.logger.Error("query failed", "device", id, "err", err)
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}

	h.queryCount.Add(1)

	// Determine next_cursor from the last returned point's timestamp.
	var nextCursor *string
	if len(points) == limit {
		ts := strconv.FormatInt(points[len(points)-1].TS, 10)
		nextCursor = &ts
	}

	resp := map[string]any{
		"points": points,
		"next_cursor": nextCursor,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	writeJSON(w, resp)
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
	writeJSON(w, map[string]any{
		"z_score":   0,
		"samples":   result.Samples,
		"anomalous": false,
	})
		return
	}

	h.anomalyCount.Add(1)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	writeJSON(w, result)
}
