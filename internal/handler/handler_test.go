package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bolacha/the_500mb_club_go/internal/telemetry"
)

func TestValidDeviceID(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"dev-1", true},
		{"device_123", true},
		{"abcDEF", true},
		{"a", true},
		{"a1b2c3d4e5f6g7h8i9j0_abcdefghijklMNOPQRSTUVWXYZ123456789012", true},
		{"", false},
		{"dev 1", false},
		{"dev/1", false},
		{"dev@1", false},
		// 65 chars — too long.
		{"a1234567890123456789012345678901234567890123456789012345678901234", false},
	}
	for _, tt := range tests {
		got := validDeviceID(tt.id)
		if got != tt.want {
			t.Errorf("validDeviceID(%q) = %v, want %v", tt.id, got, tt.want)
		}
	}
}

func TestHandleHealthz(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	handleHealthzTop(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if !strings.Contains(rr.Body.String(), "ok") {
		t.Errorf("body = %q, want contains 'ok'", rr.Body.String())
	}
}

func TestHandleHealthzTop(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	handleHealthzTop(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d", rr.Code)
	}
}

func TestHandlePostSingleInvalidJSON(t *testing.T) {
	h := &Handler{store: telemetry.NewStore(nil)} // nil store will panic if reached

	body := strings.NewReader(`not json`)
	req := httptest.NewRequest(http.MethodPost, "/devices/dev-1/telemetry", body)
	req.SetPathValue("id", "dev-1")
	rr := httptest.NewRecorder()

	// This will fail at JSON decode — should return 400.
	h.handlePostSingle(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandlePostSingleInvalidDeviceID(t *testing.T) {
	h := &Handler{}

	body := strings.NewReader(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/devices/invalid%20id/telemetry", body)
	req.SetPathValue("id", "invalid id") // space not allowed by regex
	rr := httptest.NewRecorder()

	h.handlePostSingle(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandlePostBatchEmpty(t *testing.T) {
	h := &Handler{}

	body := strings.NewReader(`{"points":[]}`)
	req := httptest.NewRequest(http.MethodPost, "/devices/dev-1/telemetry/batch", body)
	req.SetPathValue("id", "dev-1")
	rr := httptest.NewRecorder()

	h.handlePostBatch(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandlePostBatchTooLarge(t *testing.T) {
	h := &Handler{}

	// Build a payload with 101 points.
	points := make([]telemetry.TelemetryPoint, 101)
	payload, _ := json.Marshal(map[string]any{"points": points})

	req := httptest.NewRequest(http.MethodPost, "/devices/dev-1/telemetry/batch", strings.NewReader(string(payload)))
	req.SetPathValue("id", "dev-1")
	rr := httptest.NewRecorder()

	h.handlePostBatch(rr, req)
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestHandleQueryMissingParams(t *testing.T) {
	h := &Handler{}

	req := httptest.NewRequest(http.MethodGet, "/devices/dev-1/telemetry", nil)
	req.SetPathValue("id", "dev-1")
	rr := httptest.NewRecorder()

	h.handleQuery(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandleQueryFromGreaterThanTo(t *testing.T) {
	h := &Handler{}

	req := httptest.NewRequest(http.MethodGet, "/devices/dev-1/telemetry?from=2000&to=1000", nil)
	req.SetPathValue("id", "dev-1")
	rr := httptest.NewRecorder()

	h.handleQuery(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandleQueryInvalidLimit(t *testing.T) {
	h := &Handler{}

	tests := []struct {
		limit string
	}{
		{"0"},
		{"501"},
		{"abc"},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodGet, "/devices/dev-1/telemetry?from=1000&to=2000&limit="+tt.limit, nil)
		req.SetPathValue("id", "dev-1")
		rr := httptest.NewRecorder()

		h.handleQuery(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("limit=%s: status = %d, want %d", tt.limit, rr.Code, http.StatusBadRequest)
		}
	}
}
