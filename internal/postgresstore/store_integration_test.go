//go:build integration

package postgresstore

import (
	"context"
	"fmt"
	"hexa-simulator/internal/httpapi"
	"os"
	"testing"
	"time"
)

func TestPostgreSQLDeviceRoundTrip(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Fatal("TEST_DATABASE_URL is required")
	}
	s, err := Open(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	imei := fmt.Sprintf("test-%d", time.Now().UnixNano())
	d, err := s.CreateDevice(context.Background(), httpapi.DeviceInput{Name: "Integration Device", IMEI: imei, Model: "Teltonika FMC920"})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := s.UpdateTelemetry(context.Background(), d.ID, httpapi.TelemetryInput{Status: "online", Latitude: -6.2, Longitude: 106.8, Speed: 25, Heading: 120, Ignition: true})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != "online" || updated.Speed != 25 {
		t.Fatalf("unexpected device: %+v", updated)
	}
}

func TestEnsureDemoFleetFillsToTargetAndIsIdempotent(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Fatal("TEST_DATABASE_URL is required")
	}
	s, err := Open(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	var before int
	if err := s.db.QueryRowContext(context.Background(), `SELECT count(*) FROM devices`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	target := before + 7
	got, err := s.EnsureDemoFleet(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if got != target {
		t.Fatalf("first ensure total=%d want=%d", got, target)
	}
	got, err = s.EnsureDemoFleet(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if got != target {
		t.Fatalf("second ensure total=%d want=%d", got, target)
	}
	var total, distinctIMEI int
	if err := s.db.QueryRowContext(context.Background(), `SELECT count(*), count(DISTINCT imei) FROM devices`).Scan(&total, &distinctIMEI); err != nil {
		t.Fatal(err)
	}
	if total != target || distinctIMEI != total {
		t.Fatalf("total=%d distinct imei=%d want=%d", total, distinctIMEI, target)
	}
}
