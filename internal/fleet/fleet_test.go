package fleet

import (
	"strings"
	"testing"

	"hexa-simulator/internal/world"
)

func TestDefaultCompositionIs350WithPlanShares(t *testing.T) {
	c, err := ParseComposition("")
	if err != nil {
		t.Fatal(err)
	}
	if c.Total() != 350 {
		t.Fatalf("total=%d", c.Total())
	}
	if c["haul-truck"] < 155 || c["farm-tractor"] != 70 || c["pickup"] < 52 {
		t.Fatalf("composition=%s", c)
	}
	explicit, err := ParseComposition("haul-truck=3, dozer:2")
	if err != nil || explicit.Total() != 5 || explicit["dozer"] != 2 {
		t.Fatalf("explicit=%v err=%v", explicit, err)
	}
	for _, bad := range []string{"tank=3", "haul-truck", "haul-truck=-1", "99999"} {
		if _, err := ParseComposition(bad); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
	if off, _ := ParseComposition("off"); off.Total() != 0 {
		t.Fatal("off must seed nothing")
	}
}

func TestRosterIdentityIsUniqueAndClearOfSensorFleet(t *testing.T) {
	units := Roster(Shares(350), 7)
	names, imeis := map[string]bool{}, map[string]bool{}
	outputs := map[string]int{}
	for _, u := range units {
		if names[u.Name] || imeis[u.IMEI] {
			t.Fatalf("duplicate identity %s %s", u.Name, u.IMEI)
		}
		names[u.Name], imeis[u.IMEI] = true, true
		if len(u.IMEI) != 15 || !strings.HasPrefix(u.IMEI, IMEIPrefix) || luhn(u.IMEI[:14]) != int(u.IMEI[14]-'0') {
			t.Fatalf("bad IMEI %s", u.IMEI)
		}
		if strings.HasPrefix(u.IMEI, "3563070424") {
			t.Fatalf("IMEI %s collides with Hexa.Sensor's simulator fleet", u.IMEI)
		}
		if u.Estate != "KNG" && u.Estate != "MRT" && u.Estate != "SLG" {
			t.Fatalf("%s estate %q", u.Name, u.Estate)
		}
		outputs[u.Output]++
	}
	if outputs[OutputMQTT] < 175 || outputs[OutputTeltonika] < 90 || outputs[OutputHTTPPush] < 45 {
		t.Fatalf("output split %v", outputs)
	}
	if !names["HT-012"] || !names["TR-004"] || !names["DZ-002"] {
		t.Fatal("the plan's example names must exist")
	}
}

// Every unit stays inside the operating area and out of the conservation zone for a whole
// cycle, heavy equipment stays inside the concession, and haul roads stay under Sensor's
// 60 km/h truck limit, so the baseline fleet raises no alarm by itself.
func TestItinerariesRaiseNoAlarmsByThemselves(t *testing.T) {
	for _, u := range Roster(Shares(350), 7) {
		it := Plan(u.Kind, u.Number, 7)
		if it.Cycle < 600 {
			t.Fatalf("%s cycle %.0f s", u.Name, it.Cycle)
		}
		for s := 0.0; s < it.Cycle; s += 7 {
			st := it.At(s)
			if !world.Inside(st.Pos, world.OperatingArea) {
				t.Fatalf("%s leaves OPS at %.0f s (%s): %v", u.Name, s, st.Label, st.Pos)
			}
			if world.Inside(st.Pos, world.ConservationZone) {
				t.Fatalf("%s enters the conservation zone at %.0f s (%s)", u.Name, s, st.Label)
			}
			if u.Kind.AssetType == "heavy" && (!world.Inside(st.Pos, world.Sialang) || world.Inside(st.Pos, world.Meranti)) {
				t.Fatalf("%s leaves the concession at %.0f s", u.Name, s)
			}
			if u.Kind.AssetType == "truck" && st.Kmh > 56 {
				t.Fatalf("%s drives %.1f km/h", u.Name, st.Kmh)
			}
		}
	}
}

func TestPlanIsDeterministic(t *testing.T) {
	k, _ := KindByKey("pickup")
	a, b := Plan(k, 9, 7), Plan(k, 9, 7)
	for _, s := range []float64{0, 311, 1999, a.Cycle * 3.4} {
		if a.At(s) != b.At(s) {
			t.Fatalf("t=%v: %+v vs %+v", s, a.At(s), b.At(s))
		}
	}
	if c := Plan(k, 9, 8); c.Cycle == a.Cycle {
		t.Fatal("another seed should give another day")
	}
}

func TestTractorWorksLanesWithImplementDown(t *testing.T) {
	k, _ := KindByKey("farm-tractor")
	it := Plan(k, 1, 7)
	working, turning := 0, 0
	for s := 0.0; s < it.Cycle; s += 5 {
		st := it.At(s)
		if st.Working {
			working++
			if st.Kmh < 5 || st.Kmh > 12 {
				t.Fatalf("working at %.1f km/h", st.Kmh)
			}
		} else if st.Kmh > 0 {
			turning++
		}
	}
	if working == 0 || turning == 0 {
		t.Fatalf("working=%d turning=%d", working, turning)
	}
	standby := Plan(k, StandbyTractor, 7)
	if st := standby.At(100); st.Kmh != 0 || st.Ignition || standby.Estate != "SLG" {
		t.Fatalf("standby tractor %+v", st)
	}
	work := CompartmentWork(world.CompartmentC07, 4.5)
	for s := 0.0; s < work.Cycle; s += 5 {
		if st := work.At(s); st.Working && !world.Inside(st.Pos, world.CompartmentC07) {
			t.Fatalf("lane work outside C07 at %v", st.Pos)
		}
	}
}
