package httpapi

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"hexa-simulator/internal/fleet"
	"hexa-simulator/internal/scenario"
	"hexa-simulator/internal/world"
)

type healthResponse struct {
	Revision           string `json:"revision"`
	DatabaseConfigured bool   `json:"database_configured"`
	DatabaseReady      bool   `json:"database_ready"`
}

// Handler serves the API without a fleet runtime.
func Handler(revision string, store DeviceStore, webRoot string) http.Handler {
	return HandlerWithRuntime(revision, store, webRoot, nil)
}

// HandlerWithRuntime serves the API, the fleet runtime's controls and the web console.
func HandlerWithRuntime(revision string, store DeviceStore, webRoot string, runtime *SimulationRuntime) http.Handler {
	mux := http.NewServeMux()
	needStore := func(w http.ResponseWriter) bool {
		if store == nil {
			writeError(w, http.StatusServiceUnavailable, "PostgreSQL is not configured; set DATABASE_URL")
			return false
		}
		return true
	}
	needRuntime := func(w http.ResponseWriter) bool {
		if runtime == nil {
			writeError(w, http.StatusServiceUnavailable, "server-side simulation runtime is unavailable")
			return false
		}
		return true
	}
	deviceID := func(w http.ResponseWriter, r *http.Request) (int64, bool) {
		var id int64
		if _, err := fmtSscan(r.PathValue("id"), &id); err != nil || id < 1 {
			writeError(w, http.StatusBadRequest, "invalid device id")
			return 0, false
		}
		return id, true
	}

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ready := false
		status := http.StatusOK
		if store != nil {
			ready = store.Ping(r.Context()) == nil
			if !ready {
				status = http.StatusServiceUnavailable
			}
		}
		writeJSON(w, status, healthResponse{Revision: revision, DatabaseConfigured: store != nil, DatabaseReady: ready})
	})

	mux.HandleFunc("GET /api/devices", func(w http.ResponseWriter, r *http.Request) {
		if !needStore(w) {
			return
		}
		query := strings.TrimSpace(r.URL.Query().Get("query"))
		limit, offset := 0, 0
		if raw := r.URL.Query().Get("limit"); raw != "" {
			fmt.Sscanf(raw, "%d", &limit)
		}
		if raw := r.URL.Query().Get("offset"); raw != "" {
			fmt.Sscanf(raw, "%d", &offset)
		}
		if limit > 0 {
			limit = min(limit, 100)
			offset = max(offset, 0)
			if searchable, ok := store.(DeviceSearchStore); ok {
				items, total, err := searchable.SearchDevices(r.Context(), query, limit, offset)
				if err != nil {
					writeError(w, http.StatusInternalServerError, "load devices")
					return
				}
				writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset})
				return
			}
		}
		items, err := store.ListDevices(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "load devices")
			return
		}
		if query != "" {
			q := strings.ToLower(query)
			filtered := items[:0]
			for _, item := range items {
				for _, field := range []string{item.Name, item.IMEI, item.Model, item.Kind, item.Estate, item.Output} {
					if strings.Contains(strings.ToLower(field), q) {
						filtered = append(filtered, item)
						break
					}
				}
			}
			items = filtered
		}
		total := len(items)
		if limit > 0 {
			offset = min(offset, total)
			items = items[offset:min(offset+limit, total)]
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset})
	})

	mux.HandleFunc("POST /api/devices", func(w http.ResponseWriter, r *http.Request) {
		if !needStore(w) {
			return
		}
		var input DeviceInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid device payload")
			return
		}
		input.Name, input.IMEI, input.Model = strings.TrimSpace(input.Name), strings.TrimSpace(input.IMEI), strings.TrimSpace(input.Model)
		input.Kind, input.Output, input.Estate = strings.TrimSpace(input.Kind), strings.TrimSpace(input.Output), strings.ToUpper(strings.TrimSpace(input.Estate))
		if input.Name == "" || input.IMEI == "" {
			writeError(w, http.StatusBadRequest, "name and imei are required")
			return
		}
		if input.Kind == "" {
			input.Kind = fleet.DefaultKind.Key
		}
		kind, ok := fleet.KindByKey(input.Kind)
		if !ok {
			writeError(w, http.StatusBadRequest, "unknown device kind")
			return
		}
		if input.Model == "" {
			input.Model = kind.Model(1)
		}
		if input.Output == "" {
			input.Output = fleet.OutputAll
		}
		if !fleet.ValidOutput(input.Output) {
			writeError(w, http.StatusBadRequest, "output must be mqtt, teltonika, http-push, all or none")
			return
		}
		if input.Estate == "" {
			input.Estate = fleet.Plan(kind, 1, 0).Estate
		}
		if !validEstate(input.Estate) {
			writeError(w, http.StatusBadRequest, "estate must be KNG, MRT or SLG")
			return
		}
		item, err := store.CreateDevice(r.Context(), input)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "create device")
			return
		}
		if runtime != nil {
			runtime.AddDevice(item)
		}
		writeJSON(w, http.StatusCreated, item)
	})

	mux.HandleFunc("GET /api/devices/{id}", func(w http.ResponseWriter, r *http.Request) {
		lookup, ok := store.(DeviceLookupStore)
		if !needStore(w) || !ok {
			if store != nil {
				writeError(w, http.StatusServiceUnavailable, "device lookup is unavailable")
			}
			return
		}
		id, ok := deviceID(w, r)
		if !ok {
			return
		}
		item, err := lookup.GetDevice(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusNotFound, "device not found")
			return
		}
		writeJSON(w, http.StatusOK, item)
	})

	mux.HandleFunc("PATCH /api/devices/{id}", func(w http.ResponseWriter, r *http.Request) {
		fs, ok := store.(FleetStore)
		if !ok || !needStore(w) {
			if store != nil {
				writeError(w, http.StatusServiceUnavailable, "device updates need the fleet store")
			}
			return
		}
		id, ok := deviceID(w, r)
		if !ok {
			return
		}
		var patch DevicePatch
		if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
			writeError(w, http.StatusBadRequest, "invalid device patch")
			return
		}
		if patch.Name != nil && strings.TrimSpace(*patch.Name) == "" {
			writeError(w, http.StatusBadRequest, "name cannot be empty")
			return
		}
		if patch.Output != nil && !fleet.ValidOutput(*patch.Output) {
			writeError(w, http.StatusBadRequest, "output must be mqtt, teltonika, http-push, all or none")
			return
		}
		if patch.Kind != nil {
			if _, ok := fleet.KindByKey(*patch.Kind); !ok {
				writeError(w, http.StatusBadRequest, "unknown device kind")
				return
			}
		}
		if patch.Estate != nil && !validEstate(*patch.Estate) {
			writeError(w, http.StatusBadRequest, "estate must be KNG, MRT or SLG")
			return
		}
		item, err := fs.UpdateDevice(r.Context(), id, patch)
		if err != nil {
			writeError(w, http.StatusNotFound, "device not found")
			return
		}
		if runtime != nil {
			runtime.RefreshDevice(item)
		}
		writeJSON(w, http.StatusOK, item)
	})

	mux.HandleFunc("GET /api/devices/{id}/simulation", func(w http.ResponseWriter, r *http.Request) {
		if !needRuntime(w) {
			return
		}
		if id, ok := deviceID(w, r); ok {
			writeJSON(w, http.StatusOK, runtime.State(id))
		}
	})

	mux.HandleFunc("POST /api/devices/{id}/simulation", func(w http.ResponseWriter, r *http.Request) {
		if !needRuntime(w) {
			return
		}
		id, ok := deviceID(w, r)
		if !ok {
			return
		}
		var input SimulationControl
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid simulation payload")
			return
		}
		state, err := runtime.Apply(id, input)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, state)
	})

	mux.HandleFunc("GET /api/devices/{id}/sensor-onboarding.zip", func(w http.ResponseWriter, r *http.Request) {
		if !needStore(w) {
			return
		}
		id, ok := deviceID(w, r)
		if !ok {
			return
		}
		items, err := store.ListDevices(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "load devices")
			return
		}
		for _, d := range items {
			if d.ID == id {
				writeOnboarding(w, []Device{d}, fmt.Sprintf("hexa-sensor-onboarding-%s.zip", safeKey(d.Name)))
				return
			}
		}
		writeError(w, http.StatusNotFound, "device not found")
	})

	mux.HandleFunc("POST /api/devices/{id}/telemetry", func(w http.ResponseWriter, r *http.Request) {
		if !needStore(w) {
			return
		}
		id, ok := deviceID(w, r)
		if !ok {
			return
		}
		var input TelemetryInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid telemetry payload")
			return
		}
		if input.Status != "online" && input.Status != "offline" {
			writeError(w, http.StatusBadRequest, "status must be online or offline")
			return
		}
		if !validTelemetry(input) {
			writeError(w, http.StatusBadRequest, "telemetry values are outside supported ranges")
			return
		}
		item, err := store.UpdateTelemetry(r.Context(), id, input)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "update telemetry")
			return
		}
		if item.Status == "online" && runtime != nil {
			if err := runtime.Inject(r.Context(), item); err != nil {
				writeError(w, http.StatusBadGateway, "forward telemetry: "+err.Error())
				return
			}
		}
		writeJSON(w, http.StatusOK, item)
	})

	mux.HandleFunc("DELETE /api/devices/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !needStore(w) {
			return
		}
		id, ok := deviceID(w, r)
		if !ok {
			return
		}
		if err := store.DeleteDevice(r.Context(), id); err != nil {
			writeError(w, http.StatusInternalServerError, "delete device")
			return
		}
		if runtime != nil {
			runtime.RemoveDevice(id)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// ---- the fleet ---------------------------------------------------------------------------

	mux.HandleFunc("GET /api/world", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/geo+json")
		w.Header().Set("Cache-Control", "private, max-age=3600")
		_, _ = w.Write(world.GeoJSON())
	})

	mux.HandleFunc("GET /api/catalog", func(w http.ResponseWriter, r *http.Request) {
		type kindView struct {
			Key       string   `json:"key"`
			Name      string   `json:"name"`
			Code      string   `json:"code"`
			AssetType string   `json:"asset_type"`
			Fields    []string `json:"fields"`
		}
		type scenarioView struct {
			Name    string              `json:"name"`
			Title   string              `json:"title"`
			Summary string              `json:"summary"`
			LengthS float64             `json:"length_s"`
			Events  []ScenarioEventView `json:"events"`
		}
		var kinds []kindView
		for _, k := range fleet.Kinds {
			kinds = append(kinds, kindView{k.Key, k.Name, k.Code, k.AssetType, k.Fields})
		}
		var scenarios []scenarioView
		for _, s := range scenario.Scenarios {
			v := scenarioView{Name: s.Name, Title: s.Title, Summary: s.Summary, LengthS: s.Length.Seconds()}
			for _, ev := range s.Events {
				v.Events = append(v.Events, ScenarioEventView{AtS: ev.At.Seconds(), Type: ev.Type, Note: ev.Note})
			}
			scenarios = append(scenarios, v)
		}
		outputs := map[string]bool{}
		for _, name := range fleet.Transports {
			outputs[name] = runtime != nil && runtime.Configured(name)
		}
		writeJSON(w, http.StatusOK, map[string]any{"kinds": kinds, "behaviours": scenario.Catalog, "scenarios": scenarios, "estates": world.Estates, "outputs": outputs})
	})

	mux.HandleFunc("GET /api/fleet", func(w http.ResponseWriter, r *http.Request) {
		if needRuntime(w) {
			writeJSON(w, http.StatusOK, runtime.Fleet())
		}
	})

	mux.HandleFunc("GET /api/fleet/sensor-onboarding.zip", func(w http.ResponseWriter, r *http.Request) {
		if !needStore(w) {
			return
		}
		items, err := store.ListDevices(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "load devices")
			return
		}
		q := r.URL.Query()
		var picked []Device
		for _, d := range items {
			if (q.Get("kind") == "" || d.Kind == q.Get("kind")) && (q.Get("estate") == "" || d.Estate == q.Get("estate")) && (q.Get("output") == "" || d.Output == q.Get("output")) {
				picked = append(picked, d)
			}
		}
		if len(picked) == 0 {
			writeError(w, http.StatusNotFound, "no devices match")
			return
		}
		writeOnboarding(w, picked, "hexa-sensor-onboarding-fleet.zip")
	})

	type targetBody struct {
		Target scenario.Target `json:"target"`
	}
	mux.HandleFunc("POST /api/fleet/behaviours", func(w http.ResponseWriter, r *http.Request) {
		if !needRuntime(w) {
			return
		}
		var in struct {
			Target    scenario.Target `json:"target"`
			Type      string          `json:"type"`
			Params    scenario.Params `json:"params"`
			DelayS    float64         `json:"delay_s"`
			DurationS float64         `json:"duration_s"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "invalid behaviour payload")
			return
		}
		if in.DelayS < 0 || in.DelayS > 86400 || in.DurationS < 0 || in.DurationS > 7*86400 || math.IsNaN(in.DelayS+in.DurationS) {
			writeError(w, http.StatusBadRequest, "delay and duration are outside supported ranges")
			return
		}
		created, err := runtime.Give(in.Target, in.Type, in.Params, time.Duration(in.DelayS*float64(time.Second)), time.Duration(in.DurationS*float64(time.Second)))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if created == nil {
			created = []Behaviour{}
		}
		writeJSON(w, http.StatusCreated, map[string]any{"created": len(created), "items": created})
	})

	mux.HandleFunc("POST /api/fleet/behaviours/clear", func(w http.ResponseWriter, r *http.Request) {
		if !needRuntime(w) {
			return
		}
		var in targetBody
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "invalid target")
			return
		}
		n, err := runtime.ClearBehaviours(in.Target)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "clear behaviours")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"cleared": n})
	})

	mux.HandleFunc("POST /api/fleet/output", func(w http.ResponseWriter, r *http.Request) {
		if !needRuntime(w) {
			return
		}
		var in struct {
			Target scenario.Target `json:"target"`
			Output string          `json:"output"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "invalid output payload")
			return
		}
		n, err := runtime.SetOutputs(in.Target, in.Output)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"updated": n})
	})

	mux.HandleFunc("POST /api/fleet/run", func(w http.ResponseWriter, r *http.Request) {
		if !needRuntime(w) {
			return
		}
		var in struct {
			Target scenario.Target `json:"target"`
			Action string          `json:"action"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "invalid run payload")
			return
		}
		n, err := runtime.Run(in.Target, in.Action)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"updated": n})
	})

	mux.HandleFunc("POST /api/fleet/reset", func(w http.ResponseWriter, r *http.Request) {
		if !needRuntime(w) {
			return
		}
		if err := runtime.Reset(); err != nil {
			writeError(w, http.StatusInternalServerError, "reset the fleet")
			return
		}
		writeJSON(w, http.StatusOK, runtime.Fleet())
	})

	mux.HandleFunc("POST /api/scenarios/{name}/start", func(w http.ResponseWriter, r *http.Request) {
		if !needRuntime(w) {
			return
		}
		if err := runtime.StartScenario(r.PathValue("name")); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, runtime.Fleet().Scenario)
	})

	mux.HandleFunc("POST /api/scenarios/stop", func(w http.ResponseWriter, r *http.Request) {
		if !needRuntime(w) {
			return
		}
		if err := runtime.StopScenario(); err != nil {
			writeError(w, http.StatusInternalServerError, "stop the scenario")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	if strings.TrimSpace(webRoot) != "" {
		root := filepath.Clean(webRoot)
		assets := http.FileServer(http.Dir(root))
		mux.Handle("GET /assets/", assets)
		mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/healthz" {
				http.NotFound(w, r)
				return
			}
			index := filepath.Join(root, "index.html")
			if _, err := os.Stat(index); err != nil {
				http.NotFound(w, r)
				return
			}
			http.ServeFile(w, r, index)
		})
	}
	return mux
}

func validEstate(code string) bool {
	for _, e := range world.Estates {
		if e.Code == code {
			return true
		}
	}
	return false
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
