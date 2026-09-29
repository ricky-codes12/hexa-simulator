package httpapi

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestServerRuntimeContinuesWithoutBrowser(t *testing.T) {
	s := &fakeStore{items: []Device{{ID: 1, Name: "Truck", IMEI: "352093081234567", Latitude: -2.985, Longitude: 104.785, UpdatedAt: time.Now()}}}
	f := &fakeForwarder{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := NewSimulationRuntime(ctx, s, 10*time.Millisecond, f)
	_, err := r.Apply(1, SimulationControl{Action: "start", Mode: "manual", Speed: 40, Heading: 90})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(35 * time.Millisecond)
	state := r.State(1)
	if !state.Running || state.LastTick.IsZero() {
		t.Fatalf("state=%+v", state)
	}
	if len(f.devices) < 2 {
		t.Fatalf("forwarded=%d", len(f.devices))
	}
	before := s.items[0].Longitude
	time.Sleep(20 * time.Millisecond)
	if s.items[0].Longitude <= before {
		t.Fatal("runtime stopped progressing without UI requests")
	}
	_, _ = r.Apply(1, SimulationControl{Action: "stop"})
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
	out := r.State(7).Outputs["mqtt"]
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
	time.Sleep(170 * time.Millisecond)
	online := 0
	for _, d := range s.items {
		if d.Status == "online" && !d.UpdatedAt.IsZero() {
			online++
		}
	}
	if online != 350 {
		t.Fatalf("online=%d want 350", online)
	}
}
