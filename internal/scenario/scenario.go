// Package scenario holds the misbehaviours a unit can be given (plan S3) and the named,
// repeatable timelines that give them to the fleet in a fixed order.
//
// Behaviours are written against records, not against GPS (plan §8.3): signal loss, intermittent
// reporting and a power cut change what a device sends, whatever its profile; the motion
// behaviours apply to units that move.
package scenario

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Behaviour types.
const (
	Overspeed    = "overspeed"
	GeofenceExit = "geofence-exit"
	SignalLoss   = "signal-loss"
	Intermittent = "intermittent"
	PowerCut     = "power-cut"
	Idle         = "idle"
	Park         = "park"
	Work         = "work"
)

// Param is one setting of a behaviour.
type Param struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Unit    string `json:"unit,omitempty"`
	Default any    `json:"default"`
	Min     any    `json:"min,omitempty"`
	Max     any    `json:"max,omitempty"`
}

// Spec describes a behaviour: what the device does and what Sensor should show.
type Spec struct {
	Type     string  `json:"type"`
	Name     string  `json:"name"`
	Does     string  `json:"does"`
	Sensor   string  `json:"sensor"`
	Duration int     `json:"default_duration_s"`
	Params   []Param `json:"params"`
}

// Catalog lists every behaviour in the order the console offers them.
var Catalog = []Spec{
	{Type: Overspeed, Name: "Overspeed", Does: "Drives its route above the limit.", Sensor: "Overspeed alarm (trucks over 60 km/h for 10 s)", Duration: 90,
		Params: []Param{{Key: "kmh", Label: "Speed", Unit: "km/h", Default: 82.0, Min: 61.0, Max: 140.0}}},
	{Type: GeofenceExit, Name: "Leave the operating area", Does: "Drives out of the estate boundary, waits, then drives back to its route.", Sensor: "Left the operating area, then back", Duration: 0,
		Params: []Param{{Key: "beyond_m", Label: "Distance outside", Unit: "m", Default: 250.0, Min: 50.0, Max: 2000.0}, {Key: "wait_s", Label: "Wait outside", Unit: "s", Default: 90.0, Min: 0.0, Max: 3600.0}}},
	{Type: SignalLoss, Name: "Signal loss", Does: "Keeps moving but sends nothing.", Sensor: "Late, then offline", Duration: 360},
	{Type: Intermittent, Name: "Intermittent", Does: "Drops a share of its records, or holds them and sends them in bursts.", Sensor: "Gaps in Device Live, late between bursts", Duration: 300,
		Params: []Param{{Key: "drop_pct", Label: "Records dropped", Unit: "%", Default: 60.0, Min: 0.0, Max: 100.0}, {Key: "burst_s", Label: "Burst every (0 drops instead)", Unit: "s", Default: 0.0, Min: 0.0, Max: 900.0}}},
	{Type: PowerCut, Name: "Power cut", Does: "Vehicle power drops to 0 V and the tracker runs on its battery.", Sensor: "Power-cut alarm (HS-006b)", Duration: 600},
	{Type: Idle, Name: "Idle", Does: "Stands with the engine running.", Sensor: "Idle alarm (HS-006b)", Duration: 720},
	{Type: Park, Name: "Park", Does: "Engine off, a heartbeat every few minutes.", Sensor: "Parked, then stale", Duration: 3600,
		Params: []Param{{Key: "heartbeat_s", Label: "Heartbeat every", Unit: "s", Default: 300.0, Min: 30.0, Max: 3600.0}}},
	{Type: Work, Name: "Work a compartment", Does: "Drives to a compartment and works it lane by lane with the implement down.", Sensor: "Coverage on the work order (HS-020)", Duration: 1200,
		Params: []Param{{Key: "compartment", Label: "Compartment or block", Default: "SLG-C07"}, {Key: "kmh", Label: "Working speed", Unit: "km/h", Default: 4.5, Min: 2.0, Max: 12.0}}},
}

// SpecFor finds a behaviour by type.
func SpecFor(t string) (Spec, bool) {
	for _, s := range Catalog {
		if s.Type == t {
			return s, true
		}
	}
	return Spec{}, false
}

// Params are a behaviour's settings.
type Params map[string]any

// Number reads a numeric setting, falling back to the spec's default.
func (p Params) Number(key string, def float64) float64 {
	switch v := p[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	}
	return def
}

// String reads a text setting.
func (p Params) String(key, def string) string {
	if v, ok := p[key].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

// Normalize fills in defaults and checks every setting against its spec.
func Normalize(t string, in Params) (Params, error) {
	spec, ok := SpecFor(t)
	if !ok {
		return nil, fmt.Errorf("unknown behaviour %q", t)
	}
	out := Params{}
	for _, p := range spec.Params {
		switch def := p.Default.(type) {
		case float64:
			v := in.Number(p.Key, def)
			if min, ok := p.Min.(float64); ok && v < min {
				return nil, fmt.Errorf("%s: %s must be at least %v", spec.Name, p.Label, min)
			}
			if max, ok := p.Max.(float64); ok && v > max {
				return nil, fmt.Errorf("%s: %s must be at most %v", spec.Name, p.Label, max)
			}
			out[p.Key] = v
		case string:
			out[p.Key] = in.String(p.Key, def)
		}
	}
	for k := range in {
		if _, known := out[k]; !known {
			return nil, fmt.Errorf("%s has no setting %q", spec.Name, k)
		}
	}
	return out, nil
}

// Motion reports whether a behaviour moves or stops the unit, as opposed to changing only what
// it sends.
func Motion(t string) bool {
	switch t {
	case Overspeed, GeofenceExit, Idle, Park, Work:
		return true
	}
	return false
}

// Target picks units: by name, by ID, or by kind, estate and output, the first Limit of them in
// name order.
type Target struct {
	IDs    []int64  `json:"ids,omitempty"`
	Names  []string `json:"names,omitempty"`
	Kind   string   `json:"kind,omitempty"`
	Estate string   `json:"estate,omitempty"`
	Output string   `json:"output,omitempty"`
	Limit  int      `json:"limit,omitempty"`
}

// Empty reports whether the target names no filter at all, which means the whole fleet.
func (t Target) Empty() bool {
	return len(t.IDs) == 0 && len(t.Names) == 0 && t.Kind == "" && t.Estate == "" && t.Output == ""
}

// Candidate is what Select needs to know of a unit.
type Candidate struct {
	ID                         int64
	Name, Kind, Estate, Output string
}

// Select returns the IDs of the candidates the target picks, in name order.
func (t Target) Select(all []Candidate) []int64 {
	ids := map[int64]bool{}
	for _, id := range t.IDs {
		ids[id] = true
	}
	names := map[string]bool{}
	for _, n := range t.Names {
		names[strings.ToUpper(strings.TrimSpace(n))] = true
	}
	sorted := append([]Candidate(nil), all...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	var out []int64
	for _, c := range sorted {
		if len(ids) > 0 && !ids[c.ID] {
			continue
		}
		if len(names) > 0 && !names[strings.ToUpper(c.Name)] {
			continue
		}
		if (t.Kind != "" && c.Kind != t.Kind) || (t.Estate != "" && c.Estate != t.Estate) || (t.Output != "" && c.Output != t.Output) {
			continue
		}
		out = append(out, c.ID)
		if t.Limit > 0 && len(out) == t.Limit {
			break
		}
	}
	return out
}

// Event is one step of a timeline: at an offset, give a behaviour to a target. An event without a
// type is a cue on the countdown only, such as units coming back after a signal loss.
type Event struct {
	At       time.Duration
	Type     string
	Duration time.Duration
	Params   Params
	Target   Target
	Note     string
}

// Scenario is a named, repeatable timeline.
type Scenario struct {
	Name    string
	Title   string
	Summary string
	Length  time.Duration
	Events  []Event
}

// Scenarios are the built-in timelines.
var Scenarios = []Scenario{forestry30}

// ByName finds a scenario.
func ByName(name string) (Scenario, bool) {
	for _, s := range Scenarios {
		if s.Name == name {
			return s, true
		}
	}
	return Scenario{}, false
}

// forestry30 is the default timeline (plan S3): each event trips one Sensor reaction, spaced so
// each can be followed in Sensor before the next. Units are picked by name, so a rerun from T0 plays the same.
var forestry30 = Scenario{
	Name:    "forestry-30min",
	Title:   "Forestry operations, 30 minutes",
	Summary: "Overspeed, compartment work, a power cut, idling, signal loss in the valley, a boundary exit, patchy reporting and an early park.",
	Length:  30 * time.Minute,
	Events: []Event{
		{At: 2 * time.Minute, Type: Overspeed, Duration: 90 * time.Second, Params: Params{"kmh": 82.0}, Target: Target{Names: []string{"HT-012"}}, Note: "HT-012 overspeeds on the haul road"},
		{At: 5 * time.Minute, Type: Work, Duration: 20 * time.Minute, Params: Params{"compartment": "SLG-C07", "kmh": 4.5}, Target: Target{Names: []string{"TR-004"}}, Note: "TR-004 starts compartment C07"},
		{At: 8 * time.Minute, Type: PowerCut, Duration: 10 * time.Minute, Target: Target{Names: []string{"DZ-002"}}, Note: "DZ-002 loses vehicle power"},
		{At: 10 * time.Minute, Type: Idle, Duration: 12 * time.Minute, Target: Target{Names: []string{"WT-003"}}, Note: "WT-003 idles with the engine on"},
		{At: 12 * time.Minute, Type: SignalLoss, Duration: 6 * time.Minute, Target: Target{Kind: "pickup", Estate: "KNG", Limit: 5}, Note: "Five Kenanga pickups lose signal"},
		{At: 15 * time.Minute, Type: GeofenceExit, Params: Params{"beyond_m": 300.0, "wait_s": 120.0}, Target: Target{Names: []string{"HT-031"}}, Note: "HT-031 leaves the operating area"},
		{At: 18 * time.Minute, Note: "The five pickups report again"},
		{At: 20 * time.Minute, Type: Intermittent, Duration: 5 * time.Minute, Params: Params{"drop_pct": 60.0}, Target: Target{Names: []string{"FB-002"}}, Note: "FB-002 reports patchily"},
		{At: 22 * time.Minute, Type: Intermittent, Duration: 5 * time.Minute, Params: Params{"burst_s": 60.0}, Target: Target{Names: []string{"EX-005"}}, Note: "EX-005 reports in bursts"},
		{At: 25 * time.Minute, Type: Park, Duration: 30 * time.Minute, Params: Params{"heartbeat_s": 300.0}, Target: Target{Names: []string{"PU-010"}}, Note: "PU-010 parks for the night"},
	},
}
