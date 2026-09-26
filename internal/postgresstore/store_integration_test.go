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
