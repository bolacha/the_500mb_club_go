package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"sync"

	"github.com/bolacha/the_500mb_club_go/internal/telemetry"
)

var (
	jsonBufPool = sync.Pool{New: func() any { return new(bytes.Buffer) }}
)

func validDeviceID(id string) bool {
	n := len(id)
	if n == 0 || n > 64 {
		return false
	}
	for i := range n {
		c := id[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

type (
	telemetryBatchPayload struct {
		Points []telemetry.TelemetryPoint `json:"points"`
	}
	batchResponse struct {
		Accepted int `json:"accepted"`
	}
)

func (h *Handler) handlePostSingle(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validDeviceID(id) {
		http.Error(w, `{"error":"invalid device id"}`, http.StatusBadRequest)
		return
	}

	// Decode directly from request body — saves one allocation vs ReadFull+Unmarshal.
	// MaxBytesReader handles 413 for oversized payloads.
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var p telemetry.TelemetryPoint
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	if err := p.Validate(); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	h.writeBuf.Add(r.Context(), id, p)
	h.postCount.Add(1)
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) handlePostBatch(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validDeviceID(id) {
		http.Error(w, `{"error":"invalid device id"}`, http.StatusBadRequest)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 65536)
	var payload telemetryBatchPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			w.Write([]byte(`{"error":"payload too large"}`))
			return
		}
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	if len(payload.Points) < 1 {
		http.Error(w, `{"error":"batch must contain at least 1 point"}`, http.StatusBadRequest)
		return
	}
	if len(payload.Points) > 100 {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		w.Write([]byte(`{"error":"batch exceeds 100 points"}`))
		return
	}

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

	h.batchCount.Add(1)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, batchResponse{Accepted: accepted})
}
