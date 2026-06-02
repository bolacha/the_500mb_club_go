package anomaly

import (
	"math"
	"testing"

	"github.com/bolacha/the_500mb_club_go/internal/telemetry"
)

func TestComputeInsufficientSamples(t *testing.T) {
	points := make([]telemetry.TelemetryPoint, 5)
	_, err := Compute(points)
	if err == nil {
		t.Error("expected error for < 8 samples")
	}
}

func TestCompute(t *testing.T) {
	// Generate 256 points with constant acceleration (1g ≈ 9.81).
	// The z-score should be ~0 (no anomaly).
	points := make([]telemetry.TelemetryPoint, 256)
	for i := range points {
		points[i] = telemetry.TelemetryPoint{
			TS: int64(i * 100),
			AX: 0,
			AY: 0,
			AZ: 9.81,
		}
	}

	result, err := Compute(points)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	if result.Samples != 256 {
		t.Errorf("Samples = %d, want 256", result.Samples)
	}
	if result.Anomalous {
		t.Error("expected not anomalous for constant data")
	}
	// With constant values, stddev should be very close to 0.
	if math.Abs(result.Stddev) > 1e-10 {
		t.Errorf("Stddev = %f, want ~0", result.Stddev)
	}
}

func TestComputeWithAnomaly(t *testing.T) {
	// 255 normal points (1g) + 1 anomalous point (100g).
	points := make([]telemetry.TelemetryPoint, 256)
	for i := range 255 {
		points[i] = telemetry.TelemetryPoint{
			TS: int64(i * 100),
			AX: 0, AY: 0, AZ: 9.81,
		}
	}
	// Last point has huge acceleration — should be anomalous.
	points[255] = telemetry.TelemetryPoint{
		TS: 25500,
		AX: 0, AY: 0, AZ: 100.0, // ~100 m/s²
	}

	result, err := Compute(points)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	if !result.Anomalous {
		t.Error("expected anomalous for outlier point")
	}
	if math.Abs(result.ZScore) <= 3.0 {
		t.Errorf("ZScore = %f, want |z| > 3", result.ZScore)
	}
}

func TestComputeExactly256(t *testing.T) {
	// Exactly 256 points with varying magnitudes.
	points := make([]telemetry.TelemetryPoint, 256)
	for i := range points {
		points[i] = telemetry.TelemetryPoint{
			TS: int64(i * 100),
			AX: float64(i%10) * 0.1,
			AY: float64(i%7) * 0.05,
			AZ: 9.81 + float64(i%5)*0.02,
		}
	}

	result, err := Compute(points)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if result.Samples != 256 {
		t.Errorf("Samples = %d", result.Samples)
	}
	if math.IsNaN(result.ZScore) {
		t.Error("ZScore is NaN")
	}
	if math.IsNaN(result.Mean) {
		t.Error("Mean is NaN")
	}
	if math.IsNaN(result.Stddev) {
		t.Error("Stddev is NaN")
	}
}
