// Package main — concurrent throughput stress test.
// Finds the max sustained RPS using multiple worker goroutines.
//
// Usage: go run ./stress/concurrent.go -url http://localhost:8080
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

var (
	baseURL   = flag.String("url", "http://localhost:8080", "API base URL")
	deviceID  = flag.String("device", fmt.Sprintf("ctp-%d", time.Now().Unix()), "Device ID")
	workers   = flag.Int("workers", 50, "Number of concurrent workers")
	duration  = flag.Duration("dur", 15*time.Second, "Test duration")
	p99Target = flag.Duration("p99", 150*time.Millisecond, "p99 SLO")
	errTarget = flag.Float64("err", 0.005, "Max error rate")
)

func main() {
	flag.Parse()

	fmt.Println("╔══════════════════════════════════════════════╗")
	fmt.Println("║   500MB Club — Concurrent Throughput Test   ║")
	fmt.Println("╚══════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Printf("Target:   %s\n", *baseURL)
	fmt.Printf("Workers:  %d\n", *workers)
	fmt.Printf("Duration: %v\n", *duration)
	fmt.Printf("SLO:      p99 < %v, errors < %.1f%%\n", *p99Target, *errTarget*100)

	// Pre-seed.
	fmt.Print("Seeding... ")
	seedDevice(*deviceID)
	fmt.Println("done.")

	var (
		latencies []time.Duration
		mu        sync.Mutex
		okCount   atomic.Int64
		errCount  atomic.Int64
	)

	var wg sync.WaitGroup
	deadline := time.Now().Add(*duration)

	for range *workers {
		wg.Go(func() {
			client := &http.Client{Timeout: 5 * time.Second}
			for time.Now().Before(deadline) {
				t0 := time.Now()
				op := pickOp()
				url, body := buildRequest(op)
				resp, err := doReq(client, url, body)
				lat := time.Since(t0)

				mu.Lock()
				latencies = append(latencies, lat)
				mu.Unlock()

				if err != nil || (resp != nil && resp.StatusCode >= 400) {
					errCount.Add(1)
				} else {
					okCount.Add(1)
				}
				if resp != nil {
					resp.Body.Close()
				}
			}
		})
	}
	wg.Wait()

	delivered := int(okCount.Load())
	errors := int(errCount.Load())
	total := delivered + errors
	elapsed := *duration
	rps := float64(total) / elapsed.Seconds()
	errRate := float64(errors) / math.Max(float64(total), 1)

	if len(latencies) == 0 {
		fmt.Println("No requests completed.")
		return
	}

	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })

	var sum time.Duration
	for _, l := range latencies {
		sum += l
	}
	avg := time.Duration(int64(sum) / int64(len(latencies)))
	p50 := latencies[len(latencies)*50/100]
	p95 := latencies[len(latencies)*95/100]
	p99 := latencies[len(latencies)*99/100]

	sustained := errRate <= *errTarget && p99 <= *p99Target
	status := "✅ SUSTAINED"
	if !sustained {
		status = "❌ BROKEN"
	}

	fmt.Printf("╔══════════════════════════════════════════════╗\n")
	fmt.Printf("║  RESULTS                                   ║\n")
	fmt.Printf("╠══════════════════════════════════════════════╣\n")
	fmt.Printf("║  Total requests:  %-8d                 ║\n", total)
	fmt.Printf("║  Effective RPS:   %-8.0f                 ║\n", rps)
	fmt.Printf("║  Errors:          %-8d (%.2f%%)        ║\n", errors, errRate*100)
	fmt.Printf("║  Avg latency:     %-8v                 ║\n", avg.Round(time.Microsecond))
	fmt.Printf("║  P50 latency:     %-8v                 ║\n", p50.Round(time.Microsecond))
	fmt.Printf("║  P95 latency:     %-8v                 ║\n", p95.Round(time.Microsecond))
	fmt.Printf("║  P99 latency:     %-8v                 ║\n", p99.Round(time.Microsecond))
	fmt.Printf("║  Status:          %s       ║\n", status)
	fmt.Printf("╚══════════════════════════════════════════════╝\n")
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
	if resp != nil {
		resp.Body.Close()
	}
}

func pickOp() string {
	r := float64(time.Now().UnixNano()%100) / 100.0
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

func buildRequest(op string) (string, []byte) {
	id := *deviceID
	now := time.Now().UnixMilli()
	base := fmt.Sprintf("%s/devices/%s", *baseURL, id)

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

func doReq(client *http.Client, url string, body []byte) (*http.Response, error) {
	if body != nil {
		return client.Post(url, "application/json", bytes.NewReader(body))
	}
	return client.Get(url)
}
