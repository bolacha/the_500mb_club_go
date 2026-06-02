// Package telemetry defines the telemetry point type, validation, binary encoding,
// and Redis-backed storage operations.
package telemetry

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
)

// PointSize is the size in bytes of a compact binary-encoded telemetry point.
const PointSize = 56

// TelemetryPoint represents a single device telemetry reading.
type TelemetryPoint struct {
	TS      int64   `json:"ts"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	Battery float64 `json:"battery,omitzero"`
	AX      float64 `json:"ax"`
	AY      float64 `json:"ay"`
	AZ      float64 `json:"az"`
}

// Validate checks that all fields are within valid ranges.
// Returns nil if valid, or an error describing the first invalid field.
func (p *TelemetryPoint) Validate() error {
	if p.TS <= 0 {
		return fmt.Errorf("ts must be positive, got %d", p.TS)
	}
	if p.Lat < -90 || p.Lat > 90 {
		return fmt.Errorf("lat out of range [-90,90]: %f", p.Lat)
	}
	if p.Lon < -180 || p.Lon > 180 {
		return fmt.Errorf("lon out of range [-180,180]: %f", p.Lon)
	}
	if p.Battery < 0 || p.Battery > 1 {
		return fmt.Errorf("battery out of range [0,1]: %f", p.Battery)
	}
	if math.IsNaN(p.AX) || math.IsInf(p.AX, 0) {
		return fmt.Errorf("ax must be finite: %f", p.AX)
	}
	if math.IsNaN(p.AY) || math.IsInf(p.AY, 0) {
		return fmt.Errorf("ay must be finite: %f", p.AY)
	}
	if math.IsNaN(p.AZ) || math.IsInf(p.AZ, 0) {
		return fmt.Errorf("az must be finite: %f", p.AZ)
	}
	return nil
}

// DecodeJSON parses raw JSON into a TelemetryPoint, validating all fields.
func DecodeJSON(raw []byte) (TelemetryPoint, error) {
	var p TelemetryPoint
	if err := json.Unmarshal(raw, &p); err != nil {
		return p, fmt.Errorf("invalid json: %w", err)
	}
	if err := p.Validate(); err != nil {
		return p, err
	}
	return p, nil
}

// ── compact binary encoding (56 bytes) ──────────────────

// Encode serializes a point into a compact 56-byte binary format.
// Layout: [ts:8][lat:8][lon:8][battery:8][ax:8][ay:8][az:8] = 56 bytes.
func (p *TelemetryPoint) Encode() []byte {
	b := make([]byte, PointSize)
	binary.LittleEndian.PutUint64(b[0:8], uint64(p.TS))
	binary.LittleEndian.PutUint64(b[8:16], math.Float64bits(p.Lat))
	binary.LittleEndian.PutUint64(b[16:24], math.Float64bits(p.Lon))
	binary.LittleEndian.PutUint64(b[24:32], math.Float64bits(p.Battery))
	binary.LittleEndian.PutUint64(b[32:40], math.Float64bits(p.AX))
	binary.LittleEndian.PutUint64(b[40:48], math.Float64bits(p.AY))
	binary.LittleEndian.PutUint64(b[48:56], math.Float64bits(p.AZ))
	return b
}

// EncodeInto writes the compact binary encoding into b. b must have at least PointSize capacity.
func (p *TelemetryPoint) EncodeInto(b []byte) {
	binary.LittleEndian.PutUint64(b[0:8], uint64(p.TS))
	binary.LittleEndian.PutUint64(b[8:16], math.Float64bits(p.Lat))
	binary.LittleEndian.PutUint64(b[16:24], math.Float64bits(p.Lon))
	binary.LittleEndian.PutUint64(b[24:32], math.Float64bits(p.Battery))
	binary.LittleEndian.PutUint64(b[32:40], math.Float64bits(p.AX))
	binary.LittleEndian.PutUint64(b[40:48], math.Float64bits(p.AY))
	binary.LittleEndian.PutUint64(b[48:56], math.Float64bits(p.AZ))
}

// DecodeBinary deserializes a 56-byte compact representation into a point.
func DecodeBinary(b []byte) TelemetryPoint {
	return TelemetryPoint{
		TS:      int64(binary.LittleEndian.Uint64(b[0:8])),
		Lat:     math.Float64frombits(binary.LittleEndian.Uint64(b[8:16])),
		Lon:     math.Float64frombits(binary.LittleEndian.Uint64(b[16:24])),
		Battery: math.Float64frombits(binary.LittleEndian.Uint64(b[24:32])),
		AX:      math.Float64frombits(binary.LittleEndian.Uint64(b[32:40])),
		AY:      math.Float64frombits(binary.LittleEndian.Uint64(b[40:48])),
		AZ:      math.Float64frombits(binary.LittleEndian.Uint64(b[48:56])),
	}
}

// MarshalJSON serializes the point to JSON. Used for API responses.
func (p *TelemetryPoint) MarshalJSON() ([]byte, error) {
	return json.Marshal(p)
}
