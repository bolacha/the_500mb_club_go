// Throughput stress test — finds the max sustained RPS.
//
// Run with the stack already up:
//   make up
//   go run ./stress/throughput.go
//   make down
//
// Or as a test:
//   go test -tags=stress -run TestThroughput -count=1 -timeout=30m ./stress/

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"net/http"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

var (
	baseURL    = flag.String("url", "http://localhost:8080", "API base URL")
	deviceID   = flag.String("device", fmt.Sprintf("throughput-%d", time.Now().Unix()), "Device ID")
	startRPS   = flag.Int("start", 100, "Starting RPS")
	endRPS     = flag.Int("end", 2000, "Ending RPS")
	stepRPS    = flag.Int("step", 100, "RPS increment per step")
	stepDur    = flag.Duration("dur", 15*time.Second, "Duration per step")
	p99Target  = flag.Duration("p99", 150*time.Millisecond, "p99 latency SLO")
	errTarget  = flag.Float64("err", 0.005, "Max error rate (0.5%)")
)

func main() {
	flag.Parse()

	fmt.Println("╔══════════════════════════════════════════════╗")
	fmt.Println("║   500MB Club — Throughput Capacity Test     ║")
	fmt.Println("╚══════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Printf("Target:  %s\n", *baseURL)
	fmt.Printf("Device:  %s\n", *deviceID)
	fmt.Printf("Range:   %d → %d RPS (step %d)\n", *startRPS, *endRPS, *stepRPS)
	fmt.Printf("SLO:     p99 < %v, errors < %.1f%%\n", *p99Target, *errTarget*100)
	fmt.Printf("Step:    %v each\n\n", *stepDur)

	// Pre-seed 256 points for anomaly queries.
	fmt.Print("Seeding 256 points for anomaly queries... ")
	seedDevice(*deviceID)
	fmt.Println("done.")

	fmt.Printf("%-8s %-10s %-12s %-12s %-12s %-10s %s\n",
		"RPS", "SUSTAINED", "AVG", "P50", "P99", "ERRORS", "STATUS")
	fmt.Println("──────── ────────── ──────────── ──────────── ──────────── ────────── ──────")

	maxSustained := 0
	var lastOK bool

	for rps := *startRPS; rps <= *endRPS; rps += *stepRPS {
		avg, p50, p99, delivered, errors := runStep(rps, *stepDur)
		errRate := float64(errors) / float64(errors+delivered)
		sustained := errRate <= *errTarget && p99 <= *p99Target && float64(delivered) >= float64(rps)*0.95

		status := "✅"
		if !sustained {
			status = "❌"
			if lastOK {
				status += " KNEE"
			}
		}

		fmt.Printf("%-8d %-10d %-12v %-12v %-12v %-5d/%d %s\n",
			rps, delivered, avg.Round(time.Microsecond), p50.Round(time.Microsecond),
			p99.Round(time.Microsecond), errors, errors+delivered, status)

		if sustained {
			maxSustained = rps
			lastOK = true
		} else {
			lastOK = false
		}
	}

	fmt.Println()
	fmt.Printf("╔══════════════════════════════════════════════╗\n")
	fmt.Printf("║  MAX SUSTAINED RPS: %-5d                   ║\n", maxSustained)
	fmt.Printf("║  Score (vs 1000 par): %.2f                  ║\n", float64(maxSustained)/1000)
	fmt.Printf("╚══════════════════════════════════════════════╝\n")
}

func seedDevice(id string) {
	points := make([]map[string]any, 256)
	base := time.Now().UnixMilli()
	for i := range points {
		points[i] = map[string]any{
			"ts":      base - int64((256-i)*100),
			"lat":     -23.55 + float64(i)*0.001,
			"lon":     -46.63 + float64(i)*0.001,
			"battery": 0.8,
			"ax":      float64(i%3) * 0.1,
			"ay":      float64(i%5) * 0.05,
			"az":      9.81 + float64(i%4)*0.02,
		}
	}
	body, _ := json.Marshal(map[string]any{"points": points})
	url := fmt.Sprintf("%s/devices/%s/telemetry/batch", *baseURL, id)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		fmt.Fprintf(os.Stderr, "seed failed: %v\n", err)
		os.Exit(1)
	}
	resp.Body.Close()
}

type latencySample struct {
	dur time.Duration
}

func runStep(targetRPS int, dur time.Duration) (avg, p50, p99 time.Duration, delivered, errors int) {
	var samples []latencySample
	var mu sync.Mutex
	var errCount atomic.Int64
	var okCount atomic.Int64

	// Calculate interval between requests to achieve target RPS.
	interval := time.Second / time.Duration(targetRPS)

	// Mix: 60% POST, 10% batch, 20% range, 10% anomaly.
	deadline := time.Now().Add(dur)
	var wg sync.WaitGroup

	// Single worker goroutine firing at the target rate.
	wg.Go(func() {
		for time.Now().Before(deadline) {
			t0 := time.Now()
			op := pickOp()
			url, body := buildRequest(op)
			resp, err := doRequest(url, body)
			lat := time.Since(t0)

			mu.Lock()
			samples = append(samples, latencySample{lat})
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

	delivered = int(okCount.Load())
	errors = int(errCount.Load())

	if len(samples) == 0 {
		return 0, 0, 0, 0, 0
	}

	sort.Slice(samples, func(i, j int) bool { return samples[i].dur < samples[j].dur })

	var total time.Duration
	for _, s := range samples {
		total += s.dur
	}
	avg = time.Duration(int64(total) / int64(len(samples)))
	p50 = samples[len(samples)*50/100].dur
	p99 = samples[len(samples)*99/100].dur

	return
}

func pickOp() string {
	// 60% POST, 10% batch, 20% range, 10% anomaly
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

func buildRequest(op string) (url string, body []byte) {
	id := *deviceID
	now := time.Now().UnixMilli()
	base := fmt.Sprintf("%s/devices/%s", *baseURL, id)

	switch op {
	case "post":
		p := map[string]any{
			"ts":      now,
			"lat":     -23.55,
			"lon":     -46.63,
			"battery": 0.82,
			"ax":      0.1,
			"ay":      -0.04,
			"az":      9.81,
		}
		body, _ = json.Marshal(p)
		return base + "/telemetry", body

	case "batch":
		points := make([]map[string]any, 50)
		for i := range points {
			points[i] = map[string]any{
				"ts":      now - int64((49-i)*100),
				"lat":     -23.55,
				"lon":     -46.63,
				"battery": 0.8,
				"ax":      0.1, "ay": -0.04, "az": 9.81,
			}
		}
		body, _ = json.Marshal(map[string]any{"points": points})
		return base + "/telemetry/batch", body

	case "range":
		return fmt.Sprintf("%s/telemetry?from=%d&to=%d&limit=50", base, now-60000, now), nil

	case "anomaly":
		return base + "/anomaly", nil
	}
	return base + "/telemetry", nil
}

func doRequest(url string, body []byte) (*http.Response, error) {
	if body != nil {
		return http.Post(url, "application/json", bytes.NewReader(body))
	}
	return http.Get(url)
}

// Prevent unused import warnings for math.
var _ = math.Abs
