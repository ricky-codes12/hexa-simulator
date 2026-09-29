package httpapi

import (
	"context"
	"math"
	"sync"
	"testing"
	"time"

	"hexa-simulator/internal/fleet"
	"hexa-simulator/internal/scenario"
	"hexa-simulator/internal/telemetry"
	"hexa-simulator/internal/world"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) add(d time.Duration) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
	return c.t
}

// seeded is a device as the fleet seeds it.
func seeded(id int64, kindKey string, n int, output string) Device {
	k, _ := fleet.KindByKey(kindKey)
	return Device{ID: id, Name: fleet.UnitName(k, n), IMEI: fleet.SeededIMEI(k, n), Model: k.Model(n), Kind: k.Key, Output: output,
		Estate: fleet.Plan(k, n, 7).Estate, Seeded: true, Status: "offline", Attributes: map[string]any{}}
}

// harness starts a runtime on a fake clock whose own loop never fires; tests step it by hand.
func harness(t *testing.T, devices []Device, outputs ...Output) (*SimulationRuntime, *memStore, *clock) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	c := &clock{t: time.Date(2026, 9, 30, 8, 0, 0, 0, time.UTC)}
	s := &memStore{items: devices, fleet: FleetState{Epoch: c.now()}}
	r := NewSimulationRuntime(ctx, s, RuntimeOptions{Interval: 5 * time.Second, Step: time.Hour, Seed: 7, Outputs: outputs, Now: c.now})
	if n, err := r.StartFleet(); err != nil || n != len(devices) {
		t.Fatalf("StartFleet=%d err=%v", n, err)
	}
	return r, s, c
}

func run(r *SimulationRuntime, c *clock, seconds int) {
	for i := 0; i < seconds; i++ {
		r.step(c.add(time.Second))
	}
}

// settle waits until the output workers have drained what the steps queued.
func settle(outputs ...*recordingOutput) {
	deadline := time.Now().Add(2 * time.Second)
	last := -1
	for time.Now().Before(deadline) {
		n := 0
		for _, o := range outputs {
			n += len(o.deliveries())
		}
		if n == last {
			return
		}
		last = n
		time.Sleep(20 * time.Millisecond)
	}
}

func records(o *recordingOutput, id int64) []telemetry.Record {
	var out []telemetry.Record
	for _, d := range o.deliveries() {
		if d.DeviceID == id {
			out = append(out, d.Records...)
		}
	}
	return out
}

func TestEachDeviceSendsOnlyToItsOutput(t *testing.T) {
	mqtt, tcp, push := &recordingOutput{name: "mqtt"}, &recordingOutput{name: "teltonika"}, &recordingOutput{name: "http-push"}
	devices := []Device{
		seeded(1, "haul-truck", 1, "mqtt"), seeded(2, "farm-tractor", 1, "teltonika"), seeded(3, "water-truck", 1, "http-push"),
		seeded(4, "pickup", 1, "all"), seeded(5, "dozer", 1, "none"),
	}
	r, s, c := harness(t, devices, mqtt, tcp, push)
	run(r, c, 11)
	settle(mqtt, tcp, push)
	want := map[*recordingOutput][]int64{mqtt: {1, 4}, tcp: {2, 4}, push: {3, 4}}
	for o, ids := range want {
		got := map[int64]bool{}
		for _, d := range o.deliveries() {
			got[d.DeviceID] = true
		}
		if len(got) != len(ids) {
			t.Fatalf("%s got devices %v, want %v", o.name, got, ids)
		}
		for _, id := range ids {
			if !got[id] {
				t.Fatalf("%s is missing device %d: %v", o.name, id, got)
			}
		}
	}
	// A report every 5 s: two reports in 11 s for every device, the silent one included in the
	// simulator's own state.
	if n := len(records(mqtt, 1)); n < 2 || n > 3 {
		t.Fatalf("device 1 sent %d records in 11 s", n)
	}
	rec := records(tcp, 2)[0]
	for _, key := range []string{"ignition", "movement", "external_voltage", "battery_voltage", "power_cut", "gnss_status", "gsm_signal", "odometer", "din1"} {
		if _, ok := rec.Attributes[key]; !ok {
			t.Fatalf("tractor record lacks %s: %v", key, rec.Attributes)
		}
	}
	if _, ok := records(mqtt, 1)[0].Attributes["din1"]; ok {
		t.Fatal("a haul truck's profile does not report din1")
	}
	if s.persisted == 0 {
		t.Fatal("reports must be persisted")
	}
	if st := r.State(5); st.State == "stopped" || len(st.Outputs) != 0 {
		t.Fatalf("a device on none still runs but has no output: %+v", st)
	}
}

func TestRuntimeRunsWithoutBrowser(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := &recordingOutput{name: "mqtt"}
	s := &memStore{items: []Device{seeded(1, "haul-truck", 3, "mqtt")}, fleet: FleetState{Epoch: time.Now()}}
	r := NewSimulationRuntime(ctx, s, RuntimeOptions{Interval: 30 * time.Millisecond, Step: 10 * time.Millisecond, Seed: 7, Outputs: []Output{out}})
	if _, err := r.StartFleet(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(out.deliveries()) < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := len(out.deliveries()); n < 3 {
		t.Fatalf("delivered %d records without any request", n)
	}
}

func TestSlowOutputDoesNotStallTheClock(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	out := &recordingOutput{name: "teltonika", block: block}
	r, s, c := harness(t, []Device{seeded(1, "haul-truck", 2, "teltonika")}, out)
	first, _ := s.GetDevice(context.Background(), 1)
	run(r, c, 600)
	later, _ := s.GetDevice(context.Background(), 1)
	if first.Latitude == later.Latitude && first.Longitude == later.Longitude {
		t.Fatal("the unit stopped moving behind a blocked output")
	}
}

func TestBehavioursChangeWhatUnitsSend(t *testing.T) {
	out := &recordingOutput{name: "mqtt"}
	devices := []Device{seeded(1, "haul-truck", 1, "mqtt"), seeded(2, "haul-truck", 2, "mqtt"), seeded(3, "haul-truck", 3, "mqtt"),
		seeded(4, "haul-truck", 4, "mqtt"), seeded(5, "haul-truck", 5, "mqtt"), seeded(6, "haul-truck", 6, "mqtt")}
	r, _, c := harness(t, devices, out)
	give := func(id int64, kind string, p scenario.Params, d time.Duration) {
		t.Helper()
		if _, err := r.Give(scenario.Target{IDs: []int64{id}}, kind, p, 0, d); err != nil {
			t.Fatal(err)
		}
	}
	give(1, scenario.SignalLoss, nil, time.Minute)
	give(2, scenario.PowerCut, nil, time.Minute)
	give(3, scenario.Idle, nil, time.Minute)
	give(4, scenario.Park, scenario.Params{"heartbeat_s": 30.0}, 2*time.Minute)
	give(5, scenario.Intermittent, scenario.Params{"burst_s": 20.0}, time.Minute)
	give(6, scenario.Overspeed, scenario.Params{"kmh": 82.0}, time.Minute)
	run(r, c, 30)
	if st := r.State(1); st.State != "silent" {
		t.Fatalf("signal loss state %q", st.State)
	}
	run(r, c, 30)
	settle(out)

	if n := len(records(out, 1)); n != 0 {
		t.Fatalf("signal loss sent %d records", n)
	}
	cut := records(out, 2)
	if len(cut) == 0 || cut[0].Priority != 1 || cut[0].EventIO != 66 {
		t.Fatalf("power cut must open with a high-priority voltage event: %+v", cut)
	}
	for _, rec := range cut {
		if v, _ := rec.Number("external_voltage"); v != 0 {
			t.Fatalf("external voltage %.2f during a cut", v)
		}
		if b, _ := rec.Bool("power_cut"); !b {
			t.Fatal("power_cut must be true during a cut")
		}
	}
	for _, rec := range records(out, 3) {
		if ign, _ := rec.Bool("ignition"); !ign || rec.Position.SpeedKmh != 0 {
			t.Fatalf("idle record: ignition=%v speed=%v", ign, rec.Position.SpeedKmh)
		}
	}
	parked := records(out, 4)
	if len(parked) < 2 || len(parked) > 3 {
		t.Fatalf("park with a 30 s heartbeat sent %d records in 60 s", len(parked))
	}
	for _, rec := range parked {
		if ign, _ := rec.Bool("ignition"); ign {
			t.Fatal("a parked unit has its ignition off")
		}
	}
	bursts := 0
	for _, d := range out.deliveries() {
		if d.DeviceID == 5 && len(d.Records) > 1 {
			bursts++
		}
	}
	if bursts == 0 {
		t.Fatal("burst reporting must send several records at once")
	}
	fast := 0
	for _, rec := range records(out, 6) {
		if rec.Position.SpeedKmh > 70 {
			fast++
		}
	}
	if fast == 0 {
		t.Fatalf("overspeed never exceeded 70 km/h: %v", records(out, 6))
	}
	// Once the signal loss ends, the unit reports again.
	run(r, c, 20)
	settle(out)
	if len(records(out, 1)) == 0 {
		t.Fatal("the unit did not come back after the signal loss")
	}
}

func TestGeofenceExitLeavesAndComesBack(t *testing.T) {
	r, _, c := harness(t, []Device{seeded(1, "haul-truck", 31, "none")})
	if _, err := r.Give(scenario.Target{Names: []string{"HT-031"}}, scenario.GeofenceExit, scenario.Params{"beyond_m": 300.0, "wait_s": 30.0}, 0, 0); err != nil {
		t.Fatal(err)
	}
	left, back := false, false
	for i := 0; i < 3600 && !back; i++ {
		run(r, c, 1)
		r.mu.Lock()
		p := r.units[1].pos
		detouring := r.units[1].detour != nil
		r.mu.Unlock()
		outside := !world.Inside(p, world.OperatingArea)
		left = left || outside
		back = left && !outside && !detouring
	}
	if !left || !back {
		t.Fatalf("left=%v back=%v", left, back)
	}
}

func TestWorkBehaviourCoversTheCompartment(t *testing.T) {
	out := &recordingOutput{name: "teltonika"}
	r, _, c := harness(t, []Device{seeded(1, "farm-tractor", fleet.StandbyTractor, "teltonika")}, out)
	if _, err := r.Give(scenario.Target{Names: []string{"TR-004"}}, scenario.Work, scenario.Params{"compartment": "SLG-C07"}, 0, 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	run(r, c, 600)
	settle(out)
	working := 0
	for _, rec := range records(out, 1) {
		if din1, _ := rec.Bool("din1"); din1 {
			working++
			if !world.Inside(world.LonLat{rec.Position.Lon, rec.Position.Lat}, world.CompartmentC07) {
				t.Fatalf("working outside C07 at %v", rec.Position)
			}
		}
	}
	if working < 50 {
		t.Fatalf("only %d working records in 10 minutes", working)
	}
	// After the behaviour ends the tractor drives back to its standby spot.
	run(r, c, 600)
	r.mu.Lock()
	d := world.Distance(r.units[1].pos, fleet.StandbyPoint())
	r.mu.Unlock()
	if d > 5 {
		t.Fatalf("tractor is %.0f m from its standby spot after the work ended", d)
	}
}

func TestScenarioFiresOnTheTimelineAndResetClearsIt(t *testing.T) {
	r, s, c := harness(t, []Device{seeded(1, "haul-truck", 12, "mqtt")})
	if err := r.StartScenario("demo-30min"); err != nil {
		t.Fatal(err)
	}
	run(r, c, 119)
	if st := r.State(1); len(st.Behaviours) != 0 {
		t.Fatalf("overspeed fired early: %+v", st.Behaviours)
	}
	run(r, c, 3)
	st := r.State(1)
	if len(st.Behaviours) != 1 || st.Behaviours[0].Type != scenario.Overspeed || !st.Behaviours[0].Active {
		t.Fatalf("HT-012 behaviours at T+2 min: %+v", st.Behaviours)
	}
	view := r.Fleet().Scenario
	if view == nil || !view.Events[0].Fired || view.Events[1].Fired {
		t.Fatalf("countdown %+v", view)
	}
	if err := r.Reset(); err != nil {
		t.Fatal(err)
	}
	if r.Fleet().Scenario != nil || len(r.State(1).Behaviours) != 0 || len(s.behaviours) != 0 {
		t.Fatal("reset must stop the timeline and clear behaviours")
	}
	k, _ := fleet.KindByKey("haul-truck")
	plan := fleet.Plan(k, 12, 7)
	r.mu.Lock()
	p := r.units[1].pos
	r.mu.Unlock()
	if world.Distance(p, plan.At(plan.Phase).Pos) > 0.01 {
		t.Fatal("reset must put the unit at its T0 position")
	}
}

func TestRejectionsShowOnTheDevice(t *testing.T) {
	push := &recordingOutput{name: "http-push", err: Rejection{"unknown_device"}}
	r, _, c := harness(t, []Device{seeded(1, "water-truck", 1, "http-push")}, push)
	run(r, c, 6)
	settle(push)
	st := r.State(1).Outputs["http-push"]
	if st.Status != "rejected" || st.LastError != "unknown_device" {
		t.Fatalf("output state %+v", st)
	}
	var summary OutputSummary
	for _, o := range r.Fleet().Outputs {
		if o.Name == "http-push" {
			summary = o
		}
	}
	if summary.Rejected == 0 || summary.Failed != 0 || summary.Devices != 1 {
		t.Fatalf("summary %+v", summary)
	}
}

func TestRestartResumesTheItineraryClock(t *testing.T) {
	r, s, c := harness(t, []Device{seeded(1, "pickup", 4, "none")})
	run(r, c, 300)
	r.mu.Lock()
	before := r.units[1].clock
	r.mu.Unlock()
	d, _ := s.GetDevice(context.Background(), 1)
	if d.PlanClock == nil || math.Abs(*d.PlanClock-before) > 5 {
		t.Fatalf("persisted clock %v, runtime clock %.0f", d.PlanClock, before)
	}
	later := c.add(time.Minute)
	fresh := NewSimulationRuntime(context.Background(), s, RuntimeOptions{Seed: 7, Step: time.Hour, Now: func() time.Time { return later }})
	if _, err := fresh.StartFleet(); err != nil {
		t.Fatal(err)
	}
	fresh.mu.Lock()
	after := fresh.units[1].clock
	fresh.mu.Unlock()
	if math.Abs(after-(*d.PlanClock+later.Sub(d.UpdatedAt).Seconds())) > 0.001 {
		t.Fatalf("restarted clock %.1f, want the persisted clock plus the downtime", after)
	}
}
