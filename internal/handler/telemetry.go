package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strconv"

	"github.com/bolacha/the_500mb_club_go/internal/telemetry"
)

var deviceIDRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func validDeviceID(id string) bool {
	return deviceIDRe.MatchString(id)
}

// telemetryBatchPayload is the JSON structure for batch ingest.
type telemetryBatchPayload struct {
	Points []telemetry.TelemetryPoint `json:"points"`
}

func (h *Handler) handlePostSingle(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validDeviceID(id) {
		http.Error(w, `{"error":"invalid device id"}`, http.StatusBadRequest)
		return
	}

	// Limit body size to 4KB for single points.
	body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
	if err != nil {
		http.Error(w, `{"error":"failed to read body"}`, http.StatusBadRequest)
		return
	}

	point, err := telemetry.DecodeJSON(body)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	if err := h.store.IngestSingle(r.Context(), id, point); err != nil {
		h.logger.Error("ingest single failed", "device", id, "err", err)
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) handlePostBatch(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validDeviceID(id) {
		http.Error(w, `{"error":"invalid device id"}`, http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 65536)) // 64KB max
	if err != nil {
		http.Error(w, `{"error":"failed to read body"}`, http.StatusBadRequest)
		return
	}

	var payload telemetryBatchPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	n := len(payload.Points)
	if n < 1 {
		http.Error(w, `{"error":"batch must contain at least 1 point"}`, http.StatusBadRequest)
		return
	}
	if n > 100 {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		w.Write([]byte(`{"error":"batch exceeds 100 points"}`))
		return
	}

	// Validate all points before ingesting.
	for i := range payload.Points {
		if err := payload.Points[i].Validate(); err != nil {
			http.Error(w, `{"error":"invalid point at index `+strconv.Itoa(i)+`: `+err.Error()+`"}`, http.StatusBadRequest)
			return
		}
	}

	accepted, err := h.store.IngestBatch(r.Context(), id, payload.Points)
	if err != nil {
		h.logger.Error("ingest batch failed", "device", id, "err", err)
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]int{"accepted": accepted})
}
