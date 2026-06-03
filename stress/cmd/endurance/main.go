// Endurance test — matches the benchmark's "endurance" scenario.
// Sustained load, measures drift over time.
// Usage: go run ./stress/cmd/endurance/ -dur 5m

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
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
	duration   = flag.Duration("dur", 5*time.Minute, "Test duration")
	reportFreq = flag.Duration("report", 30*time.Second, "Report frequency")
	rps        = flag.Int("rps", 200, "Target RPS")
	workers    = flag.Int("workers", 4, "Number of goroutines")
)

type snapshot struct {
	elapsed   time.Duration
	latencies []time.Duration
	errors    int64
	ok        int64
}

func main() {
	flag.Parse()
	device := fmt.Sprintf("endurance-%d", time.Now().Unix())

	fmt.Println("╔══════════════════════════════════════════════╗")
	fmt.Println("║   ENDURANCE TEST (matches benchmark)        ║")
	fmt.Println("╚══════════════════════════════════════════════╝")
	fmt.Printf("URL: %s  |  Duration: %v  |  RPS: %d  |  Workers: %d  |  Report: %v\n\n", *baseURL, *duration, *rps, *workers, *reportFreq)

	seedDevice(device)

	var (
		snapshots    []snapshot
		curLatencies []time.Duration
		mu           sync.Mutex
		errCount     atomic.Int64
		okCount      atomic.Int64
		start        = time.Now()
	)

	// Reporter goroutine — takes a snapshot every reportFreq.
	go func() {
		ticker := time.NewTicker(*reportFreq)
		defer ticker.Stop()
		for range ticker.C {
			mu.Lock()
			s := snapshot{
				elapsed:   time.Since(start),
				errors:    errCount.Load(),
				ok:        okCount.Load(),
				latencies: curLatencies,
			}
			curLatencies = nil
			snapshots = append(snapshots, s)
			mu.Unlock()
			printSnapshot(s)
		}
	}()

	// Fire at steady rate with multiple workers.
	var loadWG sync.WaitGroup
	n := *workers
	workerRPS := *rps / n
	interval := time.Second / time.Duration(workerRPS)
	deadline := start.Add(*duration)

	for range n {
		loadWG.Go(func() {
			for time.Now().Before(deadline) {
				t0 := time.Now()
				op := pickOp()
				url, body := buildReq(device, op)
				resp, err := doReq(url, body)
				lat := time.Since(t0)

				mu.Lock()
				curLatencies = append(curLatencies, lat)
				mu.Unlock()

				if err != nil || (resp != nil && resp.StatusCode >= 400) {
					errCount.Add(1)
				} else {
					okCount.Add(1)
				}
				drainClose(resp)
				time.Sleep(interval)
			}
		})
	}
	loadWG.Wait()

	// Final snapshot for remaining.
	mu.Lock()
	snapshots = append(snapshots, snapshot{
		elapsed:   time.Since(start),
		errors:    errCount.Load(),
		ok:        okCount.Load(),
		latencies: curLatencies,
	})
	mu.Unlock()

	// Drift analysis.
	if len(snapshots) >= 2 {
		first := &snapshots[0]
		last := &snapshots[len(snapshots)-1]

		fmt.Printf("\n╔══════════════════════════════════════════════╗\n")
		fmt.Printf("║  DRIFT ANALYSIS                             ║\n")
		fmt.Printf("╠══════════════════════════════════════════════╣\n")

		if len(first.latencies) > 0 && len(last.latencies) > 0 {
			sortFn(first.latencies)
			sortFn(last.latencies)
			fp99 := first.latencies[len(first.latencies)*99/100]
			lp99 := last.latencies[len(last.latencies)*99/100]
			drift := float64(lp99) / math.Max(float64(fp99), 1)
			fmt.Printf("║  p99 first:  %-10v                       ║\n", fp99.Round(time.Microsecond))
			fmt.Printf("║  p99 last:   %-10v                       ║\n", lp99.Round(time.Microsecond))
			fmt.Printf("║  Drift:      %.2fx (target <1.10)          ║\n", drift)
			st := "✅ STABLE"
			if drift > 1.10 { st = "❌ DEGRADED" }
			fmt.Printf("║  Status:     %s                           ║\n", st)
		}

		total := last.ok + last.errors
		errRate := float64(last.errors) / math.Max(float64(total), 1)
		fmt.Printf("║  Total:      %-8d                          ║\n", total)
		fmt.Printf("║  Errors:     %-8d (%.2f%%)                 ║\n", last.errors, errRate*100)
		fmt.Printf("╚══════════════════════════════════════════════╝\n")
	}
}

func printSnapshot(s snapshot) {
	if len(s.latencies) == 0 {
		fmt.Printf("[%6v] (no data yet)\n", s.elapsed.Round(time.Second))
		return
	}
	sortFn(s.latencies)
	p99 := s.latencies[len(s.latencies)*99/100]
	fmt.Printf("[%6v] p99=%6v | reqs=%d | errs=%d\n",
		s.elapsed.Round(time.Second), p99.Round(time.Microsecond), s.ok, s.errors)
}

func sortFn(d []time.Duration) { sort.Slice(d, func(i, j int) bool { return d[i] < d[j] }) }

func pickOp() string {
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
		points[i] = map[string]any{"ts": base - int64((256-i)*100), "lat": -23.55, "lon": -46.63, "battery": 0.8, "ax": 0.1, "ay": -0.04, "az": 9.81}
	}
	body, _ := json.Marshal(map[string]any{"points": points})
	resp, _ := http.Post(fmt.Sprintf("%s/devices/%s/telemetry/batch", *baseURL, id), "application/json", bytes.NewReader(body))
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

func drainClose(resp *http.Response) {
	if resp != nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

func doReq(url string, body []byte) (*http.Response, error) {
	if body != nil {
		return http.Post(url, "application/json", bytes.NewReader(body))
	}
	return http.Get(url)
}
