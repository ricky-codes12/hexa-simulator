// Package telemetry is the simulator's record: one reading of one device, shaped like Hexa.Sensor's
// `hexa.sensor/telemetry/v1` (its docs/contracts/telemetry-v1.md). Every output encodes the same
// record: HTTP push and MQTT as telemetry-v1 JSON, Teltonika Direct as Codec 8/8E IO elements.
package telemetry

import (
	"encoding/json"
	"math"
	"time"
)

// Schema is the telemetry-v1 schema ID.
const Schema = "hexa.sensor/telemetry/v1"

// Canonical attribute keys (telemetry-v1 §3) the simulator's profiles report.
const (
	Ignition        = "ignition"
	Movement        = "movement"
	ExternalVoltage = "external_voltage"
	BatteryVoltage  = "battery_voltage"
	PowerCut        = "power_cut"
	GNSSStatus      = "gnss_status"
	GSMSignal       = "gsm_signal"
	Odometer        = "odometer"
	DIN1            = "din1"
)

// Position is a GNSS fix.
type Position struct {
	Lat        float64
	Lon        float64
	AltitudeM  float64
	SpeedKmh   float64
	HeadingDeg float64
	Satellites int
	FixValid   bool
}

// Record is one reading. Attributes hold canonical keys only; a profile decides which.
type Record struct {
	HardwareID string
	Time       time.Time
	Position   *Position
	Attributes map[string]any
	// Priority and EventIO are Teltonika record metadata: 0 low, 1 high; the IO element that
	// triggered the record, 0 for a periodic one.
	Priority int
	EventIO  int
}

// Bool reads a boolean attribute.
func (r Record) Bool(key string) (bool, bool) {
	v, ok := r.Attributes[key].(bool)
	return v, ok
}

// Number reads a numeric attribute.
func (r Record) Number(key string) (float64, bool) {
	switch v := r.Attributes[key].(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	}
	return 0, false
}

type wireDevice struct {
	HardwareID string `json:"hardware_id"`
}

type wirePosition struct {
	FixValid   bool     `json:"fix_valid"`
	Lat        float64  `json:"lat"`
	Lon        float64  `json:"lon"`
	AltitudeM  *float64 `json:"altitude_m,omitempty"`
	SpeedKmh   float64  `json:"speed_kmh"`
	HeadingDeg int      `json:"heading_deg"`
	Satellites *int     `json:"satellites,omitempty"`
}

type wireRecord struct {
	Schema     string         `json:"schema"`
	Device     wireDevice     `json:"device"`
	DeviceTime string         `json:"device_time"`
	Position   *wirePosition  `json:"position,omitempty"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

// Wire returns the telemetry-v1 JSON form: RFC 3339 UTC time with milliseconds, a heading of
// 0 to 359 and coordinates to seven decimals.
func (r Record) Wire() any {
	w := wireRecord{Schema: Schema, Device: wireDevice{r.HardwareID}, DeviceTime: FormatTime(r.Time), Attributes: r.Attributes}
	if p := r.Position; p != nil {
		wp := &wirePosition{FixValid: p.FixValid, Lat: Round(p.Lat, 7), Lon: Round(p.Lon, 7), SpeedKmh: Round(math.Max(0, p.SpeedKmh), 1), HeadingDeg: Heading(p.HeadingDeg)}
		if p.FixValid {
			alt, sats := Round(p.AltitudeM, 0), p.Satellites
			wp.AltitudeM, wp.Satellites = &alt, &sats
		}
		w.Position = wp
	}
	return w
}

// MarshalJSON writes the telemetry-v1 JSON form.
func (r Record) MarshalJSON() ([]byte, error) { return json.Marshal(r.Wire()) }

// FormatTime writes a device time as telemetry-v1 wants it.
func FormatTime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

// Heading returns a compass heading as an integer from 0 to 359.
func Heading(h float64) int {
	v := int(math.Round(h)) % 360
	if v < 0 {
		v += 360
	}
	return v
}

// Round rounds to n decimals.
func Round(v float64, n int) float64 {
	p := math.Pow(10, float64(n))
	return math.Round(v*p) / p
}
