package sensorpush

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hexa-simulator/internal/telemetry"
)

func record(id string) telemetry.Record {
	return telemetry.Record{
		HardwareID: id, Time: time.Date(2026, 9, 26, 15, 27, 0, 0, time.UTC),
		Position:   &telemetry.Position{Lat: -2.0231, Lon: 104.0654, SpeedKmh: 42, HeadingDeg: 187, Satellites: 11, FixValid: true},
		Attributes: map[string]any{"ignition": true, "movement": true, "power_cut": false},
	}
}

func TestSendPostsTelemetryV1BatchAndReadsRejections(t *testing.T) {
	var auth string
	var got map[string]any
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"accepted":1,"duplicate":0,"rejected":1,"errors":[{"index":1,"reason":"unknown_device","detail":""}]}`))
	}))
	defer s.Close()
	res, err := Client{URL: s.URL, Key: "secret", Timeout: time.Second}.Send(context.Background(), []telemetry.Record{record("359815060000011"), record("359815060000029")})
	if err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer secret" {
		t.Fatalf("auth=%q", auth)
	}
	if res.Accepted != 1 || len(res.Errors) != 1 || res.Errors[0].Index != 1 || res.Errors[0].Reason != "unknown_device" {
		t.Fatalf("result %+v", res)
	}
	b, _ := json.Marshal(got)
	text := string(b)
	for _, want := range []string{`"schema":"hexa.sensor/telemetry/v1"`, `"hardware_id":"359815060000011"`, `"hardware_id":"359815060000029"`,
		`"device_time":"2026-09-26T15:27:00.000Z"`, `"speed_kmh":42`, `"heading_deg":187`, `"ignition":true`, `"power_cut":false`} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s in %s", want, text)
		}
	}
}

func TestNon2xxIsError(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", http.StatusUnauthorized) }))
	defer s.Close()
	_, err := Client{URL: s.URL, Key: "bad"}.Send(context.Background(), []telemetry.Record{record("1")})
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err=%v", err)
	}
}

func TestTimeoutIsError(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(100 * time.Millisecond) }))
	defer s.Close()
	_, err := Client{URL: s.URL, Key: "x", Timeout: 10 * time.Millisecond}.Send(context.Background(), []telemetry.Record{record("1")})
	if err == nil {
		t.Fatal("expected timeout")
	}
}
