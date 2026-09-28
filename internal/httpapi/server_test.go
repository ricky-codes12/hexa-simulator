package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeForwarder struct {
	devices []Device
	err     error
}

func (f *fakeForwarder) ForwardTelemetry(_ context.Context, device Device) error {
	f.devices = append(f.devices, device)
	return f.err
}

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
func (f *fakeStore) DeleteDevice(_ context.Context, id int64) error {
	for i := range f.items {
		if f.items[i].ID == id {
			f.items = append(f.items[:i], f.items[i+1:]...)
			return nil
		}
	}
	return errors.New("not found")
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

func TestTelemetryValidation(t *testing.T) {
	s := &fakeStore{items: []Device{{ID: 1, Name: "Truck", IMEI: "1", Status: "offline"}}}
	h := Handler("test", s, "")
	for _, payload := range []string{
		`{"status":"online","latitude":91,"longitude":106.8,"speed":42,"heading":90,"ignition":true}`,
		`{"status":"online","latitude":-6.2,"longitude":181,"speed":42,"heading":90,"ignition":true}`,
		`{"status":"online","latitude":-6.2,"longitude":106.8,"speed":-1,"heading":90,"ignition":true}`,
		`{"status":"online","latitude":-6.2,"longitude":106.8,"speed":42,"heading":360,"ignition":true}`,
	} {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/api/devices/1/telemetry", strings.NewReader(payload)))
		if r.Code != http.StatusBadRequest {
			t.Fatalf("payload=%s status=%d body=%s", payload, r.Code, r.Body.String())
		}
	}
}

func TestDeleteDevice(t *testing.T) {
	s := &fakeStore{items: []Device{{ID: 1, Name: "Truck", IMEI: "1", Status: "offline"}}}
	h := Handler("test", s, "")
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest(http.MethodDelete, "/api/devices/1", nil))
	if r.Code != http.StatusNoContent || len(s.items) != 0 {
		t.Fatalf("status=%d items=%d body=%s", r.Code, len(s.items), r.Body.String())
	}
}

func TestOnlineTelemetryIsForwarded(t *testing.T) {
	s := &fakeStore{items: []Device{{ID: 1, Name: "Truck", IMEI: "352093081234567", Status: "offline"}}}
	f := &fakeForwarder{}
	h := Handler("test", s, "", f)
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/api/devices/1/telemetry", strings.NewReader(`{"status":"online","latitude":-6.2,"longitude":106.8,"speed":42,"heading":90,"ignition":true}`)))
	if r.Code != http.StatusOK || len(f.devices) != 1 || f.devices[0].IMEI != "352093081234567" {
		t.Fatalf("status=%d forwarded=%+v body=%s", r.Code, f.devices, r.Body.String())
	}
}

func TestGatewayFailureIsVisible(t *testing.T) {
	s := &fakeStore{items: []Device{{ID: 1, Name: "Truck", IMEI: "352093081234567", Status: "offline"}}}
	f := &fakeForwarder{err: errors.New("gateway unavailable")}
	h := Handler("test", s, "", f)
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/api/devices/1/telemetry", strings.NewReader(`{"status":"online","latitude":-6.2,"longitude":106.8,"speed":42,"heading":90,"ignition":true}`)))
	if r.Code != http.StatusBadGateway || !strings.Contains(r.Body.String(), "gateway unavailable") {
		t.Fatalf("status=%d body=%s", r.Code, r.Body.String())
	}
}

func TestSensorOnboardingZIP(t *testing.T) {
	s := &fakeStore{items: []Device{{ID: 2, Name: "Truck Test 01", IMEI: "352093081234568", Model: "Teltonika FMC920", Status: "offline"}}}
	h := Handler("test", s, "")
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/api/devices/2/sensor-onboarding.zip", nil))
	if r.Code != http.StatusOK || r.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("status=%d type=%s body=%s", r.Code, r.Header().Get("Content-Type"), r.Body.String())
	}
	zr, err := zip.NewReader(bytes.NewReader(r.Body.Bytes()), int64(r.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(zr.File) != 4 {
		t.Fatalf("files=%d", len(zr.File))
	}
	contents := map[string]string{}
	for _, file := range zr.File {
		rc, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		contents[file.Name] = string(data)
	}
	if !strings.Contains(contents["02_devices.csv"], "352093081234568,teltonika-fmc920-test") {
		t.Fatalf("devices csv=%q", contents["02_devices.csv"])
	}
	if !strings.Contains(contents["03_assets.csv"], "TRUCK-TEST-01,Truck,Truck Test 01,,DEMO-ESTATE") {
		t.Fatalf("assets csv=%q", contents["03_assets.csv"])
	}
	if !strings.Contains(contents["04_assignments.csv"], "352093081234568,TRUCK-TEST-01,") {
		t.Fatalf("assignments csv=%q", contents["04_assignments.csv"])
	}
}

func TestSensorOnboardingZIPMissingDevice(t *testing.T) {
	h := Handler("test", &fakeStore{}, "")
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/api/devices/99/sensor-onboarding.zip", nil))
	if r.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", r.Code, r.Body.String())
	}
}

func TestTOTPVerificationRFCVector(t *testing.T) {
	const secret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	if !verifyTOTP(secret, "287082", time.Unix(59, 0)) {
		t.Fatal("expected RFC 6238-derived six-digit TOTP to verify")
	}
	if verifyTOTP(secret, "000000", time.Unix(59, 0)) {
		t.Fatal("unexpected TOTP verification")
	}
}
