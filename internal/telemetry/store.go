package telemetry

import (
	"context"
	"fmt"

	"github.com/bolacha/the_500mb_club_go/internal/redis"
)

// Store handles Redis-backed telemetry point storage.
type Store struct {
	client *redis.Client
}

// NewStore creates a new store backed by the given Redis client.
func NewStore(client *redis.Client) *Store {
	return &Store{client: client}
}

func deviceKey(id string) string {
	return "telemetry:" + id
}

// IngestSingle stores a single telemetry point in the device's sorted set.
func (s *Store) IngestSingle(ctx context.Context, deviceID string, p TelemetryPoint) error {
	buf := GetPointBuf()
	p.EncodeInto(*buf)
	err := s.client.ZADD(ctx, deviceKey(deviceID), p.TS, *buf)
	PutPointBuf(buf)
	return err
}

// IngestBatch stores a batch of telemetry points using a single ZADD command
// with multiple score/member pairs — one Redis round trip, one command parse.
func (s *Store) IngestBatch(ctx context.Context, deviceID string, points []TelemetryPoint) (int, error) {
	pairs := make([]redis.ZADDPair, len(points))
	// Keep buffers alive until after the Redis call completes.
	bufs := make([]*[]byte, len(points))
	for i := range points {
		buf := GetPointBuf()
		points[i].EncodeInto(*buf)
		pairs[i] = redis.ZADDPair{Score: points[i].TS, Member: *buf}
		bufs[i] = buf
	}

	err := s.client.ZADDM(ctx, deviceKey(deviceID), pairs)

	// Return buffers to pool after Redis call completes.
	for _, b := range bufs {
		PutPointBuf(b)
	}

	if err != nil {
		return 0, fmt.Errorf("zaddm: %w", err)
	}
	return len(points), nil
}

// Query returns telemetry points within [from, to] (inclusive), ordered by ts ascending.
// Supports cursor-based pagination: pass the last returned ts as cursor for the next page.
// If cursor is non-zero, points with ts <= cursor are excluded.
func (s *Store) Query(ctx context.Context, deviceID string, from, to int64, limit int, cursor int64) ([]TelemetryPoint, error) {
	// With cursor, we use (cursor as exclusive lower bound.
	min := from
	if cursor > 0 && cursor > min {
		// Use exclusive range: add 1 to skip the cursor value itself.
		min = cursor + 1
	}

	raw, err := s.client.ZRANGEBYSCORE(ctx, deviceKey(deviceID), min, to, 0, limit)
	if err != nil {
		return nil, fmt.Errorf("zrangebyscore: %w", err)
	}

	points := make([]TelemetryPoint, len(raw))
	for i, b := range raw {
		points[i] = DecodeBinary(b)
	}
	return points, nil
}

// LastN returns the last n telemetry points for a device (most recent first via ZREVRANGE).
func (s *Store) LastN(ctx context.Context, deviceID string, n int) ([]TelemetryPoint, error) {
	raw, err := s.client.ZREVRANGE(ctx, deviceKey(deviceID), 0, n-1)
	if err != nil {
		return nil, fmt.Errorf("zrevrange: %w", err)
	}

	// ZREVRANGE returns from highest to lowest score (newest first).
	// Reverse to chronological order for the anomaly calculation.
	points := make([]TelemetryPoint, len(raw))
	for i, b := range raw {
		points[len(raw)-1-i] = DecodeBinary(b)
	}
	return points, nil
}
