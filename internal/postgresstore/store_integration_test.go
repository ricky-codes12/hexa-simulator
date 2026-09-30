//go:build integration

package postgresstore

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"hexa-simulator/internal/fleet"
	"hexa-simulator/internal/httpapi"
)

func open(t *testing.T) *Store {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Fatal("TEST_DATABASE_URL is required")
	}
	s, err := Open(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.CheckSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPostgreSQLDeviceRoundTrip(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	imei := fmt.Sprintf("test-%d", time.Now().UnixNano())
	d, err := s.CreateDevice(ctx, httpapi.DeviceInput{Name: "Integration Device", IMEI: imei, Model: "Teltonika FMC920", Kind: "pickup", Output: "mqtt", Estate: "MRT"})
	if err != nil {
		t.Fatal(err)
	}
	if d.Kind != "pickup" || d.Output != "mqtt" || d.Estate != "MRT" || d.PlanClock != nil {
		t.Fatalf("created %+v", d)
	}
	updated, err := s.UpdateTelemetry(ctx, d.ID, httpapi.TelemetryInput{Status: "online", Latitude: -2.02, Longitude: 104.06, Speed: 25, Heading: 120, Ignition: true})
	if err != nil || updated.Status != "online" || updated.Speed != 25 {
		t.Fatalf("updated %+v err=%v", updated, err)
	}
	output := "teltonika"
	if patched, err := s.UpdateDevice(ctx, d.ID, httpapi.DevicePatch{Output: &output}); err != nil || patched.Output != "teltonika" || patched.Kind != "pickup" {
		t.Fatalf("patched %+v err=%v", patched, err)
	}
	clock, at := 1234.5, time.Now().UTC().Truncate(time.Millisecond)
	if err := s.PersistTelemetry(ctx, []httpapi.TelemetryUpdate{{ID: d.ID, Status: "online", Latitude: -2.03, Longitude: 104.07, Speed: 41.5, Heading: 90,
		Ignition: true, Attributes: map[string]any{"odometer": 1834221.0, "power_cut": false}, PlanClock: &clock, At: at}}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetDevice(ctx, d.ID)
	if err != nil || got.PlanClock == nil || *got.PlanClock != clock || got.Attributes["odometer"] != 1834221.0 || !got.UpdatedAt.Equal(at) || got.Speed != 41.5 {
		t.Fatalf("persisted %+v clock=%v err=%v", got, got.PlanClock, err)
	}
	if err := s.DeleteDevice(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureFleetMatchesTheRosterAndKeepsOperatorChanges(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	roster := func(c fleet.Composition) []httpapi.SeedDevice {
		var out []httpapi.SeedDevice
		for _, u := range fleet.Roster(c, 7) {
			out = append(out, httpapi.SeedDevice{Name: u.Name, IMEI: u.IMEI, Model: u.Model, Kind: u.Kind.Key, Output: u.Output, Estate: u.Estate})
		}
		return out
	}
	big := roster(fleet.Composition{"haul-truck": 4, "pickup": 2})
	if _, added, _, err := s.EnsureFleet(ctx, big); err != nil || added != 6 {
		t.Fatalf("first ensure added=%d err=%v", added, err)
	}
	if _, added, removed, err := s.EnsureFleet(ctx, big); err != nil || added != 0 || removed != 0 {
		t.Fatalf("second ensure added=%d removed=%d err=%v", added, removed, err)
	}
	// An operator routes a seeded unit elsewhere; a restart must not undo it.
	var id int64
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM devices WHERE imei=$1`, big[0].IMEI).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetOutput(ctx, []int64{id}, "none"); err != nil {
		t.Fatal(err)
	}
	small := roster(fleet.Composition{"haul-truck": 4})
	if _, added, removed, err := s.EnsureFleet(ctx, small); err != nil || added != 0 || removed != 2 {
		t.Fatalf("shrink added=%d removed=%d err=%v", added, removed, err)
	}
	if d, err := s.GetDevice(ctx, id); err != nil || d.Output != "none" || !d.Seeded {
		t.Fatalf("operator change lost: %+v err=%v", d, err)
	}
	if _, _, removed, err := s.EnsureFleet(ctx, nil); err != nil || removed != 4 {
		t.Fatalf("empty roster removed=%d err=%v", removed, err)
	}
}

func TestBehavioursAndFleetState(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	d, err := s.CreateDevice(ctx, httpapi.DeviceInput{Name: "Behaviour Device", IMEI: fmt.Sprintf("b-%d", time.Now().UnixNano()), Model: "Teltonika FMC650", Kind: "dozer", Output: "teltonika", Estate: "SLG"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.DeleteDevice(ctx, d.ID)
	now := time.Now().UTC().Truncate(time.Millisecond)
	created, err := s.CreateBehaviours(ctx, []httpapi.Behaviour{
		{DeviceID: d.ID, Type: "power-cut", Params: map[string]any{}, StartsAt: now, EndsAt: now.Add(10 * time.Minute), Source: "scenario:forestry-30min"},
		{DeviceID: d.ID, Type: "idle", Params: map[string]any{}, StartsAt: now.Add(-time.Hour), EndsAt: now.Add(-time.Minute), Source: "manual"},
	})
	if err != nil || len(created) != 2 || created[0].ID == 0 {
		t.Fatalf("created %+v err=%v", created, err)
	}
	active, err := s.ActiveBehaviours(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, b := range active {
		found = found || (b.DeviceID == d.ID && b.Type == "power-cut")
		if b.DeviceID == d.ID && b.Type == "idle" {
			t.Fatal("an ended behaviour is not active")
		}
	}
	if !found {
		t.Fatalf("active behaviours %+v", active)
	}
	if n, err := s.DeleteBehaviours(ctx, httpapi.BehaviourFilter{Source: "scenario:forestry-30min"}); err != nil || n < 1 {
		t.Fatalf("deleted %d err=%v", n, err)
	}
	started := now
	if err := s.SaveFleetState(ctx, httpapi.FleetState{Epoch: now, Scenario: "forestry-30min", ScenarioStartedAt: &started}); err != nil {
		t.Fatal(err)
	}
	if st, err := s.FleetState(ctx); err != nil || st.Scenario != "forestry-30min" || st.ScenarioStartedAt == nil || !st.ScenarioStartedAt.Equal(now) {
		t.Fatalf("state %+v err=%v", st, err)
	}
	if err := s.ResetFleet(ctx, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if st, _ := s.FleetState(ctx); st.Scenario != "" || st.ScenarioStartedAt != nil {
		t.Fatalf("reset left %+v", st)
	}
}
