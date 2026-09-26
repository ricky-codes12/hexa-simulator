package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hexa-simulator/internal/teltonika"
)

func TestGatewayAcceptsTeltonikaAndForwardsNormalizedHTTP(t *testing.T) {
	received := make(chan NormalizedTelemetry, 1)
	sensor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-SECRET-KEY"); got != "test-secret" {
			t.Errorf("secret header = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("content type = %q", got)
		}
		var payload NormalizedTelemetry
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		received <- payload
		w.WriteHeader(http.StatusAccepted)
	}))
	defer sensor.Close()

	server, err := New(Config{ListenAddress: "127.0.0.1:0", SensorURL: sensor.URL, SecretKey: "test-secret", Timeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := server.Start(ctx); err != nil {
		t.Fatal(err)
	}

	when := time.Date(2026, 9, 26, 12, 30, 0, 0, time.UTC)
	client := teltonika.Client{Address: server.Addr(), Timeout: 2 * time.Second}
	if err := client.Send(context.Background(), teltonika.Telemetry{IMEI: "356307042441007", Timestamp: when, Latitude: -2.985, Longitude: 104.767, Speed: 42, Heading: 180, Ignition: true}); err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-received:
		if got.DeviceID != "356307042441007" || !got.Timestamp.Equal(when) || got.Location.Latitude != -2.985 || got.Location.Longitude != 104.767 || got.Speed != 42 || got.Heading != 180 || !got.Ignition || got.Status != "online" || got.Protocol != "teltonika-codec8e" {
			t.Fatalf("normalized payload = %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for normalized telemetry")
	}
}

func TestNewRequiresSensorConfiguration(t *testing.T) {
	if _, err := New(Config{ListenAddress: "127.0.0.1:0", SensorURL: "http://sensor"}); err == nil {
		t.Fatal("expected missing secret error")
	}
}

func TestGatewayDoesNotAcknowledgeWhenSensorRejectsTelemetry(t *testing.T) {
	sensor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer sensor.Close()
	server, err := New(Config{ListenAddress: "127.0.0.1:0", SensorURL: sensor.URL, SecretKey: "test-secret", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := server.Start(ctx); err != nil {
		t.Fatal(err)
	}
	client := teltonika.Client{Address: server.Addr(), Timeout: time.Second}
	err = client.Send(context.Background(), teltonika.Telemetry{IMEI: "356307042441007", Timestamp: time.Now().UTC(), Latitude: -2.985, Longitude: 104.767, Speed: 42, Heading: 180, Ignition: true})
	if err == nil {
		t.Fatal("expected Teltonika client failure when HEXA.SENSOR rejects telemetry")
	}
}
