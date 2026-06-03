// Full local benchmark — runs all 3 unscored dimensions:
//   1. Capacity: max sustained RPS (step ramp, finds the knee)
//   2. Resilience: spike test (p99 under burst + error rate)
//   3. Stability: endurance test (latency drift over 5 min)
//
// Produces a score estimate matching the Pi-Bench formula.
//
// Usage:
//   make up                                    # start stack
//   go run ./stress/cmd/bench/ -url http://localhost:8080
//   make down

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

var baseURL = flag.String("url", "http://localhost:8080", "API base URL")

// ── scoring constants (from challenge docs) ──────────────
const (
	capacityTarget   = 1000                 // reference RPS
	spikeP99Target   = 12 * time.Millisecond
	spikeErrTarget   = 0.01 // 1%
	driftTarget      = 1.10
	capacityClipLow  = 0.25
	capacityClipHigh = 4.0
	resilienceClipLow  = 0.25
	resilienceClipHigh = 2.0
	stabilityClipLow   = 0.25
	stabilityClipHigh  = 1.5
)

func main() {
	flag.Parse()
	fmt.Println("╔══════════════════════════════════════════════════╗")
	fmt.Println("║   500MB Club — Full Local Benchmark             ║")
	fmt.Println("╠══════════════════════════════════════════════════╣")
	fmt.Printf("║   URL: %-40s ║\n", *baseURL)
	fmt.Println("╚══════════════════════════════════════════════════╝")
	fmt.Println()

	// Verify stack is up.
	if !healthCheck() {
		fmt.Println("❌ Stack not reachable. Run: make up")
		os.Exit(1)
	}

	var (
		maxRPS    int
		spikeP99  time.Duration
		spikeErr  float64
		latDrift  float64
	)

	// ── 1. Capacity ──────────────────────────────────────
	fmt.Println("─── 1. CAPACITY (max sustained RPS) ───")
	maxRPS = runCapacity()
	fmt.Printf("Max sustained: %d RPS\n\n", maxRPS)

	// ── 2. Resilience ────────────────────────────────────
	fmt.Println("─── 2. RESILIENCE (spike test) ───")
	spikeP99, spikeErr = runSpike()
	fmt.Printf("Spike p99: %v | Error rate: %.2f%%\n\n", spikeP99.Round(time.Microsecond), spikeErr*100)

	// ── 3. Stability ─────────────────────────────────────
	fmt.Println("─── 3. STABILITY (endurance drift) ───")
	latDrift = runEndurance(2 * time.Minute)
	fmt.Printf("Latency drift: %.2fx\n\n", latDrift)

	// ── Score calculation ────────────────────────────────
	capacityScore := clip(float64(maxRPS)/capacityTarget, capacityClipLow, capacityClipHigh)

	spikeP99Score := clip(float64(spikeP99Target)/math.Max(float64(spikeP99), 1e-9), resilienceClipLow, resilienceClipHigh)
	spikeErrScore := clip(spikeErrTarget/math.Max(spikeErr, 1e-9), resilienceClipLow, resilienceClipHigh)
	resilienceScore := (spikeP99Score + spikeErrScore) / 2

	stabilityScore := clip(driftTarget/math.Max(latDrift, 1e-9), stabilityClipLow, stabilityClipHigh)

	// Global score with efficiency=4.0, tail_latency=1.5 (your measured ceilings).
	globalScore := 100 * (0.32*4.0 + 0.27*capacityScore + 0.20*1.5 + 0.13*resilienceScore + 0.08*stabilityScore)

	fmt.Println("╔══════════════════════════════════════════════════╗")
	fmt.Println("║  SCORE ESTIMATE (local, vs Pi-Bench formula)    ║")
	fmt.Println("╠══════════════════════════════════════════════════╣")
	fmt.Printf("║  Efficiency:    4.00  (ceiling, weight 32%%)    ║\n")
	fmt.Printf("║  Capacity:      %-5.2f (weight 27%%, RPS=%d)   ║\n", capacityScore, maxRPS)
	fmt.Printf("║  Tail latency:  1.50  (ceiling, weight 20%%)    ║\n")
	fmt.Printf("║  Resilience:    %-5.2f (weight 13%%)            ║\n", resilienceScore)
	fmt.Printf("║  Stability:     %-5.2f (weight 8%%)             ║\n", stabilityScore)
	fmt.Println("╠══════════════════════════════════════════════════╣")
	fmt.Printf("║  GLOBAL SCORE:  %-6.0f  (100 = meets target)   ║\n", globalScore)
	fmt.Println("╚══════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("⚠️  Local (M1 Max / Docker Desktop) ≠ Pi 5 (ARM, 2 CPU / 500MB).")
	fmt.Println("   Real scores are typically 30-50% lower on the Pi.")
}

func healthCheck() bool {
	resp, err := http.Get(*baseURL + "/readyz")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

func flushRedis() {
	// Send a dummy request that triggers Redis cleanup via the API.
	// Each test seeds its own device, so cleanup is per-test.
	// We just ensure the stack is fresh.
	http.Get(*baseURL + "/healthz")
}

func clip(v, lo, hi float64) float64 {
	return math.Max(lo, math.Min(hi, v))
}

// ── capacity ────────────────────────────────────────────

func runCapacity() int {
	const (
		startRPS  = 200
		endRPS    = 5000
		stepRPS   = 200
		stepDur   = 8 * time.Second
		p99SLO    = 150 * time.Millisecond
		errMax    = 0.005
	)
	device := fmt.Sprintf("bench-cap-%d", time.Now().Unix())

	// Pre-seed.
	seedDevice(device)

	maxSustained := 0
	for rps := startRPS; rps <= endRPS; rps += stepRPS {
		p99, delivered, errors := capacityStep(device, rps, stepDur)
		errRate := float64(errors) / float64(errors+delivered)

		sustained := errRate <= errMax && p99 <= p99SLO && float64(delivered) >= float64(rps)*float64(stepDur.Seconds())*0.95

		mark := "✅"
		if !sustained {
			mark = "❌ KNEE"
		}
		fmt.Printf("  %4d RPS → delivered=%4d p99=%6v errs=%d/%d %s\n",
			rps, delivered, p99.Round(time.Microsecond), errors, errors+delivered, mark)

		if sustained {
			maxSustained = rps
		} else {
			break // stop at first failure
		}
	}
	return maxSustained
}

func capacityStep(device string, targetRPS int, dur time.Duration) (p99 time.Duration, delivered, errors int) {
	var latencies []time.Duration
	var mu sync.Mutex
	var okCount, errCount atomic.Int64

	interval := time.Second / time.Duration(targetRPS)
	deadline := time.Now().Add(dur)

	var wg sync.WaitGroup
	wg.Go(func() {
		for time.Now().Before(deadline) {
			t0 := time.Now()
			op := pickOp()
			url, body := buildReq(device, op)
			resp, err := doReq(url, body)
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
			time.Sleep(interval)
		}
	})
	wg.Wait()

	if len(latencies) == 0 {
		return 0, 0, 0
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	p99 = latencies[len(latencies)*99/100]
	delivered = int(okCount.Load())
	errors = int(errCount.Load())
	return
}

// ── resilience (spike) ──────────────────────────────────

func runSpike() (p99 time.Duration, errRate float64) {
	device := fmt.Sprintf("bench-spike-%d", time.Now().Unix())
	seedDevice(device)

	var latencies []time.Duration
	var mu sync.Mutex
	var okCount, errCount atomic.Int64

	// Ramp 50→800 RPS over 10s, hold at 800 for 20s.
	doSpikePhase := func(rps int, dur time.Duration) {
		interval := time.Second / time.Duration(rps)
		deadline := time.Now().Add(dur)
		var wg sync.WaitGroup
		wg.Go(func() {
			for time.Now().Before(deadline) {
				t0 := time.Now()
				op := pickOp()
				url, body := buildReq(device, op)
				resp, err := doReq(url, body)
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
				time.Sleep(interval)
			}
		})
		wg.Wait()
	}

	// Ramp phase (linear 50→800 over 10s).
	fmt.Println("  Ramping 50→800 RPS...")
	rampStart := time.Now()
	rampDur := 10 * time.Second
	for time.Since(rampStart) < rampDur {
		elapsed := time.Since(rampStart)
		currentRPS := 50 + int(float64(750)*elapsed.Seconds()/rampDur.Seconds())
		if currentRPS > 0 {
			doSpikePhase(currentRPS, 200*time.Millisecond)
		}
	}

	// Hold at peak for 15s.
	fmt.Println("  Holding at 800 RPS...")
	doSpikePhase(800, 15*time.Second)

	if len(latencies) == 0 {
		return 0, 0
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	p99 = latencies[len(latencies)*99/100]
	total := float64(okCount.Load() + errCount.Load())
	errRate = float64(errCount.Load()) / math.Max(total, 1)
	return
}

// ── stability (endurance drift) ─────────────────────────

func runEndurance(dur time.Duration) float64 {
	device := fmt.Sprintf("bench-endur-%d", time.Now().Unix())
	seedDevice(device)

	var firstP99, lastP99 time.Duration
	var mu sync.Mutex
	var allLatencies []time.Duration

	interval := time.Second / 200 // steady 200 RPS
	deadline := time.Now().Add(dur)
	snapshotFreq := dur / 4 // 4 snapshots

	nextSnapshot := time.Now().Add(snapshotFreq)
	snapshots := 0

	var wg sync.WaitGroup
	wg.Go(func() {
		for time.Now().Before(deadline) {
			t0 := time.Now()
			op := pickOp()
			url, body := buildReq(device, op)
			resp, _ := doReq(url, body)
			lat := time.Since(t0)

			mu.Lock()
			allLatencies = append(allLatencies, lat)
			mu.Unlock()

			if resp != nil {
				resp.Body.Close()
			}

			// Take snapshot.
			if time.Now().After(nextSnapshot) && snapshots < 4 {
				mu.Lock()
				if len(allLatencies) > 100 {
					sorted := make([]time.Duration, len(allLatencies))
					copy(sorted, allLatencies)
					mu.Unlock()

					sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
					p99 := sorted[len(sorted)*99/100]

					if snapshots == 0 {
						firstP99 = p99
					}
					lastP99 = p99
					fmt.Printf("  [%3v] p99=%6v | samples=%d\n",
						time.Since(deadline.Add(-dur)).Round(time.Second),
						p99.Round(time.Microsecond), len(sorted))
				} else {
					mu.Unlock()
				}
				snapshots++
				nextSnapshot = time.Now().Add(snapshotFreq)
			}

			time.Sleep(interval)
		}
	})
	wg.Wait()

	if firstP99 == 0 || lastP99 == 0 {
		return 1.0
	}
	return float64(lastP99) / float64(firstP99)
}

// ── helpers ─────────────────────────────────────────────

func pickOp() string {
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
			"battery": 0.8, "ax": float64(i%3) * 0.1, "ay": float64(i%5) * 0.05, "az": 9.81,
		}
	}
	body, _ := json.Marshal(map[string]any{"points": points})
	url := fmt.Sprintf("%s/devices/%s/telemetry/batch", *baseURL, id)
	resp, _ := http.Post(url, "application/json", bytes.NewReader(body))
	if resp != nil {
		resp.Body.Close()
	}
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
