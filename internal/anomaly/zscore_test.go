package anomaly

import (
	"encoding/binary"
	"math"
	"testing"
)

func makeRawPoint(ax, ay, az float64) []byte {
	b := make([]byte, 56)
	binary.LittleEndian.PutUint64(b[32:40], math.Float64bits(ax))
	binary.LittleEndian.PutUint64(b[40:48], math.Float64bits(ay))
	binary.LittleEndian.PutUint64(b[48:56], math.Float64bits(az))
	return b
}

func TestComputeBinaryInsufficientSamples(t *testing.T) {
	raw := make([][]byte, 5)
	_, err := ComputeBinary(raw)
	if err == nil {
		t.Error("expected error for < 8 samples")
	}
}

func TestComputeBinary(t *testing.T) {
	raw := make([][]byte, 256)
	for i := range raw {
		raw[i] = makeRawPoint(0, 0, 9.81)
	}
	result, err := ComputeBinary(raw)
	if err != nil {
		t.Fatalf("ComputeBinary: %v", err)
	}
	if result.Samples != 256 {
		t.Errorf("Samples = %d", result.Samples)
	}
	if result.Anomalous {
		t.Error("expected not anomalous for constant data")
	}
	if math.Abs(result.Stddev) > 1e-10 {
		t.Errorf("Stddev = %f", result.Stddev)
	}
}

func TestComputeBinaryWithAnomaly(t *testing.T) {
	raw := make([][]byte, 256)
	for i := range 255 {
		raw[i] = makeRawPoint(0, 0, 9.81)
	}
	raw[255] = makeRawPoint(0, 0, 100.0)
	result, err := ComputeBinary(raw)
	if err != nil {
		t.Fatalf("ComputeBinary: %v", err)
	}
	if !result.Anomalous {
		t.Error("expected anomalous")
	}
	if math.Abs(result.ZScore) <= 3.0 {
		t.Errorf("ZScore = %f, want |z| > 3", result.ZScore)
	}
}

func BenchmarkComputeBinary(b *testing.B) {
	raw := make([][]byte, 256)
	for i := range raw {
		raw[i] = makeRawPoint(float64(i%10)*0.1, float64(i%7)*0.05, 9.81+float64(i%5)*0.02)
	}
	for b.Loop() {
		ComputeBinary(raw)
	}
}
