.PHONY: test test-unit test-integration test-stress bench lint build docker-build clean

# ── testing ──────────────────────────────────────────────
test: test-unit

test-unit:
	go test -count=1 -short -race ./internal/...

test-integration:
	go test -count=1 -run Integration ./internal/...

test-stress:
	go test -tags=stress -count=1 -timeout=30m ./stress/

# ── benchmarks ───────────────────────────────────────────
bench:
	go test -bench=. -benchmem -benchtime=5s ./internal/...

bench-profile:
	mkdir -p profiles
	go test -bench=. -benchmem -benchtime=10s \
		-cpuprofile=profiles/cpu.prof \
		-memprofile=profiles/mem.prof \
		-blockprofile=profiles/block.prof \
		./internal/...

# ── quality ──────────────────────────────────────────────
lint:
	go vet ./...
	go run honnef.co/go/tools/cmd/staticcheck@latest ./...

# ── build ────────────────────────────────────────────────
build:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/api ./cmd/api/

build-linux-arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
		go build -ldflags="-s -w" -o bin/api-arm64 ./cmd/api/

docker-build:
	docker buildx build --platform linux/arm64 -t api:latest .

# ── local run ────────────────────────────────────────────
run:
	$(MAKE) build
	INSTANCE_ID=dev-1 REDIS_ADDR=localhost:6379 ./bin/api

# ── stress tests (match benchmark scenarios) ─────────────
up:
	docker compose up -d --build

steady:
	go run ./stress/cmd/steady/ -url http://localhost:8080

spike:
	go run ./stress/cmd/spike/ -url http://localhost:8080

endurance:
	go run ./stress/cmd/endurance/ -url http://localhost:8080 -dur 5m

capacity:
	go run ./stress/cmd/throughput/ -url http://localhost:8080 -start 200 -end 5000 -step 200 -dur 8s

concurrent:
	go run ./stress/cmd/concurrent/ -url http://localhost:8080 -workers 50 -dur 30s

# Full local benchmark: capacity + resilience + stability → score estimate
bench-full:
	go run ./stress/cmd/bench/ -url http://localhost:8080

down:
	docker compose down -v

logs:
	docker compose logs -f

# ── smoke / load tests (via k6, requires challenge repo) ──
smoke:
	docker compose up -d --build
	sleep 3
	k6 run ../the_500mb_club_challenge/test/smoke.js
	docker compose down -v

test-k6:
	docker compose up -d --build
	sleep 3
	k6 run ../the_500mb_club_challenge/test/test.js
	docker compose down -v

# ── clean ────────────────────────────────────────────────
clean:
	rm -rf bin/ profiles/
