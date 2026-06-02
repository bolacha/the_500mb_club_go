# The 500MB Club — Go Implementation

Go 1.26 implementation of the [500MB Club Challenge](https://github.com/gandarez/the_500mb_club_challenge) telemetry API.

**Stack:** Go + Redis + Nginx | **Deps:** Zero external libraries | **Target:** 500 MB / 2 CPU

## Quick Start

```bash
# Run tests
make test

# Run benchmarks
make bench

# Build and run locally (needs Redis on localhost:6379)
make run

# Full stack with docker compose
make up

# Smoke test against the stack
make smoke
```

## Architecture

- `net/http` with enhanced ServeMux (Go 1.22+) — no external router
- `encoding/json` (stdlib) — no external JSON library
- Custom RESP2 Redis client (~200 lines) — no redis driver dependency
- `log/slog` — structured logging from stdlib
- GC tuned: `GOGC=off` + `GOMEMLIMIT=70MiB` + periodic manual GC

## Project Structure

```
cmd/api/          — Entry point, server setup, graceful shutdown
internal/
  handler/         — HTTP handlers + ServeMux routing
  redis/           — Minimal RESP2 Redis client + pool + pipeline
  telemetry/       — Point type, validation, binary encode/decode, storage
  anomaly/         — Welford single-pass z-score algorithm
  middleware/      — X-Instance-Id, request logging
stress/           — End-to-end stress tests
```
