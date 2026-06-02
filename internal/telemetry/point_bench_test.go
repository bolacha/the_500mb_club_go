package telemetry

import (
	"testing"
)

func BenchmarkEncode(b *testing.B) {
	p := TelemetryPoint{
		TS:      1715800000000,
		Lat:     -23.5505,
		Lon:     -46.6333,
		Battery: 0.82,
		AX:      0.11,
		AY:      -0.04,
		AZ:      9.81,
	}
	for b.Loop() {
		_ = p.Encode()
	}
}

func BenchmarkDecodeBinary(b *testing.B) {
	p := TelemetryPoint{
		TS:      1715800000000,
		Lat:     -23.5505,
		Lon:     -46.6333,
		Battery: 0.82,
		AX:      0.11,
		AY:      -0.04,
		AZ:      9.81,
	}
	enc := p.Encode()
	for b.Loop() {
		_ = DecodeBinary(enc)
	}
}

func BenchmarkEncodeInto(b *testing.B) {
	p := TelemetryPoint{TS: 42, Lat: -23.5, Lon: -46.6, Battery: 0.5, AX: 1, AY: 2, AZ: 9.8}
	buf := make([]byte, PointSize)
	for b.Loop() {
		p.EncodeInto(buf)
	}
}

func BenchmarkDecodeJSON(b *testing.B) {
	data := []byte(`{"ts":1715800000000,"lat":-23.5505,"lon":-46.6333,"battery":0.82,"ax":0.11,"ay":-0.04,"az":9.81}`)
	for b.Loop() {
		DecodeJSON(data)
	}
}

func BenchmarkValidate(b *testing.B) {
	p := TelemetryPoint{
		TS:      1715800000000,
		Lat:     -23.5505,
		Lon:     -46.6333,
		Battery: 0.82,
		AX:      0.11,
		AY:      -0.04,
		AZ:      9.81,
	}
	for b.Loop() {
		p.Validate()
	}
}
