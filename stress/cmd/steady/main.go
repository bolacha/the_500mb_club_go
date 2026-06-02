// Steady-state load test — matches the benchmark's "steady" scenario.
// 200 RPS constant, 60s, realistic mix: 60% POST, 10% batch, 20% range, 10% anomaly.
// Feeds: efficiency (RSS+CPU), tail latency (p99 per op), and the gate.
//
// Usage: go run ./stress/cmd/steady/

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
	baseURL  = flag.String("url", "http://localhost:8080", "API base URL")
	duration = flag.Duration("dur", 60*time.Second, "Test duration")
	rps      = flag.Int("rps", 200, "Target requests per second")
)

func main() {
	flag.Parse()
	device := fmt.Sprintf("steady-%d", time.Now().Unix())

	fmt.Println("╔══════════════════════════════════════════════╗")
	fmt.Println("║   STEADY-STATE TEST (matches benchmark)     ║")
	fmt.Println("╚══════════════════════════════════════════════╝")
	fmt.Printf("URL: %s  |  RPS: %d  |  Duration: %v\n\n", *baseURL, *rps, *duration)

	seedDevice(device)

	var (
		postLat, batchLat, rangeLat, anomalyLat []time.Duration
		mu       sync.Mutex
		errCount atomic.Int64
		okCount  atomic.Int64
	)

	deadline := time.Now().Add(*duration)
	interval := time.Second / time.Duration(*rps)

	var wg sync.WaitGroup
	wg.Go(func() {
		for time.Now().Before(deadline) {
			t0 := time.Now()
			op := pickOpSteady()
			url, body := buildReq(device, op)
			resp, err := doReq(url, body)
			lat := time.Since(t0)

			mu.Lock()
			switch op {
			case "post":
				postLat = append(postLat, lat)
			case "batch":
				batchLat = append(batchLat, lat)
			case "range":
				rangeLat = append(rangeLat, lat)
			case "anomaly":
				anomalyLat = append(anomalyLat, lat)
			}
			mu.Unlock()

			if err != nil || (resp != nil && resp.StatusCode >= 400) {
				errCount.Add(1)
			} else {
				okCount.Add(1)
			}
			if resp != nil {
				resp.Body.Close()
			}
			time.Sleep(interval)
		}
	})
	wg.Wait()

	errors := int(errCount.Load())
	delivered := int(okCount.Load())
	total := delivered + errors
	errRate := float64(errors) / math.Max(float64(total), 1)
	effectiveRPS := float64(total) / duration.Seconds()

	fmt.Printf("╔══════════════════════════════════════════════╗\n")
	fmt.Printf("║  STEADY RESULTS                             ║\n")
	fmt.Printf("╠══════════════════════════════════════════════╣\n")
	fmt.Printf("║  Total:    %-8d  (%.0f RPS)          ║\n", total, effectiveRPS)
	fmt.Printf("║  Errors:   %-8d  (%.2f%%)             ║\n", errors, errRate*100)

	printOpStats("POST", postLat)
	printOpStats("BATCH", batchLat)
	printOpStats("RANGE", rangeLat)
	printOpStats("ANOMALY", anomalyLat)

	gatePassed := errRate < 0.005
	status := "✅ GATE PASSED"
	if !gatePassed {
		status = "❌ GATE FAILED"
	}
	fmt.Printf("║  Gate:     %s                ║\n", status)
	fmt.Printf("╚══════════════════════════════════════════════╝\n")
}

func printOpStats(name string, latencies []time.Duration) {
	if len(latencies) == 0 {
		fmt.Printf("║  %-8s  (no samples)                      ║\n", name)
		return
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	p50 := latencies[len(latencies)*50/100]
	p95 := latencies[len(latencies)*95/100]
	p99 := latencies[len(latencies)*99/100]
	fmt.Printf("║  %-8s  p50=%7v  p95=%7v  p99=%7v  ║\n",
		name, p50.Round(time.Microsecond), p95.Round(time.Microsecond), p99.Round(time.Microsecond))
}

func pickOpSteady() string {
	r := rand.Float64()
	switch {
	case r < 0.60:
		return "post"
	case r < 0.70:
		return "batch"
	case r < 0.90:
		return "range"
	default:
		return "anomaly"
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
