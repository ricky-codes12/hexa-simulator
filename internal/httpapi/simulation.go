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

type SimulationState struct {
	DeviceID        int64     `json:"device_id"`
	Running         bool      `json:"running"`
	Paused          bool      `json:"paused"`
	Mode            string    `json:"mode"`
	Speed           float64   `json:"speed"`
	Heading         float64   `json:"heading"`
	TargetLatitude  *float64  `json:"target_latitude,omitempty"`
	TargetLongitude *float64  `json:"target_longitude,omitempty"`
	Preset          string    `json:"preset,omitempty"`
	LastTick        time.Time `json:"last_tick,omitempty"`
	LastError       string    `json:"last_error,omitempty"`
}

type SimulationRuntime struct {
	ctx        context.Context
	store      DeviceStore
	forwarders []TelemetryForwarder
	interval   time.Duration
	mu         sync.Mutex
	states     map[int64]*SimulationState
	cancels    map[int64]context.CancelFunc
}

func NewSimulationRuntime(ctx context.Context, store DeviceStore, interval time.Duration, forwarders ...TelemetryForwarder) *SimulationRuntime {
	if interval <= 0 {
		interval = 3 * time.Second
	}
	return &SimulationRuntime{ctx: ctx, store: store, forwarders: forwarders, interval: interval, states: map[int64]*SimulationState{}, cancels: map[int64]context.CancelFunc{}}
}

func (r *SimulationRuntime) State(id int64) SimulationState {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s := r.states[id]; s != nil {
		return *s
	}
	return SimulationState{DeviceID: id, Mode: "auto", Speed: 40}
}

func (r *SimulationRuntime) Apply(id int64, in SimulationControl) (SimulationState, error) {
	items, err := r.store.ListDevices(r.ctx)
	if err != nil {
		return SimulationState{}, err
	}
	var d *Device
	for i := range items {
		if items[i].ID == id {
			x := items[i]
			d = &x
			break
		}
	}
	if d == nil {
		return SimulationState{}, fmt.Errorf("device not found")
	}
	r.mu.Lock()
	s := r.states[id]
	if s == nil {
		s = &SimulationState{DeviceID: id, Mode: "auto", Speed: 40, Heading: d.Heading}
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

func (r *SimulationRuntime) tick(id int64) {
	r.mu.Lock()
	s := r.states[id]
	if s == nil || !s.Running || s.Paused {
		r.mu.Unlock()
		return
	}
	state := *s
	r.mu.Unlock()
	items, err := r.store.ListDevices(r.ctx)
	if err != nil {
		r.setError(id, err)
		return
	}
	var d *Device
	for i := range items {
		if items[i].ID == id {
			x := items[i]
			d = &x
			break
		}
	}
	if d == nil {
		return
	}
	lat, lon, heading := d.Latitude, d.Longitude, state.Heading
	if lat == 0 && lon == 0 {
		lat = -2.985
		lon = 104.785
	}
	if state.Mode == "auto" {
		heading = autoHeading(id, d.UpdatedAt)
		state.Heading = heading
	}
	if state.Mode == "target" && state.TargetLatitude != nil && state.TargetLongitude != nil {
		heading = bearing(lat, lon, *state.TargetLatitude, *state.TargetLongitude)
		state.Heading = heading
	}
	lat, lon = move(lat, lon, heading, state.Speed, r.interval.Seconds())
	if state.Mode == "target" && state.TargetLatitude != nil && state.TargetLongitude != nil && distance(lat, lon, *state.TargetLatitude, *state.TargetLongitude) < 8 {
		lat = *state.TargetLatitude
		lon = *state.TargetLongitude
		state.Speed = 0
	}
	updated, err := r.store.UpdateTelemetry(r.ctx, id, TelemetryInput{Status: "online", Latitude: lat, Longitude: lon, Speed: state.Speed, Heading: heading, Ignition: true})
	if err != nil {
		r.setError(id, err)
		return
	}
	var last error
	for _, f := range r.forwarders {
		if err := f.ForwardTelemetry(r.ctx, updated); err != nil {
			last = err
		}
	}
	r.mu.Lock()
	if current := r.states[id]; current != nil {
		current.Heading = heading
		current.Speed = state.Speed
		current.LastTick = updated.UpdatedAt
		if last != nil {
			current.LastError = last.Error()
		} else {
			current.LastError = ""
		}
	}
	r.mu.Unlock()
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
func autoHeading(id int64, t time.Time) float64 { return math.Mod(float64((id*47)+t.Unix()/18), 360) }
