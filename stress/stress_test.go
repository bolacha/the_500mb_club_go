// Package stress contains end-to-end stress tests that require a running API + Redis.
// Run with: go test -tags=stress -count=1 -timeout=30m ./stress/
//
// These tests hit the real HTTP server, so they need the stack running:
//
//	make up          # starts docker compose
//	make test-stress # runs these tests
//	make down        # stops everything
package stress

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var baseURL string

func init() {
	baseURL = os.Getenv("BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
}

func TestSmokeEndpoints(t *testing.T) {
	// healthz
	resp, err := http.Get(baseURL + "/healthz")
	if err != nil {
		t.Fatalf("healthz: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("healthz status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	// readyz
	resp, err = http.Get(baseURL + "/readyz")
	if err != nil {
		t.Fatalf("readyz: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("readyz status = %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestStressConcurrentWrites(t *testing.T) {
	deviceID := fmt.Sprintf("stress-concurrent-%d", time.Now().UnixNano())
	concurrency := 10
	writesPerGoroutine := 100

	var wg sync.WaitGroup
	var success atomic.Int64
	var failures atomic.Int64

	client := &http.Client{Timeout: 5 * time.Second}

	for range concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range writesPerGoroutine {
				p := map[string]any{
					"ts":      time.Now().UnixMilli() + int64(i),
					"lat":     -23.55,
					"lon":     -46.63,
					"battery": 0.8,
					"ax":      0.1,
					"ay":      -0.04,
					"az":      9.81,
				}
				body, _ := json.Marshal(p)
				url := fmt.Sprintf("%s/devices/%s/telemetry", baseURL, deviceID)
				resp, err := client.Post(url, "application/json", bytes.NewReader(body))
				if err != nil {
					failures.Add(1)
					continue
				}
				if resp.StatusCode == 202 {
					success.Add(1)
				} else {
					failures.Add(1)
				}
				resp.Body.Close()
			}
		}()
	}
	wg.Wait()

	t.Logf("writes: %d success, %d failures", success.Load(), failures.Load())
	if failures.Load() > 0 {
		t.Errorf("%d write failures", failures.Load())
	}
}

func TestStressBatchWrites(t *testing.T) {
	deviceID := fmt.Sprintf("stress-batch-%d", time.Now().UnixNano())
	client := &http.Client{Timeout: 5 * time.Second}

	points := make([]map[string]any, 100)
	for i := range points {
		points[i] = map[string]any{
			"ts":      time.Now().UnixMilli() + int64(i*100),
			"lat":     -23.55 + float64(i)*0.001,
			"lon":     -46.63 + float64(i)*0.001,
			"battery": 0.8,
			"ax":      0.1,
			"ay":      -0.04,
			"az":      9.81,
		}
	}

	for range 50 {
		body, _ := json.Marshal(map[string]any{"points": points})
		url := fmt.Sprintf("%s/devices/%s/telemetry/batch", baseURL, deviceID)
		resp, err := client.Post(url, "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("batch: %v", err)
		}
		if resp.StatusCode != 202 {
			t.Errorf("batch status = %d", resp.StatusCode)
		}
		resp.Body.Close()
	}

	t.Log("50 batches of 100 points written")
}

func TestStressSustainedLoad(t *testing.T) {
	deviceID := fmt.Sprintf("stress-sustained-%d", time.Now().UnixNano())
	client := &http.Client{Timeout: 5 * time.Second}

	duration := 10 * time.Second
	start := time.Now()

	var requests atomic.Int64
	var errors atomic.Int64

	// Pre-seed with 256 points for anomaly queries.
	points := make([]map[string]any, 256)
	for i := range points {
		points[i] = map[string]any{
			"ts":      start.UnixMilli() - int64((256-i)*100),
			"lat":     -23.55,
			"lon":     -46.63,
			"battery": 0.8,
			"ax":      float64(i%3) * 0.1,
			"ay":      float64(i%5) * 0.05,
			"az":      9.81 + float64(i%4)*0.02,
		}
	}
	body, _ := json.Marshal(map[string]any{"points": points})
	resp, err := client.Post(
		fmt.Sprintf("%s/devices/%s/telemetry/batch", baseURL, deviceID),
		"application/json", bytes.NewReader(body),
	)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	resp.Body.Close()

	// Sustained load: mix of POST and GET operations.
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Since(start) < duration {
				// POST single
				p := map[string]any{
					"ts": time.Now().UnixMilli(), "lat": -23.55, "lon": -46.63,
					"battery": 0.8, "ax": 0.1, "ay": -0.04, "az": 9.81,
				}
				b, _ := json.Marshal(p)
				resp, err := client.Post(
					fmt.Sprintf("%s/devices/%s/telemetry", baseURL, deviceID),
					"application/json", bytes.NewReader(b),
				)
				requests.Add(1)
				if err != nil || resp.StatusCode != 202 {
					errors.Add(1)
				}
				if resp != nil {
					resp.Body.Close()
				}

				// GET anomaly
				resp, err = client.Get(fmt.Sprintf("%s/devices/%s/anomaly", baseURL, deviceID))
				requests.Add(1)
				if err != nil || resp.StatusCode != 200 {
					errors.Add(1)
				}
				if resp != nil {
					resp.Body.Close()
				}
			}
		}()
	}
	wg.Wait()

	t.Logf("sustained: %d requests, %d errors over %v", requests.Load(), errors.Load(), duration)
	if float64(errors.Load())/float64(requests.Load()) > 0.01 {
		t.Errorf("error rate %.2f%% > 1%%", float64(errors.Load())/float64(requests.Load())*100)
	}
}

func TestStressSpike(t *testing.T) {
	deviceID := fmt.Sprintf("stress-spike-%d", time.Now().UnixNano())
	client := &http.Client{Timeout: 5 * time.Second}

	var wg sync.WaitGroup
	var requests atomic.Int64
	var errors atomic.Int64

	// Simulate spike: 50 goroutines fire requests simultaneously.
	concurrency := 50
	burst := 20

	for range concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range burst {
				p := map[string]any{
					"ts": time.Now().UnixMilli(), "lat": -23.55, "lon": -46.63,
					"battery": 0.8, "ax": 0.1, "ay": -0.04, "az": 9.81,
				}
				b, _ := json.Marshal(p)
				resp, err := client.Post(
					fmt.Sprintf("%s/devices/%s/telemetry", baseURL, deviceID),
					"application/json", bytes.NewReader(b),
				)
				requests.Add(1)
				if err != nil || (resp != nil && resp.StatusCode != 202) {
					errors.Add(1)
				}
				if resp != nil {
					resp.Body.Close()
				}
			}
		}()
	}
	wg.Wait()

	t.Logf("spike: %d requests, %d errors", requests.Load(), errors.Load())
	if float64(errors.Load())/float64(requests.Load()) > 0.01 {
		t.Errorf("spike error rate %.2f%% > 1%%", float64(errors.Load())/float64(requests.Load())*100)
	}
}
