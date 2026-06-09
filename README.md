# The 500MB Club — Go 1.26 Implementation

> **Zero external dependencies.** Production-grade telemetry API on 2 CPUs / 500 MB.

[![CI](https://github.com/bolacha/the_500mb_club_go/actions/workflows/docker-publish.yml/badge.svg)](https://github.com/bolacha/the_500mb_club_go/actions/workflows/docker-publish.yml)
[![Go 1.26](https://img.shields.io/badge/Go-1.26.3-00ADD8?logo=go)](https://go.dev/)
[![Docker](https://img.shields.io/badge/image-9.3_MB-blue?logo=docker)](https://github.com/bolacha/the_500mb_club_go/pkgs/container/the_500mb_club_go)

## Architecture

```mermaid
flowchart LR
    K6["🧪 k6 Load Test"]

    subgraph Stack["2 CPUs / 500 MB"]
        direction TB
        LB{{"nginx round-robin :8080"}}

        subgraph APIs["Go API replicas"]
            A1["api-1 :8000<br/>GOMEMLIMIT=16MiB"]
            A2["api-2 :8000<br/>GOGC=25"]
            A3["api-3 :8000<br/>GOMAXPROCS=1"]
        end

        R[("Redis 7 Alpine<br/>tmpfs /data")]
    end

    K6 --> LB
    LB --> A1 & A2 & A3
    A1 & A2 & A3 --> R

    classDef store fill:#1f2933,stroke:#7b8794,color:#e4e7eb
    classDef lb fill:#243b53,stroke:#4c63b6,color:#e4e7eb
    class R store
    class LB lb
```

### Stack

| Component | Choice | Why |
|-----------|--------|-----|
| **Language** | Go 1.26.3 | Latest stable, Green Tea GC, small-alloc optimizations |
| **HTTP** | `net/http` (stdlib) | Enhanced ServeMux with `GET /devices/{id}/telemetry` patterns |
| **JSON** | `encoding/json` (stdlib) | Good enough at 200 RPS, zero-allocation with pooled buffers |
| **Redis client** | Custom RESP2 (~270 lines) | Zero dependencies, zero-alloc command writing |
| **Storage** | Redis 7 Alpine | Sorted sets as time-series, pipelining, tmpfs `/data` |
| **Binary** | 56-byte compact encoding | 62% smaller than JSON in Redis |
| **GC** | `GOGC=25` + `GOMEMLIMIT=16MiB` | Frequent small GCs, no pause spikes |
| **Image** | `scratch` base | 9.3 MB, non-root, stripped |

## Real Pi 5 Benchmark Journey

Every change was benchmarked on the **real Raspberry Pi 5** (4 cores, 8 GB RAM, Debian Bookworm ARM64). Results from the [challenge's Pi-Bench daemon](https://github.com/The-500MB-Club/the_500mb_club_challenge).

### p99 Latency Progression (100 RPS × 60s)

```mermaid
xychart-beta
    title "POST p99 Latency Over 7 Iterations"
    x-axis ["#99 base", "#100 CI", "#101 +buf", "#102 +mZADD", "#103 Redis cfg", "#104 stable", "#106 v2"]
    y-axis "milliseconds" 0 --> 3
    bar [2.10, 1.91, 1.63, 1.94, 2.15, 2.05, 1.91]
```

| # | Change | POST p99 | Batch p99 | Range p99 | Anomaly p99 | Errors |
|---|--------|----------|-----------|-----------|-------------|--------|
| **99** | Base (local build) | 2.10ms | 3.73ms | 2.41ms | 3.58ms | 0% |
| **100** | CI build, `-trimpath` | 1.91ms | 3.07ms | 2.76ms | 1.96ms | 0% |
| **101** | + Write-buffer | **1.63ms** 🏆 | 5.07ms | 2.88ms | 1.56ms | 0% |
| **102** | + Multi-ZADD | 1.94ms | 8.89ms ❌ | 2.52ms | 2.99ms | 0% |
| **103** | Redis speed configs | 2.15ms | 6.37ms ❌ | 4.10ms ❌ | 2.63ms | 0% |
| **104** | Back to best | 2.05ms | 15.00ms | 3.37ms | 2.14ms | 0% |
| **106** | **Optimize v2** | 1.91ms | 7.07ms | **2.28ms** 🏆 | 1.76ms | 0% |
| **108** | **+GOMAXPROCS=1** | 1.75ms | **3.68ms** | 2.57ms | 1.91ms | 0% |
| **109** | +Unix socket | 1.92ms | 8.96ms | 2.41ms | 1.76ms | 0% |
| **110** | **+nginx tuning** | 1.84ms | 3.25ms | 2.58ms | 1.92ms | 0% |
| **111** | **+strip cycles** 🏆 | **1.70ms** | 5.73ms | 2.58ms | 2.19ms | 0% |
| **112** | +Redis healthcheck | **1.45ms** 🏆 | 7.95ms | **2.28ms** | 4.84ms | 0% |
| **113** | **Pre-encode + json.Decoder** 🏆 | 1.39ms | **2.37ms** 🏆 | 3.84ms | **1.59ms** 🏆 | **0%** |
| **114** | Config F + nginx tune | 1.39ms | 5.22ms | **2.16ms** 🏆 | 1.91ms | **0%** |
| **124** | Config F (clean rerun) | 1.60ms | 3.02ms | 2.45ms | 1.67ms | **0%** |

> ⚠️ Pi 5 shows ±20% run-to-run variance. Values are single-run p99. All runs had **0% errors**.

## Score Targets

| Dimension | SLO | Best Achieved | Status |
|-----------|-----|---------------|--------|
| POST p99 | < 8ms | **1.45ms** | ✅ 5.5× under |
| Batch p99 | < 25ms | 3.07ms | ✅ 8× under |
| Range p99 | < 15ms | 2.28ms | ✅ 6.5× under |
| Anomaly p99 | < 25ms | 1.56ms | ✅ 16× under |
| Error rate | < 0.5% | **0.00%** | ✅ Perfect |
| **Efficiency** | score 1.0 | **4.00** 🥇 | ✅ Ceiling |
| **Tail latency** | score 1.0 | **1.50** 🥇 | ✅ Ceiling |
| **Capacity** | > 1000 RPS | ⏳ TBD | 🚧 Awaiting bench |

### #113 Pi 5 Improvements vs #112

| Operation | #112 p99 | #113 p99 | Delta |
|-----------|----------|----------|-------|
| POST | 1.45ms | 1.39ms | −4% |
| **BATCH** | 7.95ms | **2.37ms** | **−70%** 🏆 |
| RANGE | 2.28ms | 3.84ms | +68% (Pi variance) |
| **ANOMALY** | 4.84ms | **1.59ms** | **−67%** 🏆 |

### #124 vs #113 (Config F clean rerun, confirms #114 was outlier)

| Operation | #113 p99 | #124 p99 | Delta |
|-----------|----------|----------|-------|
| POST | 1.39ms | 1.60ms | +15% |
| BATCH | 2.37ms | 3.02ms | +27% |
| RANGE | 3.84ms | 2.45ms | −36% |
| ANOMALY | 1.59ms | 1.67ms | +5% |

### All Pi 5 runs at a glance

| Run | POST | BATCH | RANGE | ANOMALY | Eff | Lat |
|-----|------|-------|-------|---------|-----|-----|
| #112 (baseline) | 1.45 | 7.95 | 2.28 | 4.84 | 4.0 | 1.5 |
| **#113** 🏆 (warm-up + pre-encode) | **1.39** | **2.37** | 3.84 | **1.59** | 4.0 | 1.5 |
| #114 (Config F, outlier) | 1.39 | 5.22 | 2.16 | 1.91 | 4.0 | 1.5 |
| #124 (Config F, rerun) | 1.60 | 3.02 | 2.45 | 1.67 | 4.0 | 1.5 |
| **#125** (tie-safe cursor) | 1.50 | 4.66 | **2.04** 🏆 | 1.80 | 4.0 | 1.5 |
| **#130** (bounded retention) 🏆 | **1.33** 🏆 | 4.70 | 2.20 | **1.30** 🏆 | 4.0 | 1.5 |

## Endurance: 2000 RPS × 45 Minutes

Proves the stack can sustain high load indefinitely within the memory budget:

| Metric | Value |
|--------|-------|
| Total requests | **4,486,724** |
| Errors | **0** (0.00%) |
| p99 latency | **2.2ms** — flat across all 45 minutes |
| Latency drift | **None** — p99 identical at 5min, 25min, 45min |
| Redis memory | Cycles 2MB → 143MB → LRU evict → repeat |
| Total RSS | ~170MB peak (34% of 500MB budget) |

```
TIME     REDIS_MB  REQUESTS  ERRORS  P99
  5 min     46.0    498,828      0   2.21ms
 15 min    126.9  1,496,287      0   2.26ms
 25 min     66.6  2,491,700      0   2.25ms
 35 min    143.8  3,488,495      0   2.24ms
 45 min        —  4,486,724      0      —
```

## Recent Optimizations

### Grouped ZADD for batch ingestion

`POST /telemetry/batch` (100 points) now uses a single `ZADD key score1 member1 ... score100 member100` Redis command instead of 100 individual ZADDs in a pipeline. One round-trip regardless of batch size.

### Tie-safe range cursor

Cursor pagination now encodes `timestamp:offset` instead of plain timestamp. When multiple points share the same millisecond timestamp, the offset prevents data loss across page boundaries.

### Redis memory tuning

| Setting | Before | After | Why |
|---------|--------|-------|-----|
| `maxmemory` | 40MB | 150MB | OOM at 6min under 2000 RPS → 45+ min |
| `maxmemory-policy` | `noeviction` | `allkeys-lru` | Rejected writes → evicts oldest data |
| Container `mem_limit` | 50MB | 200MB | Redis overhead + working set |

Budget: Redis 200MB + 3×API 60MB + nginx 20MB = **280MB** (56% of 500MB cap).

### RESP2 array response fix

Fixed a bug where `readBulkString` skipped the `$` type byte when parsing array (ZRANGEBYSCORE/ZREVRANGE) responses. Was silently causing `strconv.Atoi: parsing "$56": invalid syntax` on range and anomaly queries under load.

### Bounded per-device retention

After each write batch, `ZREMRANGEBYRANK` trims the device's sorted set to the newest 1024 points. Rides the same pipeline as writes — zero extra Redis round-trips. Memory bounded at ~57KB per device regardless of RPS or run duration:

```
WriteBuffer flush pipeline:
  ZADD dev-1 score1 m1 ... scoreN mN   ← write points
  ZREMRANGEBYRANK dev-1 0 -(1025)      ← trim to 1024
  ZADD dev-2 ...
  ZREMRANGEBYRANK dev-2 ...
  → one Exec(), one round-trip
```

At 50 devices, total Redis memory stays at ~3MB — never approaches the 150MB maxmemory ceiling even under indefinite 2000 RPS load.

## Key Design Decisions

### 1. Write-Buffer for Single POSTs

```go
// Bounded micro-batch: 128 entries or 10ms timeout.
// Points are pre-encoded to 56-byte binary at Add time —
// zero encoding work at pipeline flush.
type WriteBuffer struct {
    maxSize int           // 128 entries (~7 KB)
    maxWait time.Duration // 10ms
}
```

**Why:** Single POSTs are 60% of traffic. Each ZADD costs one Redis round-trip. Batching 128 into one pipeline saves 127 round-trips per batch. Pre-encoding at Add time eliminates the EncodeInto hot path during flush. The async contract (HTTP 202) allows this.

**Risk mitigation:** Buffer is strictly bounded (128 entries × 56 bytes = 7 KB). GOMEMLIMIT (16 MiB) prevents heap growth. Buffer flushed on shutdown.

### 2. Pipeline > Multi-ZADD for Batch

```go
// ❌ Multi-ZADD: one giant command blocks Redis event loop
client.ZADDM(key, 100 pairs) // batched but serialized

// ✅ Pipeline: 100 small commands, Redis interleaves them
pipe := client.Pipeline()
for _, p := range points { pipe.ZADD(key, p.TS, p.EncodeInto(buf)) }
pipe.Exec()
```

**Why:** Redis is single-threaded. A 100-pair ZADD blocks its event loop, delaying other operations. Pipeline sends 100 individual commands that Redis can interleave with requests from other replicas. Real Pi data: pipeline = 3.07ms p99 vs multi-ZADD = 8.89ms.

### 3. Binary Anomaly Parse (Zero Alloc)

```go
// ❌ Old: decode 256 structs, then compute
points := make([]TelemetryPoint, 256)
for _, raw := range raws { points[i] = DecodeBinary(raw) } // 256 heap allocs
Compute(points)

// ✅ New: parse AX/AY/AZ directly from bytes
func ComputeBinary(rawPoints [][]byte) (Result, error) {
    for _, raw := range rawPoints {
        ax := math.Float64frombits(binary.LittleEndian.Uint64(raw[32:40]))
        ay := math.Float64frombits(binary.LittleEndian.Uint64(raw[40:48]))
        az := math.Float64frombits(binary.LittleEndian.Uint64(raw[48:56]))
        // ... Welford's algorithm
    }
}
```

**Why:** Anomaly is CPU-bound (10% of traffic). Eliminating 256 struct allocations per call reduces GC pressure. With GOGC=25, fewer allocs = fewer GC cycles = lower tail latency.

### 4. Default Redis > Tuned Redis

```yaml
# ✅ Default Redis on Pi 5
command: redis-server --maxmemory 40mb --maxmemory-policy noeviction --save ""

# ❌ Tuned: hz=100 burns 2× CPU, activerehashing=no causes hash collisions
# command: redis-server ... --hz 100 --activerehashing no
```

**Why:** On the Pi 5's 2-CPU budget, `hz=100` (internal clock) consumes precious CPU cycles. `activerehashing=no` causes hash table collisions to accumulate, slowing lookups. Default Redis is tuned for general-purpose use and works best on constrained hardware.

### 5. GC: GOGC=25 + GOMEMLIMIT

```go
debug.SetMemoryLimit(16 << 20) // 16 MiB soft cap
debug.SetGCPercent(25)         // frequent small GCs
```

**Why:** With `mem_limit=20m` per container, the heap must stay under 16 MiB. `GOGC=25` triggers GC at 1.25× live heap (vs 100's 2×). Smaller, more frequent GCs avoid the latency spikes of large GC cycles. `GOMEMLIMIT` is the hard backstop.

### 6. GOMAXPROCS=1 — Avoiding CFS Throttling

```yaml
environment:
  - GOMAXPROCS=1
```

**Why:** With `cpus: 0.55` per container, Linux CFS allocates ~55% of one core. Go's default sees all 4 host cores, spawning threads that CFS must preempt — causing latency spikes. `GOMAXPROCS=1` matches threads to quota. **Real Pi result: batch p99 dropped 48% (7.07ms → 3.68ms).**

### 7. Nginx Tuning — Keepalive + Buffering

```nginx
upstream api { keepalive 32; }
proxy_buffering off;
tcp_nodelay on;
proxy_socket_keepalive on;
```

**Why:** Default nginx opens a new TCP connection to the backend per request. `keepalive 32` reuses connections, eliminating TCP handshake overhead. `proxy_buffering off` passes data without copying through nginx's buffer. **Docker Desktop result: spike p99 dropped 38% (7.5ms → 4.7ms).** Unix socket was also tested but showed no benefit on real Pi 5.

### 8. Anomaly Detection — How It Works

```
GET /devices/{id}/anomaly

k6 request → API fetches last 256 raw bytes from Redis (ZREVRANGE)
           → parses ax/ay/az directly from binary (zero alloc)
           → computes magnitude = √(ax² + ay² + az²) per point
           → runs Welford's single-pass algorithm (numerically stable)
           → returns z-score, mean, stddev, anomalous flag
```

| Field | Meaning |
|-------|---------|
| `z_score` | Standard deviations from the mean. \|z\| > 3 = anomalous |
| `anomalous` | `true` if \|z\| > 3 (likely crash or impact event) |
| `samples` | Points used (0-256). < 8 returns HTTP 404 |
| `mean` | Average magnitude (~9.81 m/s² = Earth's gravity) |
| `stddev` | Standard deviation (near 0 for stable sensors) |

The challenge spec mandates exactly 256 points per call — no caching allowed. Our zero-alloc binary parse computes it in **1.3µs per call** (M1 Max), well under the p99 target.

## Budget

| Service | CPU | Memory | Actual RSS (load) |
|---------|-----|--------|-------------------|
| api-1 | 0.55 | 20 MB | ~12 MB |
| api-2 | 0.55 | 20 MB | ~12 MB |
| api-3 | 0.55 | 20 MB | ~12 MB |
| Redis | 0.20 | 50 MB | ~17 MB |
| Nginx | 0.15 | 20 MB | ~4 MB |
| **Total** | **2.00** | **130 MB** | **~57 MB** |

Only 44% of the 130 MB allocation is used under 100 RPS load. 370+ MB of headroom for spikes.

## Project Structure

```
cmd/api/main.go                  # Server, GC tuning, graceful shutdown
internal/
  handler/                       # HTTP handlers (ServeMux routing)
  redis/                         # Custom RESP2 client + pool + pipeline
  telemetry/                     # Point type, binary encode, store, write buffer
  anomaly/                       # Zero-alloc Welford z-score
  middleware/                    # X-Instance-Id, request logging
stress/cmd/
  steady/                        # 200 RPS, 60s (matches benchmark steady)
  spike/                         # 50→800 RPS (matches benchmark spike)
  throughput/                    # 200→3000 RPS ramp (matches benchmark capacity)
  endurance/                     # Sustained load with drift analysis
  concurrent/                    # Configurable worker burst
```

## Local Development

```bash
# Prerequisites: Docker, Go 1.26, k6
make up          # Start stack (docker compose)
make steady      # 200 RPS, 60s
make spike       # 50→800 RPS burst
make capacity    # 200→2000 RPS ramp
make endurance   # 5-minute sustained
make test        # Unit tests (32 passing)
make bench       # Benchmarks
make down        # Stop stack
```

## Benchmarks (M1 Max)

| Operation | Time | Allocs |
|-----------|------|--------|
| Anomaly (256 pts, binary) | 1,310 ns | **0** |
| Point encode (56 B) | 17 ns | 1 |
| Point encode into buf | 2.2 ns | **0** |
| Point decode binary | 4.8 ns | **0** |
| Point validate | 3.0 ns | **0** |
| JSON decode | 1,290 ns | 5 |

## CI Pipeline

```mermaid
flowchart LR
    Push["git push main"] --> CI["GitHub Actions"]
    CI --> Build["Build arm64 binary<br/>(QEMU, -trimpath, -ldflags)"]
    Build --> PushImage["Push to GHCR<br/>ghcr.io/bolacha/the_500mb_club_go"]
    PushImage --> Issue["Open test/go issue"]
    Issue --> Gate["Gate validates"]
    Gate --> Pi["Pi-Bench daemon<br/>runs on Raspberry Pi 5"]
    Pi --> Results["Results posted<br/>on issue"]
```

Every push to `main` triggers an automatic build and GHCR push. To re-benchmark on the real Pi 5, open a `test/go` issue in the [challenge repo](https://github.com/The-500MB-Club/the_500mb_club_challenge).

## License

MIT

## Local Testing

Run any scenario against your local Docker stack:

```bash
make up              # Start the stack (Docker Compose)
make steady          # Steady-state: 200 RPS, 60s, full op mix
make spike           # Spike: 50→800 RPS burst
make capacity        # Capacity: step ramp 200→10000, finds knee
make endurance       # Endurance: 5 min sustained, drift analysis
make bench-full      # All 3 unscored dimensions → score estimate
make down            # Stop stack
```

### Multi-worker mode

All test commands accept a `-workers` flag to simulate multiple k6 instances:

```bash
go run ./stress/cmd/throughput/ -url http://localhost:8080 -workers 8
go run ./stress/cmd/endurance/ -url http://localhost:8080 -workers 4 -rps 200
go run ./stress/cmd/spike/ -url http://localhost:8080 -workers 4 -peak 1600
```

### Local benchmark results (Docker Desktop, M1 Max, Go 1.26.3)

#### Capacity (ramp 200→10000 RPS, 8s/step)

| RPS | Delivered | p50 | p99 | Errors | Status |
|-----|-----------|-----|-----|--------|--------|
| 200 | 1,378 | 1.78ms | 9.16ms | 0.14% | ✅ |
| 1000 | 6,016 | 813µs | 6.33ms | 0% | ✅ |
| 2000 | 9,935 | 732µs | 6.03ms | 0% | ✅ |
| 4000 | 10,007 | 1.13ms | 54.5ms | 0% | ✅ |
| 6000 | 9,317 | 2.14ms | 68.5ms | 0% | ✅ |
| 8000 | 8,317 | 4.31ms | 76.5ms | 0% | ✅ |
| **8600** 🏆 | 8,606 | 3.59ms | 78.9ms | **0%** | ✅ KNEE |

> **Max sustained RPS: 8,600** (0 errors, p99 < 150ms, 95%+ delivery). Above 8,600, the single-process test harness can't deliver enough traffic — the API itself still shows 0 errors but delivery drops below 95%.

#### Endurance (200 RPS, 5 min, 4 workers)

| Time | p99 | Requests | Errors |
|------|-----|----------|--------|
| 1 min | 10.7ms | 10,179 | 1 |
| 2 min | 10.8ms | 20,311 | 1 |
| 3 min | 11.9ms | 30,408 | 1 |
| 4 min | 11.7ms | 40,482 | 1 |
| 5 min | 10.8ms | **50,511** | **1** (0.002%) |

> **Drift: 1.01×** ✅ (well under 1.10 target). Zero degradation over 5 minutes of sustained load.

#### Spike (50→1600 RPS, 4 workers)

| Metric | Value | Target | Status |
|--------|-------|--------|--------|
| p99 | 4.96ms | < 12ms | ✅ |
| Errors | 9.9% (harness limit) | < 1% | ⚠️ |

> Spike p99 is well under the 12ms target. Error rate is inflated by single-goroutine test harness — real k6 from external machine doesn't have this limit.

### Important: Redis OOM between tests

The capacity ramp can fill Redis's 40MB limit (~700K points). Always run `docker compose down -v && make up` between test scenarios to reset Redis, or add a `FLUSHALL`:

```bash
docker exec the_500mb_club_go-storage-1 redis-cli FLUSHALL
```

## CPU Budget Tuning

The capacity test exposes which component throttles first. Docker Desktop virtualizes CPU limits, but trends are directionally correct for the Pi 5.

### Configurations tested (capacity ramp 200→10000, 5s/step)

| Config | nginx | api×3 | redis | Total | Max RPS | Notes |
|--------|-------|-------|-------|-------|---------|-------|
| **A** (original) | 0.15 | 0.55 | 0.20 | 2.00 | **8400** ✅ | Current `main` |
| B | 0.25 | 0.50 | 0.20 | 1.95 | 8000 | nginx gain < api loss |
| C | 0.35 | 0.45 | 0.20 | 1.90 | 8000 | Unstable, multiple knees |
| **D** | 0.20 | 0.45 | 0.30 | 1.85 | **8400** ✅ | Redis boost compensates |
| E | 0.15 | 0.50 | 0.30 | 1.95 | 8200 | APIs starved at 0.50 |
| **F** | 0.20 | 0.50 | 0.25 | 1.95 | **8400** ✅ | Best balance |

### What the numbers mean

- **APIs below 0.50 hurt consistently** — Configs C (0.45) and D (0.45) had knee instability even when total RPS was high. The JSON encode/decode + anomaly compute need CPU headroom.
- **Redis above 0.20 helps** — Configs D (0.30) and F (0.25) matched or beat the original despite lower API CPU. Redis single-threaded event loop benefits from not being CFS-preempted at high write volume.
- **nginx above 0.20 doesn't help on Docker** — Configs B+C gave nginx extra CPU but total RPS dropped. nginx's bottleneck here is virtualized networking, not CPU.
- **Config A (original) ties for first** — 0.55 API + 0.15 nginx + 0.20 redis is already well-balanced for this workload mix (60% POST, 10% batch, 20% range, 10% anomaly).

### Pi 5 projection

On bare-metal ARM Linux, CFS throttling is real and measured in microseconds of stolen CPU time. The Docker Desktop VM masks this. On the Pi:

| Change | Expected effect |
|--------|----------------|
| nginx 0.15→0.20 | Later CFS throttling during capacity ramp. nginx hit 15% CPU at 3000+ RPS on Docker — at 0.15 that's right at the limit. 0.20 gives 33% more headroom. |
| API 0.55→0.50 | Safe. `GOMAXPROCS=1` means 0.50 vs 0.55 is irrelevant — one thread can't use more than one core's worth of time slice anyway. |
| Redis 0.20→0.25 | Higher single-thread Redis throughput before CFS kicks in. Worth testing on Pi. |

**Recommended for Pi benchmark:** Config F (`nginx=0.20, api=0.50, redis=0.25, total=1.95`). It preserves API headroom while buffering nginx and Redis against CFS throttling.

### Verdict

**#113 (pre-encode + warm-up) is the best Pi 5 run** — BATCH p99 −70%, ANOMALY p99 −67%. #124 confirmed Config F doesn't regress (#114's BATCH=5.22ms was Pi variance).

**Final config**: Code from #113 (pre-encode WriteBuffer, Redis warm-up, json.NewDecoder) + Config F budget (`nginx=0.20, api=0.50, redis=0.25, total=1.95`) + tuned nginx.conf (`keepalive 128`, `worker_connections 8192`). All 4 Pi runs: Efficiency **4.00** 🥇, Tail Latency **1.50** 🥇 — both at ceiling. Real score awaits capacity/spike/endurance benchmarks on the Pi.
