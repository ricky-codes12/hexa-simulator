package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeStore struct {
	items   []Device
	pingErr error
}

func (f *fakeStore) Ping(context.Context) error { return f.pingErr }
func (f *fakeStore) ListDevices(context.Context) ([]Device, error) {
	return append([]Device(nil), f.items...), nil
}
func (f *fakeStore) CreateDevice(_ context.Context, input DeviceInput) (Device, error) {
	d := Device{ID: int64(len(f.items) + 1), Name: input.Name, IMEI: input.IMEI, Model: input.Model, Status: "offline", CreatedAt: time.Unix(1, 0).UTC(), UpdatedAt: time.Unix(1, 0).UTC()}
	f.items = append(f.items, d)
	return d, nil
}
func (f *fakeStore) UpdateTelemetry(_ context.Context, id int64, input TelemetryInput) (Device, error) {
	for i := range f.items {
		if f.items[i].ID == id {
			f.items[i].Status = input.Status
			f.items[i].Latitude = input.Latitude
			f.items[i].Longitude = input.Longitude
			f.items[i].Speed = input.Speed
			f.items[i].Heading = input.Heading
			f.items[i].Ignition = input.Ignition
			return f.items[i], nil
		}
	}
	return Device{}, errors.New("not found")
}

func TestHealthWorksWithoutDatabase(t *testing.T) {
	h := Handler("test", nil, "")
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"database_configured":false`) {
		t.Fatalf("status=%d body=%s", r.Code, r.Body.String())
	}
}
func TestConfiguredButUnreadyDatabaseFailsHealth(t *testing.T) {
	h := Handler("test", &fakeStore{pingErr: errors.New("no")}, "")
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if r.Code != 503 {
		t.Fatalf("status=%d", r.Code)
	}
}
func TestDeviceFlow(t *testing.T) {
	s := &fakeStore{}
	h := Handler("test", s, "")
	c := httptest.NewRecorder()
	h.ServeHTTP(c, httptest.NewRequest(http.MethodPost, "/api/devices", strings.NewReader(`{"name":"Truck 01","imei":"352093081234567","model":"Teltonika FMC920"}`)))
	if c.Code != 201 || !strings.Contains(c.Body.String(), "Truck 01") {
		t.Fatalf("create=%d %s", c.Code, c.Body.String())
	}
	u := httptest.NewRecorder()
	h.ServeHTTP(u, httptest.NewRequest(http.MethodPost, "/api/devices/1/telemetry", strings.NewReader(`{"status":"online","latitude":-6.2,"longitude":106.8,"speed":42,"heading":90,"ignition":true}`)))
	if u.Code != 200 || !strings.Contains(u.Body.String(), `"status":"online"`) {
		t.Fatalf("update=%d %s", u.Code, u.Body.String())
	}
}
