package telemetry

import (
	"context"
	"sync"
	"time"
)

const (
	defaultBufferSize = 128
	defaultMaxWait    = 10 * time.Millisecond
	maxPooledCap      = 256
)

// WriteBuffer accumulates single-point writes and flushes them as a Redis pipeline.
// Stays strictly bounded: max 128 entries (~7 KB per instance).
// Points are pre-encoded to 56-byte binary at Add time — zero work at flush.
type WriteBuffer struct {
	store   *Store
	maxSize int
	maxWait time.Duration
	mu      sync.Mutex
	entries []bufferEntry
	timer   *time.Timer
	closed  bool
}

type bufferEntry struct {
	deviceID string
	score    int64
	data     []byte // 56-byte pre-encoded point
}

// NewWriteBuffer creates a bounded write buffer. maxSize caps entries (default 64).
func NewWriteBuffer(store *Store) *WriteBuffer {
	return &WriteBuffer{
		store:   store,
		maxSize: defaultBufferSize,
		maxWait: defaultMaxWait,
	}
}

// Add queues a single point, pre-encoding it to 56-byte binary immediately.
// Returns immediately — persistence is async (spec allows this for 202 responses).
func (wb *WriteBuffer) Add(ctx context.Context, deviceID string, p TelemetryPoint) {
	// Pre-encode: one allocation now saves encode+pool ops at flush time.
	buf := GetPointBuf()
	p.EncodeInto(*buf)
	entry := bufferEntry{
		deviceID: deviceID,
		score:    p.TS,
		data:     make([]byte, PointSize),
	}
	copy(entry.data, *buf)
	PutPointBuf(buf)

	wb.mu.Lock()
	if wb.closed {
		wb.mu.Unlock()
		return
	}

	wb.entries = append(wb.entries, entry)

	if len(wb.entries) >= wb.maxSize {
		wb.flushLocked(ctx)
	} else if wb.timer == nil {
		wb.timer = time.AfterFunc(wb.maxWait, func() {
			wb.mu.Lock()
			if !wb.closed && len(wb.entries) > 0 {
				wb.flushLocked(context.Background())
			}
			wb.mu.Unlock()
		})
	}
	wb.mu.Unlock()
}

// Flush forces an immediate flush of all buffered entries.
func (wb *WriteBuffer) Flush(ctx context.Context) {
	wb.mu.Lock()
	defer wb.mu.Unlock()
	if len(wb.entries) > 0 {
		wb.flushLocked(ctx)
	}
}

// Close flushes remaining entries and stops the timer.
func (wb *WriteBuffer) Close() {
	wb.mu.Lock()
	defer wb.mu.Unlock()
	wb.closed = true
	if wb.timer != nil {
		wb.timer.Stop()
		wb.timer = nil
	}
	if len(wb.entries) > 0 {
		wb.flushLocked(context.Background())
	}
}

func (wb *WriteBuffer) flushLocked(ctx context.Context) {
	if len(wb.entries) == 0 {
		return
	}

	// Stop timer if running.
	if wb.timer != nil {
		wb.timer.Stop()
		wb.timer = nil
	}

	// Take ownership of the entries slice and reset.
	entries := wb.entries
	wb.entries = wb.acquireBuf()

	// Flush outside the lock to avoid blocking new writes.
	wb.mu.Unlock()
	wb.flushEntries(ctx, entries)
	wb.mu.Lock()
}

func (wb *WriteBuffer) flushEntries(ctx context.Context, entries []bufferEntry) {
	pipe, err := wb.store.client.Pipeline(ctx)
	if err != nil {
		return
	}

	// Queue all ZADDs using pre-encoded data, then trim each device.
	seen := make(map[string]bool, 16)
	for _, e := range entries {
		pipe.ZADD(deviceKey(e.deviceID), e.score, e.data)
		seen[e.deviceID] = true
	}

	// Trim each device to its newest retainPerDevice points.
	// Rides the same pipeline — no extra round-trip.
	for dev := range seen {
		pipe.ZREMRANGEBYRANK(deviceKey(dev), 0, -(retainPerDevice + 1))
	}

	pipe.Exec(ctx)
}

// acquireBuf returns a pooled buffer if available, or a fresh one.
func (wb *WriteBuffer) acquireBuf() []bufferEntry {
	// Cap at maxPooledCap entries to prevent unbounded growth.
	cap := wb.maxSize
	if cap > maxPooledCap {
		cap = maxPooledCap
	}
	return make([]bufferEntry, 0, cap)
}

// Len returns the current number of buffered entries (for testing/metrics).
func (wb *WriteBuffer) Len() int {
	wb.mu.Lock()
	defer wb.mu.Unlock()
	return len(wb.entries)
}
