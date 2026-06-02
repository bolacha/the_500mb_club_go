package anomaly

import (
	"testing"

	"github.com/bolacha/the_500mb_club_go/internal/telemetry"
)

func BenchmarkCompute(b *testing.B) {
	points := make([]telemetry.TelemetryPoint, 256)
	for i := range points {
		points[i] = telemetry.TelemetryPoint{
			TS: int64(i * 100),
			AX: float64(i%10) * 0.1,
			AY: float64(i%7) * 0.05,
			AZ: 9.81 + float64(i%5)*0.02,
		}
	}

	for b.Loop() {
		Compute(points)
	}
}

func BenchmarkComputeSmall(b *testing.B) {
	points := make([]telemetry.TelemetryPoint, 8)
	for i := range points {
		points[i] = telemetry.TelemetryPoint{
			TS: int64(i * 100),
			AX: 0, AY: 0, AZ: 9.81,
		}
	}
	for b.Loop() {
		Compute(points)
	}
}
