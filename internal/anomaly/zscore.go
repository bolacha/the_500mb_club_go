// Package anomaly implements anomaly detection using z-score over the acceleration magnitude.
// Uses Welford's online algorithm for a single-pass, numerically stable computation.
// No caching is performed — computation is done fresh on every call (as required by the spec).
package anomaly

import (
	"fmt"
	"math"

	"github.com/bolacha/the_500mb_club_go/internal/telemetry"
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
	maxSamples   = 256  // window size
	anomalyThres = 3.0  // |z| > 3 → anomalous
)

// Compute calculates the z-score of the most recent point's acceleration magnitude
// against the mean and standard deviation of the last up-to-256 points.
// Points must be in chronological order (oldest first). The last point is the "most recent".
func Compute(points []telemetry.TelemetryPoint) (Result, error) {
	n := len(points)
	if n < minSamples {
		return Result{Samples: n}, fmt.Errorf("insufficient samples: %d < %d", n, minSamples)
	}

	// Single-pass Welford's algorithm for mean and variance.
	var mean, m2 float64
	for i, p := range points {
		mag := math.Sqrt(p.AX*p.AX + p.AY*p.AY + p.AZ*p.AZ)
		delta := mag - mean
		mean += delta / float64(i+1)
		delta2 := mag - mean
		m2 += delta * delta2
	}

	variance := m2 / float64(n-1) // sample variance (n-1)
	stddev := math.Sqrt(variance)

	// Z-score of the last (most recent) point.
	last := points[n-1]
	lastMag := math.Sqrt(last.AX*last.AX + last.AY*last.AY + last.AZ*last.AZ)

	zScore := (lastMag - mean) / stddev

	return Result{
		ZScore:    zScore,
		Samples:   n,
		Anomalous: math.Abs(zScore) > anomalyThres,
		Mean:      mean,
		Stddev:    stddev,
	}, nil
}
