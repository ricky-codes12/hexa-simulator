package httpapi

import (
	"context"
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
