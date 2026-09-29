package httpapi

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"
)

type SimulationControl struct {
	Action          string   `json:"action,omitempty"`
	Mode            string   `json:"mode,omitempty"`
	Speed           float64  `json:"speed,omitempty"`
	Heading         float64  `json:"heading,omitempty"`
	TargetLatitude  *float64 `json:"target_latitude,omitempty"`
	TargetLongitude *float64 `json:"target_longitude,omitempty"`
	Preset          string   `json:"preset,omitempty"`
}

type OutputState struct {
	Configured bool      `json:"configured"`
	Status     string    `json:"status"`
	LastOK     time.Time `json:"last_ok,omitempty"`
	LastError  string    `json:"last_error,omitempty"`
}

type NamedTelemetryForwarder interface {
	TelemetryForwarder
	OutputName() string
}

type SimulationState struct {
	DeviceID         int64                  `json:"device_id"`
	Running          bool                   `json:"running"`
	Paused           bool                   `json:"paused"`
	Mode             string                 `json:"mode"`
	Speed            float64                `json:"speed"`
	Heading          float64                `json:"heading"`
	TargetLatitude   *float64               `json:"target_latitude,omitempty"`
	TargetLongitude  *float64               `json:"target_longitude,omitempty"`
	Preset           string                 `json:"preset,omitempty"`
	LastTick         time.Time              `json:"last_tick,omitempty"`
	LastError        string                 `json:"last_error,omitempty"`
	Outputs          map[string]OutputState `json:"outputs,omitempty"`
	routeIndex       int
	waypointIndex    int
	routeDirection   int
	routeReady       bool
	routeSeedPending bool
}

type routeCoordinate struct{ latitude, longitude float64 }

var forestryRoutes = [][]routeCoordinate{
	{{-2.991, 104.701}, {-2.990, 104.730}, {-2.991, 104.758}, {-2.990, 104.784}, {-2.990, 104.812}, {-2.991, 104.842}, {-2.991, 104.875}, {-2.988, 104.898}},
	{{-3.016, 104.747}, {-3.006, 104.759}, {-2.998, 104.771}, {-2.990, 104.784}, {-2.978, 104.793}, {-2.967, 104.806}, {-2.962, 104.821}, {-2.964, 104.838}},
	{{-2.990, 104.784}, {-2.974, 104.772}, {-2.958, 104.762}, {-2.947, 104.746}},
	{{-2.990, 104.812}, {-3.006, 104.808}, {-3.019, 104.819}, {-3.027, 104.845}},
	{{-2.991, 104.875}, {-3.005, 104.882}, {-3.011, 104.895}},
}

type TransmissionLog struct {
	Timestamp time.Time `json:"timestamp"`
	Output    string    `json:"output"`
	Status    string    `json:"status"`
	LatencyMS int64     `json:"latency_ms"`
	Latitude  float64   `json:"latitude"`
	Longitude float64   `json:"longitude"`
	Speed     float64   `json:"speed"`
	Heading   float64   `json:"heading"`
	Error     string    `json:"error,omitempty"`
}

type outputTask struct {
	deviceID int64
	device   Device
}

type outputDispatcher struct {
	name      string
	forwarder TelemetryForwarder
	queue     chan outputTask
}

type SimulationRuntime struct {
	ctx         context.Context
	store       DeviceStore
	forwarders  []TelemetryForwarder
	dispatchers []*outputDispatcher
	interval    time.Duration
	mu          sync.Mutex
	states      map[int64]*SimulationState
	cancels     map[int64]context.CancelFunc
	logs        map[int64][]TransmissionLog
}

const (
	outputWorkers   = 16
	outputQueueSize = 700
)

func NewSimulationRuntime(ctx context.Context, store DeviceStore, interval time.Duration, forwarders ...TelemetryForwarder) *SimulationRuntime {
	if interval <= 0 {
		interval = 3 * time.Second
	}
	r := &SimulationRuntime{ctx: ctx, store: store, forwarders: forwarders, interval: interval, states: map[int64]*SimulationState{}, cancels: map[int64]context.CancelFunc{}, logs: map[int64][]TransmissionLog{}}
	for _, f := range forwarders {
		name := "output"
		if named, ok := f.(NamedTelemetryForwarder); ok {
			name = named.OutputName()
		}
		d := &outputDispatcher{name: name, forwarder: f, queue: make(chan outputTask, outputQueueSize)}
		r.dispatchers = append(r.dispatchers, d)
		for i := 0; i < outputWorkers; i++ {
			go r.outputWorker(d)
		}
	}
	return r
}

func (r *SimulationRuntime) outputWorker(d *outputDispatcher) {
	for {
		select {
		case <-r.ctx.Done():
			return
		case task := <-d.queue:
			started := time.Now()
			err := d.forwarder.ForwardTelemetry(r.ctx, task.device)
			r.recordOutput(task.deviceID, d.name, task.device.UpdatedAt, time.Since(started), task.device, err)
		}
	}
}

func (r *SimulationRuntime) enqueueOutput(d *outputDispatcher, task outputTask) {
	select {
	case d.queue <- task:
	default:
		r.recordOutput(task.deviceID, d.name, time.Time{}, 0, task.device, fmt.Errorf("output queue full"))
	}
}

func (r *SimulationRuntime) recordOutput(deviceID int64, name string, at time.Time, latency time.Duration, device Device, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.states[deviceID]
	if s == nil {
		return
	}
	if s.Outputs == nil {
		s.Outputs = map[string]OutputState{}
	}
	result := OutputState{Configured: true, Status: "sending"}
	if err != nil {
		result.Status = "error"
		result.LastError = err.Error()
		s.LastError = fmt.Sprintf("%s: %v", name, err)
	} else {
		result.LastOK = at
	}
	s.Outputs[name] = result
	status := "success"
	errText := ""
	if err != nil {
		status = "error"
		errText = err.Error()
	}
	entry := TransmissionLog{Timestamp: time.Now().UTC(), Output: name, Status: status, LatencyMS: latency.Milliseconds(), Latitude: device.Latitude, Longitude: device.Longitude, Speed: device.Speed, Heading: device.Heading, Error: errText}
	entries := append([]TransmissionLog{entry}, r.logs[deviceID]...)
	if len(entries) > 100 {
		entries = entries[:100]
	}
	r.logs[deviceID] = entries
}

func (r *SimulationRuntime) TransmissionLogs(deviceID int64, limit int) []TransmissionLog {
	r.mu.Lock()
	defer r.mu.Unlock()
	entries := r.logs[deviceID]
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if len(entries) < limit {
		limit = len(entries)
	}
	out := make([]TransmissionLog, limit)
	copy(out, entries[:limit])
	return out
}

func (r *SimulationRuntime) ClearTransmissionLogs(deviceID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.logs, deviceID)
}

type fleetInitialResult struct {
	id  int64
	err error
}

// StartFleet restores the server-owned demo runtime after process startup. It
// starts every persisted device exactly once without going through the HTTP
// control path, so a Runtime activation immediately owns all fleet clocks even
// when no browser is open. Initial ticks are evenly phased across one interval
// to avoid a 350-device thundering herd against PostgreSQL and configured
// protocol outputs.
func (r *SimulationRuntime) StartFleet() (int, error) {
	items, err := r.store.ListDevices(r.ctx)
	if err != nil {
		return 0, err
	}
	if len(items) == 0 {
		return 0, nil
	}

	ready := make(chan fleetInitialResult, len(items))

	r.mu.Lock()
	started := 0
	for i := range items {
		d := items[i]
		s := r.states[d.ID]
		if s == nil {
			speed := fleetSpeed(d.ID)
			s = &SimulationState{DeviceID: d.ID, Running: true, Mode: "auto", Speed: speed, Heading: d.Heading, Outputs: r.configuredOutputs()}
			prepareAutoRoute(s, d.ID, d.Latitude, d.Longitude)
			r.states[d.ID] = s
		} else {
			s.Running = true
			s.Paused = false
		}
		if _, exists := r.cancels[d.ID]; exists {
			continue
		}
		ctx, cancel := context.WithCancel(r.ctx)
		r.cancels[d.ID] = cancel
		delay := time.Duration(int64(r.interval) * int64(i) / int64(len(items)))
		go r.loopPhased(ctx, d.ID, delay, ready)
		started++
	}
	r.mu.Unlock()

	// Startup readiness is a completion barrier, not a scheduler guess. Every
	// newly-created worker must finish its phased initial persistence tick before
	// StartFleet reports the fleet as ready. This keeps regular ticks phased while
	// making Runtime activation deterministic for the full fleet.
	for i := 0; i < started; i++ {
		select {
		case <-r.ctx.Done():
			return started, r.ctx.Err()
		case result := <-ready:
			if result.err != nil {
				return started, fmt.Errorf("initial telemetry for device %d: %w", result.id, result.err)
			}
		}
	}
	return started, nil
}

func (r *SimulationRuntime) loopPhased(ctx context.Context, id int64, delay time.Duration, ready chan<- fleetInitialResult) {
	if delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			ready <- fleetInitialResult{id: id, err: ctx.Err()}
			return
		case <-timer.C:
		}
	}
	err := r.tickOnce(id)
	ready <- fleetInitialResult{id: id, err: err}
	if err != nil {
		return
	}
	r.loop(ctx, id)
}

func (r *SimulationRuntime) configuredOutputs() map[string]OutputState {
	out := map[string]OutputState{}
	for _, f := range r.forwarders {
		if named, ok := f.(NamedTelemetryForwarder); ok {
			out[named.OutputName()] = OutputState{Configured: true, Status: "ready"}
		}
	}
	return out
}

func cloneOutputs(in map[string]OutputState) map[string]OutputState {
	out := map[string]OutputState{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (r *SimulationRuntime) State(id int64) SimulationState {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s := r.states[id]; s != nil {
		out := *s
		out.Outputs = cloneOutputs(s.Outputs)
		return out
	}
	return SimulationState{DeviceID: id, Mode: "auto", Speed: 40, Outputs: r.configuredOutputs()}
}

func (r *SimulationRuntime) Apply(id int64, in SimulationControl) (SimulationState, error) {
	d, err := r.device(id)
	if err != nil {
		return SimulationState{}, err
	}
	r.mu.Lock()
	s := r.states[id]
	if s == nil {
		s = &SimulationState{DeviceID: id, Mode: "auto", Speed: 40, Heading: d.Heading, Outputs: r.configuredOutputs()}
		prepareAutoRoute(s, id, d.Latitude, d.Longitude)
		r.states[id] = s
	}
	if in.Mode != "" {
		if in.Mode != "auto" && in.Mode != "manual" && in.Mode != "target" {
			r.mu.Unlock()
			return SimulationState{}, fmt.Errorf("invalid simulation mode")
		}
		s.Mode = in.Mode
	}
	if in.Speed >= 0 && in.Speed <= 180 && (in.Speed != 0 || in.Action == "control") {
		s.Speed = in.Speed
	}
	if in.Heading >= 0 && in.Heading < 360 {
		s.Heading = in.Heading
	}
	if in.TargetLatitude != nil || in.TargetLongitude != nil {
		s.TargetLatitude = in.TargetLatitude
		s.TargetLongitude = in.TargetLongitude
	}
	if in.Preset != "" {
		s.Preset = in.Preset
	}
	action := in.Action
	switch action {
	case "start", "resume":
		s.Running = true
		s.Paused = false
		if _, ok := r.cancels[id]; !ok {
			ctx, cancel := context.WithCancel(r.ctx)
			r.cancels[id] = cancel
			go r.loop(ctx, id)
		}
	case "pause":
		s.Paused = true
	case "stop":
		s.Running = false
		s.Paused = false
		if cancel := r.cancels[id]; cancel != nil {
			cancel()
			delete(r.cancels, id)
		}
	case "control", "":
	default:
		r.mu.Unlock()
		return SimulationState{}, fmt.Errorf("invalid simulation action")
	}
	out := *s
	r.mu.Unlock()
	if action == "start" || action == "resume" {
		r.tick(id)
	}
	if action == "stop" {
		_, _ = r.store.UpdateTelemetry(r.ctx, id, TelemetryInput{Status: "offline", Latitude: d.Latitude, Longitude: d.Longitude, Speed: 0, Heading: d.Heading, Ignition: false})
	}
	return out, nil
}

func (r *SimulationRuntime) loop(ctx context.Context, id int64) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.tick(id)
		}
	}
}

func (r *SimulationRuntime) tickOnce(id int64) error {
	r.mu.Lock()
	s := r.states[id]
	if s == nil || !s.Running || s.Paused {
		r.mu.Unlock()
		return nil
	}
	state := *s
	r.mu.Unlock()
	d, err := r.device(id)
	if err != nil {
		r.setError(id, err)
		return err
	}
	lat, lon, heading := d.Latitude, d.Longitude, state.Heading
	if lat == 0 && lon == 0 {
		lat = -2.985
		lon = 104.785
	}
	if state.Mode == "auto" {
		prepareAutoRoute(&state, id, lat, lon)
		lat, lon, heading = moveOnAutoRoute(&state, lat, lon, state.Speed, r.interval.Seconds())
		state.Heading = heading
	} else if state.Mode == "target" && state.TargetLatitude != nil && state.TargetLongitude != nil {
		heading = bearing(lat, lon, *state.TargetLatitude, *state.TargetLongitude)
		state.Heading = heading
		lat, lon = move(lat, lon, heading, state.Speed, r.interval.Seconds())
	} else {
		lat, lon = move(lat, lon, heading, state.Speed, r.interval.Seconds())
	}
	if state.Mode == "target" && state.TargetLatitude != nil && state.TargetLongitude != nil && distance(lat, lon, *state.TargetLatitude, *state.TargetLongitude) < 8 {
		lat = *state.TargetLatitude
		lon = *state.TargetLongitude
		state.Speed = 0
	}
	updated, err := r.store.UpdateTelemetry(r.ctx, id, TelemetryInput{Status: "online", Latitude: lat, Longitude: lon, Speed: state.Speed, Heading: heading, Ignition: true})
	if err != nil {
		r.setError(id, err)
		return err
	}
	r.mu.Lock()
	if current := r.states[id]; current != nil {
		current.Heading = heading
		current.Speed = state.Speed
		current.LastTick = updated.UpdatedAt
		current.routeIndex = state.routeIndex
		current.waypointIndex = state.waypointIndex
		current.routeDirection = state.routeDirection
		current.routeReady = state.routeReady
		current.routeSeedPending = state.routeSeedPending
	}
	r.mu.Unlock()
	for _, d := range r.dispatchers {
		r.enqueueOutput(d, outputTask{deviceID: id, device: updated})
	}
	return nil
}

func (r *SimulationRuntime) tick(id int64) {
	_ = r.tickOnce(id)
}

func (r *SimulationRuntime) device(id int64) (Device, error) {
	if lookup, ok := r.store.(DeviceLookupStore); ok {
		return lookup.GetDevice(r.ctx, id)
	}
	items, err := r.store.ListDevices(r.ctx)
	if err != nil {
		return Device{}, err
	}
	for i := range items {
		if items[i].ID == id {
			return items[i], nil
		}
	}
	return Device{}, fmt.Errorf("device not found")
}

func (r *SimulationRuntime) setError(id int64, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s := r.states[id]; s != nil {
		s.LastError = err.Error()
	}
}
func move(lat, lon, heading, speed, seconds float64) (float64, float64) {
	m := speed * 1000 / 3600 * seconds
	rad := heading * math.Pi / 180
	return lat + (m*math.Cos(rad))/111320, lon + (m*math.Sin(rad))/(111320*math.Max(.2, math.Cos(lat*math.Pi/180)))
}
func distance(a, b, c, d float64) float64 {
	lat := (a + c) * math.Pi / 360
	return math.Hypot((d-b)*111320*math.Cos(lat), (c-a)*111320)
}
func bearing(lat, lon, tlat, tlon float64) float64 {
	a := lat * math.Pi / 180
	b := tlat * math.Pi / 180
	dl := (tlon - lon) * math.Pi / 180
	y := math.Sin(dl) * math.Cos(b)
	x := math.Cos(a)*math.Sin(b) - math.Sin(a)*math.Cos(b)*math.Cos(dl)
	return math.Mod(math.Atan2(y, x)*180/math.Pi+360, 360)
}
func fleetSpeed(id int64) float64 { return 24 + float64((id*17)%43) }

func prepareAutoRoute(state *SimulationState, id int64, lat, lon float64) {
	if state.routeReady {
		return
	}
	state.DeviceID = id
	state.routeIndex = int((id*37 + id/5) % int64(len(forestryRoutes)))
	state.routeDirection = 1
	if id%2 == 0 {
		state.routeDirection = -1
	}
	route := forestryRoutes[state.routeIndex]
	nearest, nearestDistance := 0, math.MaxFloat64
	for i, point := range route {
		if d := distance(lat, lon, point.latitude, point.longitude); d < nearestDistance {
			nearest, nearestDistance = i, d
		}
	}
	state.waypointIndex = nearest
	state.routeReady = true
	state.routeSeedPending = true
}

func moveOnAutoRoute(state *SimulationState, lat, lon, speed, seconds float64) (float64, float64, float64) {
	route := forestryRoutes[state.routeIndex]
	if state.routeSeedPending {
		segment := int((state.DeviceID*13 + int64(state.routeIndex)*7) % int64(len(route)))
		next := (segment + 1) % len(route)
		fraction := 0.15 + float64((state.DeviceID*29)%70)/100
		lat = route[segment].latitude + (route[next].latitude-route[segment].latitude)*fraction
		lon = route[segment].longitude + (route[next].longitude-route[segment].longitude)*fraction
		if state.routeDirection > 0 {
			state.waypointIndex = next
		} else {
			state.waypointIndex = segment
		}
		state.routeSeedPending = false
	}
	target := route[state.waypointIndex]
	stepDistance := speed * 1000 / 3600 * seconds
	remaining := distance(lat, lon, target.latitude, target.longitude)
	if remaining <= math.Max(8, stepDistance*1.2) {
		lat, lon = target.latitude, target.longitude
		state.waypointIndex = (state.waypointIndex + state.routeDirection + len(route)) % len(route)
		target = route[state.waypointIndex]
		remaining = distance(lat, lon, target.latitude, target.longitude)
	}
	heading := bearing(lat, lon, target.latitude, target.longitude)
	if remaining <= stepDistance {
		return target.latitude, target.longitude, heading
	}
	lat, lon = move(lat, lon, heading, speed, seconds)
	return lat, lon, heading
}
