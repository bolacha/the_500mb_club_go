package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"sync"

	"github.com/bolacha/the_500mb_club_go/internal/telemetry"
)

var (
	bodyPool4K  = sync.Pool{New: func() any { return make([]byte, 4096) }}
	bodyPool64K = sync.Pool{New: func() any { return make([]byte, 65536) }}
	jsonBufPool = sync.Pool{New: func() any { return new(bytes.Buffer) }}
)

// validDeviceID checks the device ID pattern [a-zA-Z0-9_-]{1,64} without regex.
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

type telemetryBatchPayload struct {
	Points []telemetry.TelemetryPoint `json:"points"`
}

func (h *Handler) handlePostSingle(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validDeviceID(id) {
		http.Error(w, `{"error":"invalid device id"}`, http.StatusBadRequest)
		return
	}

	buf := bodyPool4K.Get().([]byte)
	n, err := io.ReadFull(r.Body, buf[:4096])
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		bodyPool4K.Put(buf)
		http.Error(w, `{"error":"failed to read body"}`, http.StatusBadRequest)
		return
	}
	if n == 4096 {
		extra := make([]byte, 1)
		if extraN, _ := r.Body.Read(extra); extraN > 0 {
			bodyPool4K.Put(buf)
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
	}
	body := buf[:n]
	defer bodyPool4K.Put(buf)

	point, err := telemetry.DecodeJSON(body)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	h.writeBuf.Add(r.Context(), id, point)
	h.postCount.Add(1)
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) handlePostBatch(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validDeviceID(id) {
		http.Error(w, `{"error":"invalid device id"}`, http.StatusBadRequest)
		return
	}

	buf := bodyPool64K.Get().([]byte)
	n, err := io.ReadFull(r.Body, buf[:65536])
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		bodyPool64K.Put(buf)
		http.Error(w, `{"error":"failed to read body"}`, http.StatusBadRequest)
		return
	}
	body := buf[:n]
	defer bodyPool64K.Put(buf)

	var payload telemetryBatchPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	n = len(payload.Points)
	if n < 1 {
		http.Error(w, `{"error":"batch must contain at least 1 point"}`, http.StatusBadRequest)
		return
	}
	if n > 100 {
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
	writeJSON(w, map[string]int{"accepted": accepted})
}
