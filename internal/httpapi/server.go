package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type Device struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	IMEI      string    `json:"imei"`
	Model     string    `json:"model"`
	Status    string    `json:"status"`
	Latitude  float64   `json:"latitude"`
	Longitude float64   `json:"longitude"`
	Speed     float64   `json:"speed"`
	Heading   float64   `json:"heading"`
	Ignition  bool      `json:"ignition"`
	UpdatedAt time.Time `json:"updated_at"`
	CreatedAt time.Time `json:"created_at"`
}

type DeviceInput struct {
	Name  string `json:"name"`
	IMEI  string `json:"imei"`
	Model string `json:"model"`
}

type TelemetryInput struct {
	Status    string  `json:"status"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Speed     float64 `json:"speed"`
	Heading   float64 `json:"heading"`
	Ignition  bool    `json:"ignition"`
}

type TelemetryForwarder interface {
	ForwardTelemetry(context.Context, Device) error
}

type DeviceSearchStore interface {
	SearchDevices(context.Context, string, int, int) ([]Device, int, error)
}

type DeviceLookupStore interface {
	GetDevice(context.Context, int64) (Device, error)
}

type DeviceStore interface {
	Ping(context.Context) error
	ListDevices(context.Context) ([]Device, error)
	CreateDevice(context.Context, DeviceInput) (Device, error)
	UpdateTelemetry(context.Context, int64, TelemetryInput) (Device, error)
	DeleteDevice(context.Context, int64) error
}

type healthResponse struct {
	Revision           string `json:"revision"`
	DatabaseConfigured bool   `json:"database_configured"`
	DatabaseReady      bool   `json:"database_ready"`
}

func Handler(revision string, store DeviceStore, webRoot string, forwarders ...TelemetryForwarder) http.Handler {
	return HandlerWithRuntime(revision, store, webRoot, nil, forwarders...)
}

func HandlerWithRuntime(revision string, store DeviceStore, webRoot string, runtime *SimulationRuntime, forwarders ...TelemetryForwarder) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(writer http.ResponseWriter, request *http.Request) {
		ready := false
		status := http.StatusOK
		if store != nil {
			ready = store.Ping(request.Context()) == nil
			if !ready {
				status = http.StatusServiceUnavailable
			}
		}
		writeJSON(writer, status, healthResponse{Revision: revision, DatabaseConfigured: store != nil, DatabaseReady: ready})
	})
	mux.HandleFunc("GET /api/devices", func(writer http.ResponseWriter, request *http.Request) {
		if store == nil {
			writeError(writer, http.StatusServiceUnavailable, "PostgreSQL is not configured; set DATABASE_URL")
			return
		}
		query := strings.TrimSpace(request.URL.Query().Get("query"))
		limit, offset := 0, 0
		if raw := request.URL.Query().Get("limit"); raw != "" {
			fmt.Sscanf(raw, "%d", &limit)
		}
		if raw := request.URL.Query().Get("offset"); raw != "" {
			fmt.Sscanf(raw, "%d", &offset)
		}
		if limit > 0 {
			if limit > 100 {
				limit = 100
			}
			if offset < 0 {
				offset = 0
			}
			if searchable, ok := store.(DeviceSearchStore); ok {
				items, total, err := searchable.SearchDevices(request.Context(), query, limit, offset)
				if err != nil {
					writeError(writer, http.StatusInternalServerError, "load devices")
					return
				}
				writeJSON(writer, http.StatusOK, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset})
				return
			}
		}
		items, err := store.ListDevices(request.Context())
		if err != nil {
			writeError(writer, http.StatusInternalServerError, "load devices")
			return
		}
		if query != "" {
			q := strings.ToLower(query)
			filtered := items[:0]
			for _, item := range items {
				if strings.Contains(strings.ToLower(item.Name), q) || strings.Contains(strings.ToLower(item.IMEI), q) || strings.Contains(strings.ToLower(item.Model), q) {
					filtered = append(filtered, item)
				}
			}
			items = filtered
		}
		total := len(items)
		if limit > 0 {
			end := offset + limit
			if offset > total {
				offset = total
			}
			if end > total {
				end = total
			}
			items = items[offset:end]
		}
		writeJSON(writer, http.StatusOK, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset})
	})
	mux.HandleFunc("GET /api/devices/{id}/simulation", func(writer http.ResponseWriter, request *http.Request) {
		if runtime == nil {
			writeError(writer, http.StatusServiceUnavailable, "server-side simulation runtime is unavailable")
			return
		}
		var id int64
		if _, err := fmtSscan(request.PathValue("id"), &id); err != nil || id < 1 {
			writeError(writer, http.StatusBadRequest, "invalid device id")
			return
		}
		writeJSON(writer, http.StatusOK, runtime.State(id))
	})
	mux.HandleFunc("GET /api/devices/{id}/transmissions", func(writer http.ResponseWriter, request *http.Request) {
		if runtime == nil {
			writeError(writer, http.StatusServiceUnavailable, "server-side simulation runtime is unavailable")
			return
		}
		var id int64
		if _, err := fmtSscan(request.PathValue("id"), &id); err != nil || id < 1 {
			writeError(writer, http.StatusBadRequest, "invalid device id")
			return
		}
		limit := int64(50)
		if raw := request.URL.Query().Get("limit"); raw != "" {
			if _, err := fmtSscan(raw, &limit); err != nil || limit < 1 || limit > 100 {
				writeError(writer, http.StatusBadRequest, "invalid limit")
				return
			}
		}
		writeJSON(writer, http.StatusOK, map[string]any{"items": runtime.TransmissionLogs(id, int(limit))})
	})
	mux.HandleFunc("DELETE /api/devices/{id}/transmissions", func(writer http.ResponseWriter, request *http.Request) {
		if runtime == nil {
			writeError(writer, http.StatusServiceUnavailable, "server-side simulation runtime is unavailable")
			return
		}
		var id int64
		if _, err := fmtSscan(request.PathValue("id"), &id); err != nil || id < 1 {
			writeError(writer, http.StatusBadRequest, "invalid device id")
			return
		}
		runtime.ClearTransmissionLogs(id)
		writer.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("POST /api/devices/{id}/simulation", func(writer http.ResponseWriter, request *http.Request) {
		if runtime == nil {
			writeError(writer, http.StatusServiceUnavailable, "server-side simulation runtime is unavailable")
			return
		}
		var id int64
		if _, err := fmtSscan(request.PathValue("id"), &id); err != nil || id < 1 {
			writeError(writer, http.StatusBadRequest, "invalid device id")
			return
		}
		var input SimulationControl
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			writeError(writer, http.StatusBadRequest, "invalid simulation payload")
			return
		}
		state, err := runtime.Apply(id, input)
		if err != nil {
			writeError(writer, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(writer, http.StatusOK, state)
	})
	mux.HandleFunc("POST /api/devices", func(writer http.ResponseWriter, request *http.Request) {
		if store == nil {
			writeError(writer, http.StatusServiceUnavailable, "PostgreSQL is not configured; set DATABASE_URL")
			return
		}
		var input DeviceInput
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			writeError(writer, http.StatusBadRequest, "invalid device payload")
			return
		}
		input.Name, input.IMEI, input.Model = strings.TrimSpace(input.Name), strings.TrimSpace(input.IMEI), strings.TrimSpace(input.Model)
		if input.Name == "" || input.IMEI == "" {
			writeError(writer, http.StatusBadRequest, "name and imei are required")
			return
		}
		if input.Model == "" {
			input.Model = "Teltonika FMC920"
		}
		item, err := store.CreateDevice(request.Context(), input)
		if err != nil {
			writeError(writer, http.StatusInternalServerError, "create device")
			return
		}
		writeJSON(writer, http.StatusCreated, item)
	})
	mux.HandleFunc("GET /api/devices/{id}/sensor-onboarding.zip", func(writer http.ResponseWriter, request *http.Request) {
		if store == nil {
			writeError(writer, http.StatusServiceUnavailable, "PostgreSQL is not configured; set DATABASE_URL")
			return
		}
		var id int64
		if _, err := fmtSscan(request.PathValue("id"), &id); err != nil || id < 1 {
			writeError(writer, http.StatusBadRequest, "invalid device id")
			return
		}
		items, err := store.ListDevices(request.Context())
		if err != nil {
			writeError(writer, http.StatusInternalServerError, "load devices")
			return
		}
		var device *Device
		for i := range items {
			if items[i].ID == id {
				device = &items[i]
				break
			}
		}
		if device == nil {
			writeError(writer, http.StatusNotFound, "device not found")
			return
		}
		payload, err := sensorOnboardingZIP(*device)
		if err != nil {
			writeError(writer, http.StatusInternalServerError, "build Sensor onboarding package")
			return
		}
		filename := fmt.Sprintf("hexa-sensor-onboarding-%s.zip", safeKey(device.Name))
		writer.Header().Set("Content-Type", "application/zip")
		writer.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
		writer.Header().Set("Cache-Control", "no-store")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write(payload)
	})
	mux.HandleFunc("POST /api/devices/{id}/telemetry", func(writer http.ResponseWriter, request *http.Request) {
		if store == nil {
			writeError(writer, http.StatusServiceUnavailable, "PostgreSQL is not configured; set DATABASE_URL")
			return
		}
		var id int64
		if _, err := fmtSscan(request.PathValue("id"), &id); err != nil || id < 1 {
			writeError(writer, http.StatusBadRequest, "invalid device id")
			return
		}
		var input TelemetryInput
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			writeError(writer, http.StatusBadRequest, "invalid telemetry payload")
			return
		}
		if input.Status != "online" && input.Status != "offline" {
			writeError(writer, http.StatusBadRequest, "status must be online or offline")
			return
		}
		if !validTelemetry(input) {
			writeError(writer, http.StatusBadRequest, "telemetry values are outside supported ranges")
			return
		}
		item, err := store.UpdateTelemetry(request.Context(), id, input)
		if err != nil {
			writeError(writer, http.StatusInternalServerError, "update telemetry")
			return
		}
		if item.Status == "online" {
			for _, forwarder := range forwarders {
				if forwarder == nil {
					continue
				}
				if err := forwarder.ForwardTelemetry(request.Context(), item); err != nil {
					writeError(writer, http.StatusBadGateway, "forward telemetry: "+err.Error())
					return
				}
			}
		}
		writeJSON(writer, http.StatusOK, item)
	})
	mux.HandleFunc("DELETE /api/devices/{id}", func(writer http.ResponseWriter, request *http.Request) {
		if store == nil {
			writeError(writer, http.StatusServiceUnavailable, "PostgreSQL is not configured; set DATABASE_URL")
			return
		}
		var id int64
		if _, err := fmtSscan(request.PathValue("id"), &id); err != nil || id < 1 {
			writeError(writer, http.StatusBadRequest, "invalid device id")
			return
		}
		if err := store.DeleteDevice(request.Context(), id); err != nil {
			writeError(writer, http.StatusInternalServerError, "delete device")
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	})
	if strings.TrimSpace(webRoot) != "" {
		root := filepath.Clean(webRoot)
		assets := http.FileServer(http.Dir(root))
		mux.Handle("GET /assets/", assets)
		mux.HandleFunc("GET /", func(writer http.ResponseWriter, request *http.Request) {
			if strings.HasPrefix(request.URL.Path, "/api/") || request.URL.Path == "/healthz" {
				http.NotFound(writer, request)
				return
			}
			index := filepath.Join(root, "index.html")
			if _, err := os.Stat(index); err != nil {
				http.NotFound(writer, request)
				return
			}
			http.ServeFile(writer, request, index)
		})
	}
	return mux
}

func validTelemetry(input TelemetryInput) bool {
	values := []float64{input.Latitude, input.Longitude, input.Speed, input.Heading}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return input.Latitude >= -90 && input.Latitude <= 90 &&
		input.Longitude >= -180 && input.Longitude <= 180 &&
		input.Speed >= 0 && input.Speed <= 400 &&
		input.Heading >= 0 && input.Heading < 360
}

func fmtSscan(value string, target *int64) (int, error) {
	var parsed int64
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0, &parseError{}
		}
		parsed = parsed*10 + int64(r-'0')
	}
	if value == "" {
		return 0, &parseError{}
	}
	*target = parsed
	return 1, nil
}

type parseError struct{}

func (*parseError) Error() string { return "invalid integer" }

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
func writeError(writer http.ResponseWriter, status int, message string) {
	writeJSON(writer, status, map[string]string{"error": message})
}

var nonKeyChars = regexp.MustCompile(`[^a-z0-9]+`)

func safeKey(value string) string {
	key := nonKeyChars.ReplaceAllString(strings.ToLower(strings.TrimSpace(value)), "-")
	key = strings.Trim(key, "-")
	if key == "" {
		return "device"
	}
	return key
}

func sensorProfileKey(model string) string {
	key := safeKey(model)
	if key == "teltonika-fmc920" {
		return "teltonika-fmc920-test"
	}
	return key
}

func sensorAssetCode(device Device) string {
	code := strings.ToUpper(safeKey(device.Name))
	if code == "DEVICE" {
		return fmt.Sprintf("SIM-%d", device.ID)
	}
	return code
}

func csvBytes(header, row []string) ([]byte, error) {
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	if err := writer.Write(header); err != nil {
		return nil, err
	}
	if err := writer.Write(row); err != nil {
		return nil, err
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func sensorOnboardingZIP(device Device) ([]byte, error) {
	assetCode := sensorAssetCode(device)
	files := []struct {
		name   string
		header []string
		row    []string
	}{
		{"01_estates.csv", []string{"code", "name", "parent_code"}, []string{"DEMO-ESTATE", "Demo Estate", ""}},
		{"02_devices.csv", []string{"hardware_id", "profile_key", "serial", "firmware", "sim_iccid", "sim_msisdn", "sim_operator", "external_ids", "labels"}, []string{device.IMEI, sensorProfileKey(device.Model), "", "", "", "", "", "", ""}},
		{"03_assets.csv", []string{"asset_code", "asset_type", "name", "plate_number", "estate_code", "labels"}, []string{assetCode, "Truck", device.Name, "", "DEMO-ESTATE", ""}},
		{"04_assignments.csv", []string{"hardware_id", "asset_code", "valid_from", "valid_to"}, []string{device.IMEI, assetCode, time.Now().UTC().Format(time.RFC3339), ""}},
	}
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for _, file := range files {
		data, err := csvBytes(file.header, file.row)
		if err != nil {
			return nil, err
		}
		entry, err := archive.Create(file.name)
		if err != nil {
			return nil, err
		}
		if _, err := entry.Write(data); err != nil {
			return nil, err
		}
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
