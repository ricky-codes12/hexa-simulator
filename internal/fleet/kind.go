// Package fleet is the demo fleet: device profiles (kinds), the deterministic roster built from a
// composition and a seed, and each unit's daily itinerary on Hexa.Sensor's world.
//
// A kind is the seed of the device-profile idea in the demo fleet plan §8: a named type with the
// telemetry fields it reports, the Sensor asset type it registers as, and the transport it uses
// by default. Only GPS tracker profiles exist today.
package fleet

import (
	"fmt"
	"sort"
	"strings"

	"hexa-simulator/internal/telemetry"
)

// Outputs a device can be routed to (plan S1). All sends to every configured output and is
// meant for protocol testing only; none keeps the device silent.
const (
	OutputMQTT      = "mqtt"
	OutputTeltonika = "teltonika"
	OutputHTTPPush  = "http-push"
	OutputAll       = "all"
	OutputNone      = "none"
)

// Transports are the three real outputs, in display order.
var Transports = []string{OutputMQTT, OutputTeltonika, OutputHTTPPush}

// ValidOutput reports whether s is a device output setting.
func ValidOutput(s string) bool {
	switch s {
	case OutputMQTT, OutputTeltonika, OutputHTTPPush, OutputAll, OutputNone:
		return true
	}
	return false
}

// Kind is a device profile.
type Kind struct {
	Key       string  // haul-truck
	Name      string  // Haul truck
	Code      string  // HT, the unit name prefix
	Digit     int     // the kind's digit in a seeded IMEI
	AssetType string  // the asset type Sensor registers the unit as; its rules scope by it
	Share     float64 // default share of the fleet
	Volts     float64 // the vehicle's electrical system: 24 or 12
	// Fields are the canonical telemetry-v1 attribute keys the profile reports.
	Fields []string
	model  func(n int) string
	output func(n int) string
}

// Model returns the tracker model of unit n.
func (k *Kind) Model(n int) string { return k.model(n) }

// DefaultOutput returns the transport unit n uses unless an operator changes it.
func (k *Kind) DefaultOutput(n int) string { return k.output(n) }

// Reports says whether the profile reports an attribute.
func (k *Kind) Reports(key string) bool {
	for _, f := range k.Fields {
		if f == key {
			return true
		}
	}
	return false
}

var trackerFields = []string{telemetry.Ignition, telemetry.Movement, telemetry.ExternalVoltage, telemetry.BatteryVoltage,
	telemetry.PowerCut, telemetry.GNSSStatus, telemetry.GSMSignal, telemetry.Odometer}

func always(s string) func(int) string { return func(int) string { return s } }

const (
	fmc920 = "Teltonika FMC920"
	fmc650 = "Teltonika FMC650"
)

// Kinds are the demo fleet's profiles in roster order (plan S2). Tractors, dozers and
// excavators carry FMC650s wired straight to Sensor over TCP; pickups publish to the customer
// broker; water trucks and fuel bowsers come through a vendor cloud's HTTP push; haul trucks
// mostly use the broker. That gives about 55% MQTT, 30% Teltonika TCP and 15% HTTP push.
var Kinds = []*Kind{
	{Key: "haul-truck", Name: "Haul truck", Code: "HT", Digit: 1, AssetType: "truck", Share: 0.45, Volts: 24, Fields: trackerFields,
		model: func(n int) string {
			if n%3 == 0 {
				return fmc650
			}
			return fmc920
		},
		output: func(n int) string {
			if n%9 == 0 {
				return OutputHTTPPush
			}
			return OutputMQTT
		}},
	{Key: "farm-tractor", Name: "Farm tractor", Code: "TR", Digit: 2, AssetType: "tractor", Share: 0.20, Volts: 12,
		Fields: append(append([]string(nil), trackerFields...), telemetry.DIN1), model: always(fmc650), output: always(OutputTeltonika)},
	{Key: "dozer", Name: "Dozer", Code: "DZ", Digit: 3, AssetType: "heavy", Share: 0.05, Volts: 24, Fields: trackerFields,
		model: always(fmc650), output: always(OutputTeltonika)},
	{Key: "excavator", Name: "Excavator", Code: "EX", Digit: 4, AssetType: "excavator", Share: 0.05, Volts: 24, Fields: trackerFields,
		model: always(fmc650), output: always(OutputTeltonika)},
	{Key: "pickup", Name: "Pickup", Code: "PU", Digit: 5, AssetType: "pickup", Share: 0.15, Volts: 12, Fields: trackerFields,
		model: always(fmc920), output: always(OutputMQTT)},
	{Key: "water-truck", Name: "Water truck", Code: "WT", Digit: 6, AssetType: "truck", Share: 0.05, Volts: 24, Fields: trackerFields,
		model: always(fmc920), output: always(OutputHTTPPush)},
	{Key: "fuel-bowser", Name: "Fuel bowser", Code: "FB", Digit: 7, AssetType: "truck", Share: 0.05, Volts: 24, Fields: trackerFields,
		model: always(fmc920), output: always(OutputHTTPPush)},
}

// KindByKey finds a kind; ok is false for an unknown key.
func KindByKey(key string) (*Kind, bool) {
	for _, k := range Kinds {
		if k.Key == key {
			return k, true
		}
	}
	return nil, false
}

// DefaultKind is the kind of a device created without one.
var DefaultKind = Kinds[0]

// Composition is how many units of each kind the seeded fleet has.
type Composition map[string]int

// Total is the fleet size.
func (c Composition) Total() int {
	n := 0
	for _, v := range c {
		n += v
	}
	return n
}

// String writes the composition in SIM_DEMO_FLEET form, in roster order.
func (c Composition) String() string {
	var parts []string
	for _, k := range Kinds {
		if c[k.Key] > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", k.Key, c[k.Key]))
		}
	}
	return strings.Join(parts, ",")
}

// DefaultSize is the default fleet size: 350 units, the PO's limit for bn2 (plan §0).
const DefaultSize = 350

// Shares splits a fleet of n units over the kinds by their default shares (largest remainder,
// ties in roster order).
func Shares(n int) Composition {
	c := Composition{}
	if n <= 0 {
		return c
	}
	type rem struct {
		i int
		r float64
	}
	var rest []rem
	total := 0
	for i, k := range Kinds {
		exact := k.Share * float64(n)
		c[k.Key] = int(exact)
		total += c[k.Key]
		rest = append(rest, rem{i, exact - float64(int(exact))})
	}
	sort.SliceStable(rest, func(a, b int) bool { return rest[a].r > rest[b].r+1e-9 })
	for i := 0; total < n; i++ {
		c[Kinds[rest[i%len(rest)].i].Key]++
		total++
	}
	return c
}

// ParseComposition reads SIM_DEMO_FLEET: empty for the default 350, a total such as "120" split
// by the default shares, "0" or "off" for no seeded fleet, or explicit counts such as
// "haul-truck=40,farm-tractor=10".
func ParseComposition(s string) (Composition, error) {
	s = strings.TrimSpace(s)
	switch strings.ToLower(s) {
	case "":
		return Shares(DefaultSize), nil
	case "0", "off", "none":
		return Composition{}, nil
	}
	var total int
	if _, err := fmt.Sscanf(s, "%d", &total); err == nil && fmt.Sprint(total) == s {
		if total < 0 || total > 20000 {
			return nil, fmt.Errorf("fleet size %d is outside 0 to 20000", total)
		}
		return Shares(total), nil
	}
	c := Composition{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			key, value, ok = strings.Cut(part, ":")
		}
		if !ok {
			return nil, fmt.Errorf("fleet part %q: want kind=count", part)
		}
		key = strings.TrimSpace(key)
		if _, known := KindByKey(key); !known {
			return nil, fmt.Errorf("fleet part %q: unknown kind %q", part, key)
		}
		var n int
		if _, err := fmt.Sscanf(strings.TrimSpace(value), "%d", &n); err != nil || n < 0 || n > 20000 {
			return nil, fmt.Errorf("fleet part %q: count must be 0 to 20000", part)
		}
		c[key] += n
	}
	if c.Total() > 20000 {
		return nil, fmt.Errorf("fleet of %d units is larger than 20000", c.Total())
	}
	return c, nil
}
