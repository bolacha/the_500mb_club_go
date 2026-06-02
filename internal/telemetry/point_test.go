package telemetry

import (
	"math"
	"testing"
)

func TestValidate(t *testing.T) {
	ctx := t.Context()
	_ = ctx

	valid := TelemetryPoint{
		TS:      1715800000000,
		Lat:     -23.5505,
		Lon:     -46.6333,
		Battery: 0.82,
		AX:      0.11,
		AY:      -0.04,
		AZ:      9.81,
	}

	tests := []struct {
		name    string
		mutate  func(TelemetryPoint) TelemetryPoint
		wantErr bool
	}{
		{"valid", func(p TelemetryPoint) TelemetryPoint { return p }, false},
		{"zero ts", func(p TelemetryPoint) TelemetryPoint { p.TS = 0; return p }, true},
		{"negative ts", func(p TelemetryPoint) TelemetryPoint { p.TS = -1; return p }, true},
		{"lat > 90", func(p TelemetryPoint) TelemetryPoint { p.Lat = 91; return p }, true},
		{"lat < -90", func(p TelemetryPoint) TelemetryPoint { p.Lat = -91; return p }, true},
		{"lat = 90", func(p TelemetryPoint) TelemetryPoint { p.Lat = 90; return p }, false},
		{"lat = -90", func(p TelemetryPoint) TelemetryPoint { p.Lat = -90; return p }, false},
		{"lon > 180", func(p TelemetryPoint) TelemetryPoint { p.Lon = 181; return p }, true},
		{"lon < -180", func(p TelemetryPoint) TelemetryPoint { p.Lon = -181; return p }, true},
		{"lon = 180", func(p TelemetryPoint) TelemetryPoint { p.Lon = 180; return p }, false},
		{"battery > 1", func(p TelemetryPoint) TelemetryPoint { p.Battery = 1.1; return p }, true},
		{"battery < 0", func(p TelemetryPoint) TelemetryPoint { p.Battery = -0.1; return p }, true},
		{"battery = 1", func(p TelemetryPoint) TelemetryPoint { p.Battery = 1; return p }, false},
		{"battery = 0", func(p TelemetryPoint) TelemetryPoint { p.Battery = 0; return p }, false},
		{"ax NaN", func(p TelemetryPoint) TelemetryPoint { p.AX = math.NaN(); return p }, true},
		{"ax Inf", func(p TelemetryPoint) TelemetryPoint { p.AX = math.Inf(1); return p }, true},
		{"ay NaN", func(p TelemetryPoint) TelemetryPoint { p.AY = math.NaN(); return p }, true},
		{"az Inf", func(p TelemetryPoint) TelemetryPoint { p.AZ = math.Inf(-1); return p }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := tt.mutate(valid)
			err := p.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestDecodeJSON(t *testing.T) {
	valid := `{"ts":1715800000000,"lat":-23.5505,"lon":-46.6333,"battery":0.82,"ax":0.11,"ay":-0.04,"az":9.81}`
	p, err := DecodeJSON([]byte(valid))
	if err != nil {
		t.Fatalf("DecodeJSON: %v", err)
	}
	if p.TS != 1715800000000 {
		t.Errorf("TS = %d", p.TS)
	}
	if p.Lat != -23.5505 {
		t.Errorf("Lat = %f", p.Lat)
	}

	invalid := `{"ts":0}`
	_, err = DecodeJSON([]byte(invalid))
	if err == nil {
		t.Error("expected error for invalid json")
	}
}

func TestEncodeDecodeRoundtrip(t *testing.T) {
	orig := TelemetryPoint{
		TS:      1715800000000,
		Lat:     -23.5505,
		Lon:     -46.6333,
		Battery: 0.82,
		AX:      0.11,
		AY:      -0.04,
		AZ:      9.81,
	}

	b := orig.Encode()
	if len(b) != PointSize {
		t.Errorf("Encode len = %d, want %d", len(b), PointSize)
	}

	decoded := DecodeBinary(b)
	if decoded != orig {
		t.Errorf("roundtrip mismatch:\n  orig: %+v\n  got:  %+v", orig, decoded)
	}
}

func TestEncodeInto(t *testing.T) {
	p := TelemetryPoint{TS: 42, Lat: -23.5, Lon: -46.6, Battery: 0.5, AX: 1, AY: 2, AZ: 9.8}
	buf := make([]byte, PointSize)
	p.EncodeInto(buf)
	decoded := DecodeBinary(buf)
	if decoded != p {
		t.Errorf("EncodeInto roundtrip mismatch: %+v != %+v", decoded, p)
	}
}
