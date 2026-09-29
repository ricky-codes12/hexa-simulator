package teltonika

import (
	"math"

	"hexa-simulator/internal/telemetry"
)

// ioElement maps a canonical telemetry-v1 attribute to an FMB/FMC IO element, as Hexa.Sensor's
// teltonika-tcp device types read them back (plan S4): IO 66 and 67 in millivolts, IO 16 in
// metres, the flags as 0 or 1.
type ioElement struct {
	id    uint16
	key   string
	size  int
	scale float64
}

var ioElements = []ioElement{
	{1, telemetry.DIN1, 1, 1},
	{16, telemetry.Odometer, 4, 1},
	{21, telemetry.GSMSignal, 1, 1},
	{66, telemetry.ExternalVoltage, 2, 1000},
	{67, telemetry.BatteryVoltage, 2, 1000},
	{69, telemetry.GNSSStatus, 1, 1},
	{239, telemetry.Ignition, 1, 1},
	{240, telemetry.Movement, 1, 1},
}

// IOID returns the IO element an attribute is sent as.
func IOID(key string) (uint16, bool) {
	for _, e := range ioElements {
		if e.key == key {
			return e.id, true
		}
	}
	return 0, false
}

// FromTelemetry turns a simulator record into an AVL record. Attributes without an FMB/FMC IO
// element, such as power_cut, are not sent: a real tracker reports the voltage and Sensor
// derives the cut from it. A record without a fix is sent with no satellites at (0, 0), which
// Sensor reads as fix_valid=false.
func FromTelemetry(t telemetry.Record) Record {
	r := Record{Timestamp: t.Time, Priority: byte(t.Priority), EventIO: uint16(t.EventIO)}
	if p := t.Position; p != nil && p.FixValid {
		r.Latitude, r.Longitude = p.Lat, p.Lon
		r.Altitude = int(math.Round(p.AltitudeM))
		r.Angle = telemetry.Heading(p.HeadingDeg)
		r.Satellites = p.Satellites
		r.Speed = int(math.Round(math.Max(0, p.SpeedKmh)))
	}
	for _, e := range ioElements {
		if v, ok := t.Bool(e.key); ok {
			value := uint64(0)
			if v {
				value = 1
			}
			r.IO = append(r.IO, IO{ID: e.id, Size: e.size, Value: value})
			continue
		}
		if v, ok := t.Number(e.key); ok {
			r.IO = append(r.IO, IO{ID: e.id, Size: e.size, Value: uint64(math.Max(0, math.Round(v*e.scale)))})
		}
	}
	return r
}
