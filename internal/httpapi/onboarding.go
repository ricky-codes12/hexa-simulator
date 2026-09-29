package httpapi

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"hexa-simulator/internal/fleet"
	"hexa-simulator/internal/telemetry"
	"hexa-simulator/internal/world"
)

// The Hexa.Sensor onboarding package (plan S5): everything Sensor needs to take the simulator's
// units in, in one ZIP. device-types.json holds one Sensor device type per kind and transport,
// because a Sensor device type reads one plugin's data; the four CSV files are Sensor's device CSV
// import formats (its device-api-v1 §3), applied in their numbered order. Re-importing the same
// package changes nothing: assignments carry a fixed start.

// seededSince is when seeded units' assignments start. It is fixed so a second import finds the
// same rows and skips them.
var seededSince = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// sensorPlugin is the Sensor plugin that receives each transport.
func sensorPlugin(transport string) string {
	switch transport {
	case fleet.OutputMQTT:
		return "mqtt-subscribe"
	case fleet.OutputTeltonika:
		return "teltonika-tcp"
	}
	return "http-push"
}

func transportCode(transport string) string {
	switch transport {
	case fleet.OutputMQTT:
		return "mqtt"
	case fleet.OutputTeltonika:
		return "tcp"
	}
	return "http"
}

func transportName(transport string) string {
	switch transport {
	case fleet.OutputMQTT:
		return "MQTT"
	case fleet.OutputTeltonika:
		return "Teltonika TCP"
	}
	return "HTTP push"
}

// exportTransport is the transport a device registers for. A device on "all" or "none" has no
// single Sensor device type; it is registered for HTTP push, which "all" includes.
func exportTransport(d Device) string {
	switch d.Output {
	case fleet.OutputMQTT, fleet.OutputTeltonika, fleet.OutputHTTPPush:
		return d.Output
	}
	return fleet.OutputHTTPPush
}

func kindOf(d Device) *fleet.Kind {
	if k, ok := fleet.KindByKey(d.Kind); ok {
		return k
	}
	return fleet.DefaultKind
}

// DeviceTypeKey is the Sensor device type a device registers with: its kind and transport, such
// as haul-truck-mqtt. Names Sensor shows stay free of demo wording (Sensor ADR 0035).
func DeviceTypeKey(d Device) string {
	return kindOf(d).Key + "-" + transportCode(exportTransport(d))
}

type mappingField struct {
	Path     string   `json:"path"`
	Type     string   `json:"type"`
	Unit     string   `json:"unit,omitempty"`
	Scale    *float64 `json:"scale,omitempty"`
	BoolFrom []any    `json:"bool_from,omitempty"`
}

// sensorDeviceType is the body of Sensor's POST /api/v1/admin/device-profiles.
type sensorDeviceType struct {
	Key           string         `json:"key"`
	Name          string         `json:"name"`
	Vendor        string         `json:"vendor"`
	ModelFamily   string         `json:"model_family"`
	PluginKey     string         `json:"plugin_key"`
	PluginVersion string         `json:"plugin_version"`
	Mapping       map[string]any `json:"mapping"`
	IngestPolicy  map[string]any `json:"ingest_policy"`
}

// deviceType describes a kind on a transport as a Sensor device type. JSON transports map the
// telemetry-v1 record; generic plugins accept only their declared fields, so the attributes they
// do not declare are kept as plugin-local x.* keys. Teltonika TCP maps the FMC IO elements the
// simulator sends (IO 66 and 67 in mV, IO 16 in metres).
func deviceType(k *fleet.Kind, transport string) sensorDeviceType {
	fields := map[string]mappingField{
		"position":    {Path: "$.position", Type: "position"},
		"speed_kmh":   {Path: "$.position.speed_kmh", Type: "number", Unit: "km/h"},
		"heading_deg": {Path: "$.position.heading_deg", Type: "integer", Unit: "°"},
	}
	milli := 0.001
	onChange := []string{"ignition"}
	if transport == fleet.OutputTeltonika {
		fields["ignition"] = mappingField{Path: "$.io.239", Type: "bool", BoolFrom: []any{1}}
		fields["movement"] = mappingField{Path: "$.io.240", Type: "bool", BoolFrom: []any{1}}
		fields["external_voltage"] = mappingField{Path: "$.io.66", Type: "number", Unit: "V", Scale: &milli}
		fields["battery_voltage"] = mappingField{Path: "$.io.67", Type: "number", Unit: "V", Scale: &milli}
		fields["odometer"] = mappingField{Path: "$.io.16", Type: "number", Unit: "m"}
		fields["gsm_signal"] = mappingField{Path: "$.io.21", Type: "integer"}
		fields["gnss_status"] = mappingField{Path: "$.io.69", Type: "integer"}
		if k.Reports(telemetry.DIN1) {
			fields["din1"] = mappingField{Path: "$.io.1", Type: "bool", BoolFrom: []any{1}}
			onChange = append(onChange, "din1")
		}
	} else {
		for key, typ := range map[string]string{telemetry.Ignition: "bool", telemetry.Movement: "bool", telemetry.ExternalVoltage: "number", telemetry.BatteryVoltage: "number", telemetry.Odometer: "number"} {
			f := mappingField{Path: "$.attributes." + key, Type: typ}
			switch key {
			case telemetry.ExternalVoltage, telemetry.BatteryVoltage:
				f.Unit = "V"
			case telemetry.Odometer:
				f.Unit = "m"
			}
			fields[key] = f
		}
		for key, typ := range map[string]string{telemetry.PowerCut: "bool", telemetry.GSMSignal: "integer", telemetry.GNSSStatus: "integer"} {
			fields["x."+key] = mappingField{Path: "$.attributes." + key, Type: typ}
		}
		if k.Reports(telemetry.DIN1) {
			fields["x.din1"] = mappingField{Path: "$.attributes.din1", Type: "bool"}
			onChange = append(onChange, "x.din1")
		}
	}
	var stored []string
	for key := range fields {
		stored = append(stored, key)
	}
	sort.Strings(stored)
	store := map[string]any{"min_interval": "15s", "deadband": map[string]any{"speed_kmh": map[string]any{"abs": 5}}, "always_on_change": onChange, "max_silence": "5m"}
	if k.Reports(telemetry.DIN1) {
		// Work realisation measures coverage from the stored track: keep every record.
		store = map[string]any{"min_interval": "5s", "deadband": map[string]any{}, "always_on_change": onChange, "max_silence": "5m"}
	}
	model := strings.TrimPrefix(k.Model(1), "Teltonika ")
	return sensorDeviceType{
		Key: k.Key + "-" + transportCode(transport), Name: k.Name + " tracker (" + transportName(transport) + ")",
		Vendor: "Teltonika", ModelFamily: model, PluginKey: sensorPlugin(transport), PluginVersion: "1.0.0",
		Mapping: map[string]any{"hardware_id": "$.device.hardware_id", "device_time": "$.device_time", "fields": fields},
		IngestPolicy: map[string]any{
			"version":   1,
			"drop_when": []any{map[string]any{"any": []any{map[string]any{"field": "position.fix_valid", "op": "eq", "value": false}}, "effect": "drop_position"}},
			"store":     store,
			"raw":       map[string]any{"mode": "sample", "every": 20, "retention_days": 7},
			"route":     map[string]any{"history": true, "live": true, "forward": []any{"alarm", "state_change"}},
			"fields":    stored,
		},
	}
}

func assetCode(d Device) string {
	if d.Seeded {
		return d.Name
	}
	code := strings.ToUpper(safeKey(d.Name))
	if code == "DEVICE" {
		return fmt.Sprintf("SIM-%d", d.ID)
	}
	return code
}

func serial(d Device) string {
	if !d.Seeded || len(d.IMEI) < 15 {
		return ""
	}
	return strings.TrimPrefix(d.Model, "Teltonika ") + "-" + d.IMEI[8:14]
}

func labels(pairs ...string) string {
	var out []string
	for i := 0; i+1 < len(pairs); i += 2 {
		if pairs[i+1] != "" {
			out = append(out, pairs[i]+"="+pairs[i+1])
		}
	}
	return strings.Join(out, ";")
}

// OnboardingZIP builds the package for devices.
func OnboardingZIP(devices []Device) ([]byte, error) {
	sorted := append([]Device(nil), devices...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	estates := map[string]bool{}
	types := map[string]sensorDeviceType{}
	rows := map[string][][]string{}
	for _, d := range sorted {
		k, transport := kindOf(d), exportTransport(d)
		dt := deviceType(k, transport)
		types[dt.Key] = dt
		estate := d.Estate
		if estate == "" {
			estate = "KNG"
		}
		estates[estate] = true
		since := seededSince
		if !d.Seeded && !d.CreatedAt.IsZero() {
			since = d.CreatedAt.UTC().Truncate(time.Second)
		}
		code := assetCode(d)
		rows["devices"] = append(rows["devices"], []string{d.IMEI, dt.Key, serial(d), "", "", "", "", "", labels("source", "hexa-simulator", "kind", k.Key, "output", transport)})
		rows["assets"] = append(rows["assets"], []string{code, k.AssetType, d.Name, "", estate, labels("source", "hexa-simulator", "kind", k.Key)})
		rows["assignments"] = append(rows["assignments"], []string{d.IMEI, code, since.Format(time.RFC3339), ""})
	}
	for _, e := range world.Estates {
		if estates[e.Code] {
			rows["estates"] = append(rows["estates"], []string{e.Code, e.Name, ""})
		}
	}
	var typeList []sensorDeviceType
	for _, t := range types {
		typeList = append(typeList, t)
	}
	sort.Slice(typeList, func(i, j int) bool { return typeList[i].Key < typeList[j].Key })
	typesJSON, err := json.MarshalIndent(typeList, "", "  ")
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	archive := zip.NewWriter(&buf)
	write := func(name string, data []byte) error {
		entry, err := archive.Create(name)
		if err != nil {
			return err
		}
		_, err = entry.Write(data)
		return err
	}
	if err := write("README.md", []byte(onboardingReadme(len(sorted), typeList))); err != nil {
		return nil, err
	}
	if err := write("00_device_types.json", append(typesJSON, '\n')); err != nil {
		return nil, err
	}
	for _, f := range []struct{ name, kind string }{{"01_estates.csv", "estates"}, {"02_devices.csv", "devices"}, {"03_assets.csv", "assets"}, {"04_assignments.csv", "assignments"}} {
		data, err := csvBytes(csvHeaders[f.kind], rows[f.kind])
		if err != nil {
			return nil, err
		}
		if err := write(f.name, data); err != nil {
			return nil, err
		}
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

var csvHeaders = map[string][]string{
	"estates":     {"code", "name", "parent_code"},
	"devices":     {"hardware_id", "profile_key", "serial", "firmware", "sim_iccid", "sim_msisdn", "sim_operator", "external_ids", "labels"},
	"assets":      {"asset_code", "asset_type", "name", "plate_number", "estate_code", "labels"},
	"assignments": {"hardware_id", "asset_code", "valid_from", "valid_to"},
}

func csvBytes(header []string, rows [][]string) ([]byte, error) {
	var buffer bytes.Buffer
	w := csv.NewWriter(&buffer)
	if err := w.Write(header); err != nil {
		return nil, err
	}
	if err := w.WriteAll(rows); err != nil {
		return nil, err
	}
	return buffer.Bytes(), w.Error()
}

func onboardingReadme(devices int, types []sensorDeviceType) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Hexa.Sensor onboarding: %d Hexa.Simulator units\n\n", devices)
	b.WriteString("Apply in this order, as a tenant administrator:\n\n")
	b.WriteString("1. `00_device_types.json`: create each device type that does not exist yet with\n")
	b.WriteString("   `POST /api/v1/admin/device-profiles` (one object per request), or run\n")
	b.WriteString("   `hexa-simulator sensor-onboard`, which does all of this.\n")
	b.WriteString("2. Imports (CSV), in file order: `01_estates.csv` (kind `estates`), `02_devices.csv`\n")
	b.WriteString("   (`devices`), `03_assets.csv` (`assets`), `04_assignments.csv` (`assignments`).\n\n")
	b.WriteString("Device types in this package:\n\n")
	for _, t := range types {
		fmt.Fprintf(&b, "- `%s`: %s, read by the `%s` plugin\n", t.Key, t.Name, t.PluginKey)
	}
	b.WriteString("\nThe sources the units send to must exist: `mqtt-subscribe` on the broker the simulator\n")
	b.WriteString("publishes to (topic `+/data`), `teltonika-tcp` on the port in `TELTONIKA_GATEWAY_ADDR`, and\n")
	b.WriteString("`http-push` with the key in `SIM_SENSOR_PUSH_KEY`; `hexa-simulator sensor-onboard --sources`\n")
	b.WriteString("creates them. Importing the package again changes nothing.\n")
	return b.String()
}

func writeOnboarding(w http.ResponseWriter, devices []Device, filename string) {
	payload, err := OnboardingZIP(devices)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "build Sensor onboarding package")
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}
