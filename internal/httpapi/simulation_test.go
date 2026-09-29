package httpapi

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestServerRuntimeContinuesWithoutBrowser(t *testing.T) {
	s := &fakeStore{items: []Device{{ID: 1, Name: "Truck", IMEI: "352093081234567", Latitude: -2.985, Longitude: 104.785, UpdatedAt: time.Now()}}}
	f := &signalingForwarder{forwarded: make(chan struct{}, 8)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := NewSimulationRuntime(ctx, s, 10*time.Millisecond, f)
	_, err := r.Apply(1, SimulationControl{Action: "start", Mode: "manual", Speed: 40, Heading: 90})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.After(time.Second)
	for forwarded := 0; forwarded < 2; forwarded++ {
		select {
		case <-f.forwarded:
		case <-deadline:
			t.Fatalf("forwarded=%d want at least 2", forwarded)
		}
	}
	state := r.State(1)
	if !state.Running || state.LastTick.IsZero() {
		t.Fatalf("state=%+v", state)
	}
	before := s.items[0].Longitude
	time.Sleep(20 * time.Millisecond)
	if s.items[0].Longitude <= before {
		t.Fatal("runtime stopped progressing without UI requests")
	}
	_, _ = r.Apply(1, SimulationControl{Action: "stop"})
}

type signalingForwarder struct {
	forwarded chan struct{}
}

func (f *signalingForwarder) ForwardTelemetry(context.Context, Device) error {
	f.forwarded <- struct{}{}
	return nil
}

type namedFakeForwarder struct{ fakeForwarder }

func (f *namedFakeForwarder) OutputName() string { return "mqtt" }

func TestSimulationRuntimeReportsNamedOutputState(t *testing.T) {
	s := &fakeStore{items: []Device{{ID: 7, Name: "Truck", IMEI: "356307042441007", Latitude: -3.0, Longitude: 104.75, UpdatedAt: time.Now()}}}
	f := &namedFakeForwarder{}
	r := NewSimulationRuntime(context.Background(), s, time.Hour, f)
	if got := r.State(7).Outputs["mqtt"].Status; got != "ready" {
		t.Fatalf("initial mqtt status=%q", got)
	}
	if _, err := r.Apply(7, SimulationControl{Action: "start", Mode: "manual", Speed: 40, Heading: 90}); err != nil {
		t.Fatal(err)
	}
	var out OutputState
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		out = r.State(7).Outputs["mqtt"]
		if out.Status == "sending" && !out.LastOK.IsZero() {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if out.Status != "sending" || out.LastOK.IsZero() {
		t.Fatalf("mqtt output=%+v", out)
	}
	_, _ = r.Apply(7, SimulationControl{Action: "stop"})
}

func TestStartFleetRunsEveryDeviceWithoutBrowser(t *testing.T) {
	items := make([]Device, 350)
	for i := range items {
		items[i] = Device{
			ID:        int64(i + 1),
			Name:      fmt.Sprintf("Truck %03d", i+1),
			IMEI:      fmt.Sprintf("35630704%07d", 2441001+i),
			Latitude:  -3.02 + float64(i%14)*0.0055,
			Longitude: 104.715 + float64(i%25)*0.0068,
			Heading:   float64((i * 47) % 360),
			UpdatedAt: time.Now(),
		}
	}
	s := &fakeStore{items: items}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := NewSimulationRuntime(ctx, s, 70*time.Millisecond)
	started, err := r.StartFleet()
	if err != nil {
		t.Fatal(err)
	}
	if started != 350 {
		t.Fatalf("started=%d want 350", started)
	}
	if again, err := r.StartFleet(); err != nil || again != 0 {
		t.Fatalf("second StartFleet started=%d err=%v", again, err)
	}
	for _, d := range items {
		state := r.State(d.ID)
		if !state.Running || state.Paused {
			t.Fatalf("device %d state=%+v", d.ID, state)
		}
	}
	// StartFleet is a readiness barrier: once it returns, every newly-started
	// worker has persisted its initial telemetry tick. No scheduler sleep belongs
	// in this assertion.
	for _, d := range items {
		state := r.State(d.ID)
		if state.LastTick.IsZero() {
			t.Fatalf("device %d has no initial tick after StartFleet", d.ID)
		}
	}
	online := 0
	for _, d := range s.items {
		if d.Status == "online" && !d.UpdatedAt.IsZero() {
			online++
		}
	}
	if online != 350 {
		t.Fatalf("online=%d want 350 after runtime readiness", online)
	}
}

func TestFleetAutoRoutesSpreadDevicesDeterministically(t *testing.T) {
	routes := map[int]bool{}
	speeds := map[float64]bool{}
	directions := map[int]bool{}
	positions := map[string]bool{}
	for id := int64(1); id <= 350; id++ {
		state := SimulationState{}
		prepareAutoRoute(&state, id, -3.0, 104.78)
		routes[state.routeIndex] = true
		speeds[fleetSpeed(id)] = true
		directions[state.routeDirection] = true
		lat, lon, _ := moveOnAutoRoute(&state, -3.0, 104.78, fleetSpeed(id), 3)
		if lat == -3.0 && lon == 104.78 {
			t.Fatalf("device %d did not move", id)
		}
		if lat < -3.04 || lat > -2.93 || lon < 104.69 || lon > 104.91 {
			t.Fatalf("device %d seeded outside forestry map: %f,%f", id, lat, lon)
		}
		positions[fmt.Sprintf("%.6f,%.6f", lat, lon)] = true
	}
	if len(routes) != len(forestryRoutes) {
		t.Fatalf("used routes=%d want %d", len(routes), len(forestryRoutes))
	}
	if len(speeds) < 20 {
		t.Fatalf("fleet speed variants=%d want at least 20", len(speeds))
	}
	if !directions[-1] || !directions[1] {
		t.Fatalf("route directions=%v want both directions", directions)
	}
	if len(positions) < 300 {
		t.Fatalf("unique fleet positions=%d want at least 300", len(positions))
	}
}

type blockingForwarder struct {
	release <-chan struct{}
}

func (f blockingForwarder) OutputName() string { return "teltonika-direct" }
func (f blockingForwarder) ForwardTelemetry(ctx context.Context, d Device) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-f.release:
		return nil
	}
}

func TestSlowOutputDoesNotBlockSimulationClock(t *testing.T) {
	release := make(chan struct{})
	s := &fakeStore{items: []Device{{ID: 9, Name: "Truck", IMEI: "356307042441009", Latitude: -3.0, Longitude: 104.75, UpdatedAt: time.Now()}}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := NewSimulationRuntime(ctx, s, 10*time.Millisecond, blockingForwarder{release: release})
	if _, err := r.Apply(9, SimulationControl{Action: "start", Mode: "manual", Speed: 40, Heading: 90}); err != nil {
		t.Fatal(err)
	}
	first := s.items[0].Longitude
	time.Sleep(35 * time.Millisecond)
	second := s.items[0].Longitude
	if second <= first {
		t.Fatalf("simulation clock stalled behind output: first=%v second=%v", first, second)
	}
	close(release)
	_, _ = r.Apply(9, SimulationControl{Action: "stop"})
}

func TestSimulationRuntimeKeepsBoundedTransmissionHistory(t *testing.T) {
	s := &fakeStore{items: []Device{{ID: 11, Name: "Truck", IMEI: "356307042441011", Latitude: -3.0, Longitude: 104.75, UpdatedAt: time.Now()}}}
	f := &namedFakeForwarder{}
	r := NewSimulationRuntime(context.Background(), s, time.Hour, f)
	if _, err := r.Apply(11, SimulationControl{Action: "start", Mode: "manual", Speed: 40, Heading: 90}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && len(r.TransmissionLogs(11, 50)) == 0 {
		time.Sleep(time.Millisecond)
	}
	logs := r.TransmissionLogs(11, 50)
	if len(logs) == 0 {
		t.Fatal("expected transmission history")
	}
	if logs[0].Output != "mqtt" || logs[0].Status != "success" {
		t.Fatalf("log=%+v", logs[0])
	}
	if logs[0].Latitude == 0 || logs[0].Longitude == 0 {
		t.Fatalf("missing telemetry summary: %+v", logs[0])
	}
	r.ClearTransmissionLogs(11)
	if got := len(r.TransmissionLogs(11, 50)); got != 0 {
		t.Fatalf("logs after clear=%d", got)
	}
	_, _ = r.Apply(11, SimulationControl{Action: "stop"})
}
