package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInstanceID(t *testing.T) {
	id := "api-test-1"
	handler := InstanceID(id)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	got := rr.Header().Get("X-Instance-Id")
	if got != id {
		t.Errorf("X-Instance-Id = %q, want %q", got, id)
	}
}

func TestInstanceIDOnAllStatusCodes(t *testing.T) {
	id := "api-2"
	handler := InstanceID(id)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Header().Get("X-Instance-Id") != id {
		t.Error("X-Instance-Id missing on 404")
	}
}
