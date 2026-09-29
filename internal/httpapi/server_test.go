package httpapi

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func serve(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest(method, path, strings.NewReader(body)))
	return r
}

func TestHealthWorksWithoutDatabase(t *testing.T) {
	r := serve(Handler("test", nil, ""), http.MethodGet, "/healthz", "")
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"database_configured":false`) {
		t.Fatalf("status=%d body=%s", r.Code, r.Body.String())
	}
}

func TestConfiguredButUnreadyDatabaseFailsHealth(t *testing.T) {
	if r := serve(Handler("test", &memStore{pingErr: errors.New("no")}, ""), http.MethodGet, "/healthz", ""); r.Code != 503 {
		t.Fatalf("status=%d", r.Code)
	}
}

func TestDeviceFlow(t *testing.T) {
	s := &memStore{}
	h := Handler("test", s, "")
	c := serve(h, http.MethodPost, "/api/devices", `{"name":"Truck 01","imei":"352093081234567","model":"Teltonika FMC920"}`)
	if c.Code != 201 || !strings.Contains(c.Body.String(), `"kind":"haul-truck"`) || !strings.Contains(c.Body.String(), `"output":"all"`) {
		t.Fatalf("create=%d %s", c.Code, c.Body.String())
	}
	u := serve(h, http.MethodPost, "/api/devices/1/telemetry", `{"status":"online","latitude":-2.02,"longitude":104.06,"speed":42,"heading":90,"ignition":true}`)
	if u.Code != 200 || !strings.Contains(u.Body.String(), `"status":"online"`) {
		t.Fatalf("update=%d %s", u.Code, u.Body.String())
	}
	p := serve(h, http.MethodPatch, "/api/devices/1", `{"output":"mqtt","estate":"MRT"}`)
	if p.Code != 200 || !strings.Contains(p.Body.String(), `"output":"mqtt"`) || !strings.Contains(p.Body.String(), `"estate":"MRT"`) {
		t.Fatalf("patch=%d %s", p.Code, p.Body.String())
	}
	for _, bad := range []string{`{"output":"carrier-pigeon"}`, `{"kind":"tank"}`, `{"estate":"XYZ"}`, `{"name":" "}`} {
		if r := serve(h, http.MethodPatch, "/api/devices/1", bad); r.Code != 400 {
			t.Fatalf("patch %s = %d", bad, r.Code)
		}
	}
	for _, bad := range []string{`{"name":"A","imei":"1","kind":"tank"}`, `{"name":"A","imei":"1","output":"fax"}`, `{"name":"A","imei":"1","estate":"XYZ"}`} {
		if r := serve(h, http.MethodPost, "/api/devices", bad); r.Code != 400 {
			t.Fatalf("create %s = %d", bad, r.Code)
		}
	}
}

func TestTelemetryValidation(t *testing.T) {
	h := Handler("test", &memStore{items: []Device{{ID: 1, Name: "Truck", IMEI: "1", Status: "offline"}}}, "")
	for _, payload := range []string{
		`{"status":"online","latitude":91,"longitude":106.8,"speed":42,"heading":90,"ignition":true}`,
		`{"status":"online","latitude":-6.2,"longitude":181,"speed":42,"heading":90,"ignition":true}`,
		`{"status":"online","latitude":-6.2,"longitude":106.8,"speed":-1,"heading":90,"ignition":true}`,
		`{"status":"online","latitude":-6.2,"longitude":106.8,"speed":42,"heading":360,"ignition":true}`,
	} {
		if r := serve(h, http.MethodPost, "/api/devices/1/telemetry", payload); r.Code != http.StatusBadRequest {
			t.Fatalf("payload=%s status=%d body=%s", payload, r.Code, r.Body.String())
		}
	}
}

func TestDeleteDevice(t *testing.T) {
	s := &memStore{items: []Device{{ID: 1, Name: "Truck", IMEI: "1", Status: "offline"}}}
	if r := serve(Handler("test", s, ""), http.MethodDelete, "/api/devices/1", ""); r.Code != http.StatusNoContent || len(s.items) != 0 {
		t.Fatalf("status=%d items=%d body=%s", r.Code, len(s.items), r.Body.String())
	}
}

func TestInjectedTelemetryGoesToTheDeviceOutputOnly(t *testing.T) {
	mqtt, push := &recordingOutput{name: "mqtt"}, &recordingOutput{name: "http-push"}
	r, s, _ := harness(t, []Device{{ID: 1, Name: "Truck", IMEI: "352093081234567", Kind: "haul-truck", Output: "http-push", Status: "offline"}}, mqtt, push)
	h := HandlerWithRuntime("test", s, "", r)
	w := serve(h, http.MethodPost, "/api/devices/1/telemetry", `{"status":"online","latitude":-2.02,"longitude":104.06,"speed":42,"heading":90,"ignition":true}`)
	if w.Code != http.StatusOK || len(push.deliveries()) != 1 || len(mqtt.deliveries()) != 0 {
		t.Fatalf("status=%d http-push=%d mqtt=%d body=%s", w.Code, len(push.deliveries()), len(mqtt.deliveries()), w.Body.String())
	}
	if rec := push.deliveries()[0].Records[0]; rec.HardwareID != "352093081234567" || rec.Position.SpeedKmh != 42 {
		t.Fatalf("record %+v", rec)
	}
}

func TestForwardFailureIsVisible(t *testing.T) {
	push := &recordingOutput{name: "http-push", err: errors.New("gateway unavailable")}
	r, s, _ := harness(t, []Device{{ID: 1, Name: "Truck", IMEI: "352093081234567", Kind: "haul-truck", Output: "all", Status: "offline"}}, push)
	w := serve(HandlerWithRuntime("test", s, "", r), http.MethodPost, "/api/devices/1/telemetry", `{"status":"online","latitude":-2.02,"longitude":104.06,"speed":42,"heading":90,"ignition":true}`)
	if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), "gateway unavailable") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func unzip(t *testing.T, body []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		out[f.Name] = string(b)
	}
	return out
}

func TestFleetOnboardingPackage(t *testing.T) {
	devices := []Device{seeded(1, "haul-truck", 12, "mqtt"), seeded(2, "farm-tractor", 4, "teltonika"), seeded(3, "water-truck", 1, "http-push"),
		{ID: 4, Name: "Truck Test 01", IMEI: "352093081234568", Model: "Teltonika FMC920", Kind: "haul-truck", Output: "all", Estate: "MRT", CreatedAt: time.Date(2026, 9, 30, 1, 2, 3, 0, time.UTC)}}
	h := Handler("test", &memStore{items: devices}, "")
	r := serve(h, http.MethodGet, "/api/fleet/sensor-onboarding.zip", "")
	if r.Code != http.StatusOK || r.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("status=%d body=%s", r.Code, r.Body.String())
	}
	files := unzip(t, r.Body.Bytes())
	var types []map[string]any
	if err := json.Unmarshal([]byte(files["00_device_types.json"]), &types); err != nil {
		t.Fatal(err)
	}
	plugins := map[string]string{}
	for _, dt := range types {
		plugins[dt["key"].(string)] = dt["plugin_key"].(string)
		if strings.Contains(strings.ToLower(dt["name"].(string)), "simulat") {
			t.Fatalf("device type name %q shows demo wording in Sensor", dt["name"])
		}
	}
	want := map[string]string{"haul-truck-mqtt": "mqtt-subscribe", "farm-tractor-tcp": "teltonika-tcp", "water-truck-http": "http-push", "haul-truck-http": "http-push"}
	for k, p := range want {
		if plugins[k] != p {
			t.Fatalf("device types %v, want %s on %s", plugins, k, p)
		}
	}
	tractor := `"din1":{"path":"$.io.1","type":"bool","bool_from":[1]}`
	if !strings.Contains(files["00_device_types.json"], `"$.io.66"`) || !strings.Contains(strings.ReplaceAll(strings.ReplaceAll(files["00_device_types.json"], " ", ""), "\n", ""), tractor) {
		t.Fatalf("the Teltonika device type must map the FMC IO elements: %s", files["00_device_types.json"])
	}
	for _, row := range []string{
		fleetRow(devices[0], "haul-truck-mqtt"), fleetRow(devices[1], "farm-tractor-tcp"),
		"352093081234568,haul-truck-http,,,,,,,source=hexa-simulator;kind=haul-truck;output=http-push",
	} {
		if !strings.Contains(files["02_devices.csv"], row) {
			t.Fatalf("devices csv lacks %q:\n%s", row, files["02_devices.csv"])
		}
	}
	if !strings.Contains(files["03_assets.csv"], "HT-012,truck,HT-012,,") || !strings.Contains(files["03_assets.csv"], "TR-004,tractor,TR-004,,SLG") {
		t.Fatalf("assets csv:\n%s", files["03_assets.csv"])
	}
	if !strings.Contains(files["04_assignments.csv"], devices[0].IMEI+",HT-012,2026-01-01T00:00:00Z,") ||
		!strings.Contains(files["04_assignments.csv"], "352093081234568,TRUCK-TEST-01,2026-09-30T01:02:03Z,") {
		t.Fatalf("assignments csv:\n%s", files["04_assignments.csv"])
	}
	if !strings.HasPrefix(files["01_estates.csv"], "code,name,parent_code\n") || !strings.Contains(files["01_estates.csv"], "SLG,Sialang Concession,") {
		t.Fatalf("estates csv:\n%s", files["01_estates.csv"])
	}
	// A filter narrows the package; the single-device package still works.
	if r := serve(h, http.MethodGet, "/api/fleet/sensor-onboarding.zip?kind=farm-tractor", ""); !strings.Contains(unzip(t, r.Body.Bytes())["02_devices.csv"], devices[1].IMEI) ||
		strings.Contains(unzip(t, r.Body.Bytes())["02_devices.csv"], devices[0].IMEI) {
		t.Fatal("kind filter")
	}
	if r := serve(h, http.MethodGet, "/api/devices/2/sensor-onboarding.zip", ""); r.Code != 200 || !strings.Contains(unzip(t, r.Body.Bytes())["02_devices.csv"], devices[1].IMEI) {
		t.Fatalf("device package status=%d", r.Code)
	}
	if r := serve(h, http.MethodGet, "/api/devices/99/sensor-onboarding.zip", ""); r.Code != http.StatusNotFound {
		t.Fatalf("missing device status=%d", r.Code)
	}
}

func fleetRow(d Device, deviceType string) string {
	return d.IMEI + "," + deviceType + ","
}

func TestFleetEndpoints(t *testing.T) {
	mqtt := &recordingOutput{name: "mqtt"}
	r, s, _ := harness(t, []Device{seeded(1, "haul-truck", 12, "mqtt"), seeded(2, "pickup", 1, "mqtt")}, mqtt)
	h := HandlerWithRuntime("test", s, "", r)
	var view FleetView
	if w := serve(h, http.MethodGet, "/api/fleet", ""); w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &view) != nil || len(view.Units) != 2 || view.Counts["total"] != 2 {
		t.Fatalf("fleet=%d %s", w.Code, w.Body.String())
	}
	if w := serve(h, http.MethodPost, "/api/fleet/behaviours", `{"target":{"kind":"pickup"},"type":"signal-loss","duration_s":60}`); w.Code != 201 || !strings.Contains(w.Body.String(), `"created":1`) {
		t.Fatalf("behaviours=%d %s", w.Code, w.Body.String())
	}
	if w := serve(h, http.MethodPost, "/api/fleet/behaviours", `{"target":{"kind":"pickup"},"type":"teleport"}`); w.Code != 400 {
		t.Fatalf("unknown behaviour=%d", w.Code)
	}
	if w := serve(h, http.MethodPost, "/api/fleet/behaviours", `{"target":{"ids":[1]},"type":"overspeed","params":{"kmh":20}}`); w.Code != 400 {
		t.Fatalf("overspeed below the limit=%d", w.Code)
	}
	if w := serve(h, http.MethodPost, "/api/fleet/output", `{"target":{"names":["HT-012"]},"output":"teltonika"}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"updated":1`) {
		t.Fatalf("output=%d %s", w.Code, w.Body.String())
	}
	if d, _ := s.GetDevice(nil, 1); d.Output != "teltonika" || r.State(1).Output != "teltonika" {
		t.Fatalf("output not applied: store=%s runtime=%s", d.Output, r.State(1).Output)
	}
	if w := serve(h, http.MethodPost, "/api/scenarios/demo-30min/start", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"name":"demo-30min"`) {
		t.Fatalf("scenario=%d %s", w.Code, w.Body.String())
	}
	if w := serve(h, http.MethodPost, "/api/fleet/reset", ""); w.Code != 200 {
		t.Fatalf("reset=%d", w.Code)
	}
	if w := serve(h, http.MethodGet, "/api/world", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"Kenanga Estate"`) {
		t.Fatalf("world=%d", w.Code)
	}
	if w := serve(h, http.MethodGet, "/api/catalog", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"power-cut"`) || !strings.Contains(w.Body.String(), `"mqtt":true`) {
		t.Fatalf("catalog=%d %s", w.Code, w.Body.String())
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
