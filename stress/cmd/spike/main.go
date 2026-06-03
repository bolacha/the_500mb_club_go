// Spike test — matches the benchmark's "spike" scenario.
// Ramps 50→800 RPS, holds the peak, then backs off.
// Feeds: resilience (p99 under spike + error rate).
//
// Usage: go run ./stress/cmd/spike/

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

var (
	baseURL    = flag.String("url", "http://localhost:8080", "API base URL")
	peakRPS    = flag.Int("peak", 800, "Peak RPS during spike")
	rampDur    = flag.Duration("ramp", 10*time.Second, "Ramp duration")
	peakDur    = flag.Duration("hold", 30*time.Second, "Hold duration at peak")
	workers    = flag.Int("workers", 4, "Number of goroutines")
)

func main() {
	flag.Parse()
	device := fmt.Sprintf("spike-%d", time.Now().Unix())

	fmt.Println("╔══════════════════════════════════════════════╗")
	fmt.Println("║   SPIKE TEST (matches benchmark)            ║")
	fmt.Println("╚══════════════════════════════════════════════╝")
	fmt.Printf("URL: %s  |  Peak: %d RPS  |  Ramp: %v  |  Hold: %v\n\n",
		*baseURL, *peakRPS, *rampDur, *peakDur)

	seedDevice(device)

	var (
		spikeLatencies []time.Duration
		mu             sync.Mutex
		errCount       atomic.Int64
		okCount        atomic.Int64
	)

	// Phase 1: gradual ramp 50→peak
	fmt.Printf("Ramping 50→%d RPS over %v...\n", *peakRPS, *rampDur)
	rampStart := time.Now()
	for time.Since(rampStart) < *rampDur {
		elapsed := time.Since(rampStart)
		currentRPS := 50 + int(float64(*peakRPS-50)*elapsed.Seconds()/rampDur.Seconds())
		fireRequests(device, currentRPS, 1*time.Second, &spikeLatencies, &mu, &okCount, &errCount)
	}

	// Phase 2: hold at peak
	fmt.Printf("Holding at %d RPS for %v...\n", *peakRPS, *peakDur)
	fireRequests(device, *peakRPS, *peakDur, &spikeLatencies, &mu, &okCount, &errCount)

	// Phase 3: back off (not scored, just graceful)
	fmt.Println("Backing off...")
	fireRequests(device, 50, 3*time.Second, &spikeLatencies, &mu, &okCount, &errCount)

	errors := int(errCount.Load())
	delivered := int(okCount.Load())
	total := delivered + errors
	errRate := float64(errors) / math.Max(float64(total), 1)

	fmt.Printf("\n╔══════════════════════════════════════════════╗\n")
	fmt.Printf("║  SPIKE RESULTS                              ║\n")
	fmt.Printf("╠══════════════════════════════════════════════╣\n")
	fmt.Printf("║  Total:    %-8d                           ║\n", total)
	fmt.Printf("║  Errors:   %-8d  (%.2f%%)                   ║\n", errors, errRate*100)

	if len(spikeLatencies) > 0 {
		sort.Slice(spikeLatencies, func(i, j int) bool { return spikeLatencies[i] < spikeLatencies[j] })
		p50 := spikeLatencies[len(spikeLatencies)*50/100]
		p95 := spikeLatencies[len(spikeLatencies)*95/100]
		p99 := spikeLatencies[len(spikeLatencies)*99/100]
		fmt.Printf("║  p50:      %-10v                         ║\n", p50.Round(time.Microsecond))
		fmt.Printf("║  p95:      %-10v                         ║\n", p95.Round(time.Microsecond))
		fmt.Printf("║  p99:      %-10v  (target <12ms)         ║\n", p99.Round(time.Microsecond))
		spikeStatus := "✅ UNDER 12ms"
		if p99 > 12*time.Millisecond {
			spikeStatus = "❌ OVER 12ms"
		}
		fmt.Printf("║  Status:   %s                           ║\n", spikeStatus)
	}
	errStatus := "✅ UNDER 1%"
	if errRate > 0.01 {
		errStatus = "❌ OVER 1%"
	}
	fmt.Printf("║  Err Rate: %.2f%%   %s                     ║\n", errRate*100, errStatus)
	fmt.Printf("╚══════════════════════════════════════════════╝\n")
}

func fireRequests(device string, rps int, dur time.Duration, latencies *[]time.Duration, mu *sync.Mutex, okCount, errCount *atomic.Int64) {
	if rps <= 0 { return }
	n := *workers
	workerRPS := rps / n
	interval := time.Second / time.Duration(workerRPS)
	deadline := time.Now().Add(dur)

	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			for time.Now().Before(deadline) {
				t0 := time.Now()
				op := pickOpSteady()
				url, body := buildReq(device, op)
				resp, err := doReq(url, body)
				lat := time.Since(t0)

				mu.Lock()
				*latencies = append(*latencies, lat)
				mu.Unlock()

				if err != nil || (resp != nil && resp.StatusCode >= 400) {
					errCount.Add(1)
				} else {
					okCount.Add(1)
				}
				if resp != nil { resp.Body.Close() }
				time.Sleep(interval)
			}
		})
	}
	wg.Wait()
}

func pickOpSteady() string {
	r := rand.Float64()
	switch {
	case r < 0.60: return "post"
	case r < 0.70: return "batch"
	case r < 0.90: return "range"
	default: return "anomaly"
	}
}

func seedDevice(id string) {
	points := make([]map[string]any, 256)
	base := time.Now().UnixMilli()
	for i := range points {
		points[i] = map[string]any{
			"ts": base - int64((256-i)*100), "lat": -23.55, "lon": -46.63,
			"battery": 0.8, "ax": 0.1, "ay": -0.04, "az": 9.81,
		}
	}
	body, _ := json.Marshal(map[string]any{"points": points})
	resp, _ := http.Post(
		fmt.Sprintf("%s/devices/%s/telemetry/batch", *baseURL, id),
		"application/json", bytes.NewReader(body),
	)
	if resp != nil { resp.Body.Close() }
}

func buildReq(device, op string) (string, []byte) {
	now := time.Now().UnixMilli()
	base := fmt.Sprintf("%s/devices/%s", *baseURL, device)
	switch op {
	case "post":
		p := map[string]any{"ts": now, "lat": -23.55, "lon": -46.63, "ax": 0.1, "ay": -0.04, "az": 9.81}
		b, _ := json.Marshal(p)
		return base + "/telemetry", b
	case "batch":
		pts := make([]map[string]any, 50)
		for i := range pts {
			pts[i] = map[string]any{"ts": now - int64((49-i)*100), "lat": -23.55, "lon": -46.63, "ax": 0.1, "ay": -0.04, "az": 9.81}
		}
		b, _ := json.Marshal(map[string]any{"points": pts})
		return base + "/telemetry/batch", b
	case "range":
		return fmt.Sprintf("%s/telemetry?from=%d&to=%d&limit=50", base, now-60000, now), nil
	default:
		return base + "/anomaly", nil
	}
}

func doReq(url string, body []byte) (*http.Response, error) {
	if body != nil {
		return http.Post(url, "application/json", bytes.NewReader(body))
	}
	return http.Get(url)
}
