// Package anomaly implements anomaly detection using z-score over the acceleration magnitude.
// Uses Welford's online algorithm for a single-pass, numerically stable computation.
// Parses raw binary blobs directly — zero allocations. No caching (as required by spec).
package anomaly

import (
	"encoding/binary"
	"fmt"
	"math"
)

// Result holds the z-score computation output.
type Result struct {
	ZScore    float64 `json:"z_score"`
	Samples   int     `json:"samples"`
	Anomalous bool    `json:"anomalous"`
	Mean      float64 `json:"mean"`
	Stddev    float64 `json:"stddev"`
}

const (
	minSamples   = 8    // minimum points required for meaningful computation
	anomalyThres = 3.0  // |z| > 3 → anomalous
)

// ComputeBinary parses raw 56-byte binary encoded points directly — zero allocations.
// Points are in chronological order (oldest first). The last point is the "most recent".
func ComputeBinary(rawPoints [][]byte) (Result, error) {
	n := len(rawPoints)
	if n < minSamples {
		return Result{Samples: n}, fmt.Errorf("insufficient samples: %d < %d", n, minSamples)
	}

	// Single-pass Welford's algorithm — parse AX/AY/AZ directly from bytes.
	var mean, m2 float64
	for i, raw := range rawPoints {
		ax := math.Float64frombits(binary.LittleEndian.Uint64(raw[32:40]))
		ay := math.Float64frombits(binary.LittleEndian.Uint64(raw[40:48]))
		az := math.Float64frombits(binary.LittleEndian.Uint64(raw[48:56]))
		mag := math.Sqrt(ax*ax + ay*ay + az*az)

		delta := mag - mean
		mean += delta / float64(i+1)
		delta2 := mag - mean
		m2 += delta * delta2
	}

	variance := m2 / float64(n-1)
	stddev := math.Sqrt(variance)

	// Z-score of the last (most recent) point from raw bytes.
	last := rawPoints[n-1]
	lastAX := math.Float64frombits(binary.LittleEndian.Uint64(last[32:40]))
	lastAY := math.Float64frombits(binary.LittleEndian.Uint64(last[40:48]))
	lastAZ := math.Float64frombits(binary.LittleEndian.Uint64(last[48:56]))
	lastMag := math.Sqrt(lastAX*lastAX + lastAY*lastAY + lastAZ*lastAZ)

	zScore := (lastMag - mean) / stddev

	return Result{
		ZScore:    zScore,
		Samples:   n,
		Anomalous: math.Abs(zScore) > anomalyThres,
		Mean:      mean,
		Stddev:    stddev,
	}, nil
}
