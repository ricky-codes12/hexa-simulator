package sensorpush

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSend(t *testing.T) {
	var auth string
	var got map[string]any
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusOK)
	}))
	defer s.Close()
	c := Client{URL: s.URL, Key: "secret", Timeout: time.Second}
	err := c.Send(context.Background(), Telemetry{HardwareID: "352093081234567", DeviceTime: time.Date(2026, 9, 26, 15, 27, 0, 0, time.UTC), Latitude: -2.9761, Longitude: 104.7754, Speed: 42, Heading: 187, Ignition: true, Movement: true})
	if err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer secret" {
		t.Fatalf("auth=%q", auth)
	}
	b, _ := json.Marshal(got)
	text := string(b)
	for _, want := range []string{`"schema":"hexa.sensor/telemetry/v1"`, `"hardware_id":"352093081234567"`, `"speed_kmh":42`, `"heading_deg":187`, `"ignition":true`, `"movement":true`} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s in %s", want, text)
		}
	}
}
func TestNon2xxIsError(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", http.StatusUnauthorized) }))
	defer s.Close()
	err := (Client{URL: s.URL, Key: "bad"}).Send(context.Background(), Telemetry{})
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err=%v", err)
	}
}
func TestTimeoutIsError(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(100 * time.Millisecond) }))
	defer s.Close()
	err := (Client{URL: s.URL, Key: "x", Timeout: 10 * time.Millisecond}).Send(context.Background(), Telemetry{})
	if err == nil {
		t.Fatal("expected timeout")
	}
}
