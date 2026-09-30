package httpapi

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"hexa-simulator/internal/fleet"
	"hexa-simulator/internal/scenario"
	"hexa-simulator/internal/telemetry"
	"hexa-simulator/internal/world"
)

// The fleet runtime owns the simulation clock (independent of any browser): one loop steps every
// unit each second along its itinerary, applies its behaviours, and when a unit's report is due
// builds one telemetry-v1 record and hands it to that unit's output only (plan S1). Outputs run
// behind bounded queues, so a slow broker or listener never stalls the clock.

// RuntimeOptions configures the fleet runtime.
type RuntimeOptions struct {
	// Interval is how often a unit reports; 5 s by default.
	Interval time.Duration
	// Step is how often units move; 1 s by default.
	Step    time.Duration
	Seed    int64
	Outputs []Output
	Now     func() time.Time
}

// SimulationControl drives one device by hand (the device panel).
type SimulationControl struct {
	Action          string   `json:"action,omitempty"`
	Mode            string   `json:"mode,omitempty"`
	Speed           float64  `json:"speed,omitempty"`
	Heading         float64  `json:"heading,omitempty"`
	TargetLatitude  *float64 `json:"target_latitude,omitempty"`
	TargetLongitude *float64 `json:"target_longitude,omitempty"`
	Preset          string   `json:"preset,omitempty"`
}

// OutputState is how a device's records fare on one output.
type OutputState struct {
	Configured bool      `json:"configured"`
	Status     string    `json:"status"` // ready | sending | rejected | error | silent
	LastOK     time.Time `json:"last_ok,omitempty"`
	LastError  string    `json:"last_error,omitempty"`
}

// BehaviourView is a behaviour as the console shows it.
type BehaviourView struct {
	Behaviour
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

// SimulationState is one device's runtime state.
type SimulationState struct {
	DeviceID        int64                  `json:"device_id"`
	Running         bool                   `json:"running"`
	Paused          bool                   `json:"paused"`
	Mode            string                 `json:"mode"`
	Speed           float64                `json:"speed"`
	Heading         float64                `json:"heading"`
	TargetLatitude  *float64               `json:"target_latitude,omitempty"`
	TargetLongitude *float64               `json:"target_longitude,omitempty"`
	Preset          string                 `json:"preset,omitempty"`
	LastTick        time.Time              `json:"last_tick,omitempty"`
	LastError       string                 `json:"last_error,omitempty"`
	Outputs         map[string]OutputState `json:"outputs,omitempty"`
	Output          string                 `json:"output"`
	State           string                 `json:"state"`
	Activity        string                 `json:"activity"`
	Behaviours      []BehaviourView        `json:"behaviours"`
	Attributes      map[string]any         `json:"attributes,omitempty"`
}

// FleetUnit is one unit on the fleet map.
type FleetUnit struct {
	ID         int64    `json:"id"`
	Name       string   `json:"name"`
	Kind       string   `json:"kind"`
	Output     string   `json:"output"`
	Estate     string   `json:"estate"`
	Lat        float64  `json:"lat"`
	Lon        float64  `json:"lon"`
	Heading    float64  `json:"heading"`
	Speed      float64  `json:"speed"`
	State      string   `json:"state"`
	Delivery   string   `json:"delivery"`
	Behaviours []string `json:"behaviours,omitempty"`
}

// OutputSummary is one output across the fleet.
type OutputSummary struct {
	Name       string     `json:"name"`
	Configured bool       `json:"configured"`
	Devices    int        `json:"devices"`
	Sent       int64      `json:"sent"`
	Failed     int64      `json:"failed"`
	Rejected   int64      `json:"rejected"`
	LastOK     *time.Time `json:"last_ok,omitempty"`
	LastError  string     `json:"last_error,omitempty"`
	Queue      int        `json:"queue"`
}

// ScenarioEventView is one timeline event.
type ScenarioEventView struct {
	AtS   float64 `json:"at_s"`
	Type  string  `json:"type,omitempty"`
	Note  string  `json:"note"`
	Fired bool    `json:"fired"`
}

// ScenarioView is the running timeline, for the console's countdown.
type ScenarioView struct {
	Name      string              `json:"name"`
	Title     string              `json:"title"`
	StartedAt time.Time           `json:"started_at"`
	ElapsedS  float64             `json:"elapsed_s"`
	LengthS   float64             `json:"length_s"`
	Finished  bool                `json:"finished"`
	Events    []ScenarioEventView `json:"events"`
}

// FleetView is the whole fleet: counters, outputs, the timeline and every unit's position.
type FleetView struct {
	Epoch     time.Time       `json:"epoch"`
	Now       time.Time       `json:"now"`
	IntervalS float64         `json:"interval_s"`
	Counts    map[string]int  `json:"counts"`
	Kinds     map[string]int  `json:"kinds"`
	Outputs   []OutputSummary `json:"outputs"`
	Scenario  *ScenarioView   `json:"scenario,omitempty"`
	Units     []FleetUnit     `json:"units"`
}

type leg struct {
	path     []world.LonLat
	length   float64
	kmh      float64
	wait     float64
	ignition bool
	label    string
}

func newLeg(path []world.LonLat, kmh, wait float64, ignition bool, label string) leg {
	return leg{path: path, length: world.Length(path), kmh: kmh, wait: wait, ignition: ignition, label: label}
}

// detour takes a unit off its itinerary: out of the estate and back, to a compartment, or back to
// its route after being driven by hand. Its itinerary clock stands still meanwhile.
type detour struct {
	legs   []leg
	i      int
	dist   float64
	waited float64
	done   func(*unit)
}

type unit struct {
	dev    Device
	kind   *fleet.Kind
	number int
	plan   *fleet.Itinerary
	odo0   float64

	running, paused bool
	mode            string
	manualKmh       float64
	manualHeading   float64
	target          *world.LonLat
	preset          string

	clock    float64
	pos      world.LonLat
	heading  float64
	kmh      float64
	ignition bool
	working  bool
	activity string

	odometer float64
	battery  float64
	extV     float64
	powerCut bool

	detour     *detour
	work       *fleet.Itinerary
	workClock  float64
	workCode   string
	workOwner  int64
	exits      map[int64]bool
	behaviours []Behaviour

	nextReport time.Time
	reports    int64
	held       []telemetry.Record
	heldSince  time.Time
	lastIgn    bool
	lastCut    bool
	lastAttrs  map[string]any
	lastTick   time.Time
	lastError  string
	outputs    map[string]OutputState
}

type dispatcher struct {
	out    Output
	queue  chan Delivery
	policy outputPolicy
}

type outputStats struct {
	sent, failed, rejected int64
	lastOK                 time.Time
	lastError              string
}

type scenarioRun struct {
	sc      scenario.Scenario
	started time.Time
	fired   []bool
}

// SimulationRuntime is the fleet runtime.
type SimulationRuntime struct {
	ctx         context.Context
	store       FleetStore
	opts        RuntimeOptions
	dispatchers map[string]*dispatcher

	mu       sync.Mutex
	units    map[int64]*unit
	order    []int64
	stats    map[string]*outputStats
	epoch    time.Time
	run      *scenarioRun
	lastStep time.Time
	looping  bool
}

const outputQueueSize = 4000

// NewSimulationRuntime creates the runtime; StartFleet loads the fleet and starts the clock.
func NewSimulationRuntime(ctx context.Context, store FleetStore, opts RuntimeOptions) *SimulationRuntime {
	if opts.Interval <= 0 {
		opts.Interval = 5 * time.Second
	}
	if opts.Step <= 0 {
		opts.Step = time.Second
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	r := &SimulationRuntime{ctx: ctx, store: store, opts: opts, dispatchers: map[string]*dispatcher{}, units: map[int64]*unit{}, stats: map[string]*outputStats{}}
	for _, o := range opts.Outputs {
		d := &dispatcher{out: o, queue: make(chan Delivery, outputQueueSize), policy: policyFor(o.Name())}
		r.dispatchers[o.Name()] = d
		r.stats[o.Name()] = &outputStats{}
		for i := 0; i < d.policy.workers; i++ {
			go r.worker(d)
		}
	}
	return r
}

// Configured reports whether an output is configured.
func (r *SimulationRuntime) Configured(name string) bool {
	_, ok := r.dispatchers[name]
	return ok
}

// ---- fleet start ---------------------------------------------------------------------------

// StartFleet loads every device, restores each unit's clock, odometer and behaviours, marks the
// fleet online and starts the clock. First reports are spread over one interval so 350 units do
// not all send at once.
func (r *SimulationRuntime) StartFleet() (int, error) {
	items, err := r.store.ListDevices(r.ctx)
	if err != nil {
		return 0, err
	}
	state, err := r.store.FleetState(r.ctx)
	if err != nil {
		return 0, err
	}
	now := r.opts.Now()
	active, err := r.store.ActiveBehaviours(r.ctx, now)
	if err != nil {
		return 0, err
	}
	r.mu.Lock()
	r.epoch = state.Epoch
	if sc, ok := scenario.ByName(state.Scenario); ok && state.ScenarioStartedAt != nil {
		r.run = &scenarioRun{sc: sc, started: *state.ScenarioStartedAt, fired: make([]bool, len(sc.Events))}
		for i, ev := range sc.Events {
			r.run.fired[i] = !now.Before(state.ScenarioStartedAt.Add(ev.At))
		}
	}
	started := 0
	var updates []TelemetryUpdate
	for i, d := range items {
		if _, exists := r.units[d.ID]; exists {
			continue
		}
		u := r.newUnit(d, now)
		u.running = true
		u.nextReport = now.Add(time.Duration(int64(r.opts.Interval) * int64(i) / int64(len(items))))
		r.units[d.ID] = u
		started++
		updates = append(updates, r.snapshot(u, now, "online"))
	}
	for _, b := range active {
		if u := r.units[b.DeviceID]; u != nil {
			u.behaviours = append(u.behaviours, b)
		}
	}
	r.reorder()
	loop := !r.looping
	r.looping = true
	r.lastStep = now
	r.mu.Unlock()
	if err := r.store.PersistTelemetry(r.ctx, updates); err != nil {
		return started, err
	}
	if loop {
		go r.loop()
	}
	return started, nil
}

func (r *SimulationRuntime) loop() {
	t := time.NewTicker(r.opts.Step)
	defer t.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-t.C:
			r.step(r.opts.Now())
		}
	}
}

// planNumber is the itinerary number of a device: a seeded unit's roster number, read from its
// IMEI, or a number derived from an operator device's ID.
func planNumber(d Device) int {
	if d.Seeded && len(d.IMEI) == 15 && strings.HasPrefix(d.IMEI, fleet.IMEIPrefix) {
		if n, err := strconv.Atoi(d.IMEI[8:14]); err == nil && n > 0 {
			return n
		}
	}
	return 1000 + int(d.ID)
}

func (r *SimulationRuntime) newUnit(d Device, now time.Time) *unit {
	kind, ok := fleet.KindByKey(d.Kind)
	if !ok {
		kind = fleet.DefaultKind
	}
	n := planNumber(d)
	plan := fleet.Plan(kind, n, r.opts.Seed)
	u := &unit{dev: d, kind: kind, number: n, plan: plan, mode: "auto", manualKmh: 40, battery: 4.1, outputs: map[string]OutputState{}, exits: map[int64]bool{}}
	u.odo0 = baseOdometer(kind, n, r.opts.Seed)
	u.clock = plan.Phase
	if d.PlanClock != nil {
		u.clock = *d.PlanClock
		if !d.UpdatedAt.IsZero() && now.After(d.UpdatedAt) {
			u.clock += now.Sub(d.UpdatedAt).Seconds()
		}
	}
	u.odometer = u.odo0
	if v, ok := d.Attributes[telemetry.Odometer].(float64); ok && v > 0 {
		u.odometer = v
	}
	st := plan.At(u.clock)
	u.pos, u.heading, u.kmh, u.ignition, u.working, u.activity = st.Pos, st.Heading, st.Kmh, st.Ignition, st.Working, st.Label
	u.extV = restingVolts(kind, u.ignition)
	u.lastIgn = u.ignition
	for _, name := range fleet.Transports {
		if r.Configured(name) && routes(d.Output, name) {
			u.outputs[name] = OutputState{Configured: true, Status: "ready"}
		}
	}
	return u
}

// baseOdometer is a unit's odometer at T0 in metres: road vehicles 20,000 to 150,000 km, field
// machines 2,000 to 20,000 km.
func baseOdometer(k *fleet.Kind, n int, seed int64) float64 {
	f := fleet.Fraction(seed, int64(k.Digit), int64(n), 11)
	switch k.Key {
	case "farm-tractor", "dozer", "excavator":
		return math.Round(2e6 + f*18e6)
	}
	return math.Round(2e7 + f*1.3e8)
}

func (r *SimulationRuntime) reorder() {
	r.order = r.order[:0]
	for id := range r.units {
		r.order = append(r.order, id)
	}
	sort.Slice(r.order, func(i, j int) bool { return r.order[i] < r.order[j] })
}

// routes reports whether a device with output setting o sends to transport name.
func routes(o, name string) bool { return o == name || o == fleet.OutputAll }

// ---- the clock ------------------------------------------------------------------------------

func (r *SimulationRuntime) step(now time.Time) {
	r.mu.Lock()
	dt := now.Sub(r.lastStep).Seconds()
	if dt <= 0 {
		r.mu.Unlock()
		return
	}
	if dt > 10 {
		dt = 10
	}
	r.lastStep = now
	var due []scenario.Event
	var source string
	if r.run != nil {
		source = "scenario:" + r.run.sc.Name
		for i, ev := range r.run.sc.Events {
			if !r.run.fired[i] && !now.Before(r.run.started.Add(ev.At)) {
				r.run.fired[i] = true
				if ev.Type != "" {
					due = append(due, ev)
				}
			}
		}
	}
	type send struct {
		output string
		d      Delivery
	}
	var sends []send
	var updates []TelemetryUpdate
	for _, id := range r.order {
		u := r.units[id]
		if !u.running || u.paused {
			continue
		}
		active := u.active(now)
		r.advance(u, active, now, dt)
		u.lastTick = now
		if now.Before(u.nextReport) {
			continue
		}
		interval := r.opts.Interval
		if b, ok := find(active, scenario.Park); ok {
			interval = time.Duration(scenario.Params(b.Params).Number("heartbeat_s", 300) * float64(time.Second))
		}
		u.nextReport = now.Add(interval)
		rec := r.record(u, now)
		updates = append(updates, r.snapshot(u, now, "online"))
		records := r.filter(u, active, rec, now)
		if len(records) == 0 {
			continue
		}
		d := Delivery{DeviceID: u.dev.ID, IMEI: u.dev.IMEI, Kind: u.kind.Key, Estate: u.dev.Estate, Records: records}
		for _, name := range fleet.Transports {
			if routes(u.dev.Output, name) {
				sends = append(sends, send{name, d})
			}
		}
	}
	r.mu.Unlock()

	for _, s := range sends {
		r.enqueue(s.output, s.d)
	}
	if len(updates) > 0 {
		_ = r.store.PersistTelemetry(r.ctx, updates)
	}
	for _, ev := range due {
		_, _ = r.give(ev.Target, ev.Type, ev.Params, now, ev.Duration, source)
	}
}

func find(bs []Behaviour, t string) (Behaviour, bool) {
	for _, b := range bs {
		if b.Type == t {
			return b, true
		}
	}
	return Behaviour{}, false
}

// active returns the unit's behaviours in force at now, and forgets expired ones.
func (u *unit) active(now time.Time) []Behaviour {
	var out []Behaviour
	kept := u.behaviours[:0]
	for _, b := range u.behaviours {
		if !now.Before(b.EndsAt) && !(b.Type == scenario.GeofenceExit && u.exits[b.ID] && u.detour != nil) {
			continue
		}
		kept = append(kept, b)
		if !now.Before(b.StartsAt) {
			out = append(out, b)
		}
	}
	u.behaviours = kept
	return out
}

// advance moves a unit by dt seconds: a stop behaviour holds it, a hand-driven unit follows its
// controls, a detour or compartment work runs on its own clock, and otherwise the itinerary
// clock moves on (faster during an overspeed).
func (r *SimulationRuntime) advance(u *unit, active []Behaviour, now time.Time, dt float64) {
	prev := u.pos
	r.transitions(u, active, now)
	_, park := find(active, scenario.Park)
	_, idle := find(active, scenario.Idle)
	over, overspeed := find(active, scenario.Overspeed)
	switch {
	case park:
		u.kmh, u.ignition, u.working, u.activity = 0, false, false, "Parked"
	case idle:
		u.kmh, u.ignition, u.working, u.activity = 0, true, false, "Idling with the engine on"
	case u.mode == "manual":
		u.heading, u.kmh, u.ignition, u.working = u.manualHeading, u.manualKmh, true, false
		u.pos = world.Offset(u.pos, u.kmh/3.6*dt, u.heading)
		u.activity = "Driven by hand"
	case u.mode == "target" && u.target != nil:
		left := world.Distance(u.pos, *u.target)
		move := u.manualKmh / 3.6 * dt
		u.ignition, u.working = true, false
		if left <= math.Max(3, move) {
			u.pos, u.kmh, u.activity = *u.target, 0, "At the target point"
		} else {
			u.heading = world.Bearing(u.pos, *u.target)
			u.pos, u.kmh, u.activity = world.Offset(u.pos, move, u.heading), u.manualKmh, "Driving to the target point"
		}
	case u.mode == "target":
		u.kmh, u.activity = 0, "Waiting for a target point"
	case u.detour != nil:
		if u.detour.advance(u, dt) {
			done := u.detour.done
			u.detour = nil
			if done != nil {
				done(u)
			}
		}
	case u.work != nil:
		u.workClock += dt
		st := u.work.At(u.workClock)
		u.pos, u.heading, u.kmh, u.ignition, u.working = st.Pos, st.Heading, st.Kmh, st.Ignition, st.Working
		u.activity = "Working " + u.workCode + ", " + strings.ToLower(st.Label)
	default:
		rate := 1.0
		if overspeed {
			if st := u.plan.At(u.clock); st.Cruise == 0 {
				if next, ok := u.plan.NextDrive(u.clock); ok {
					u.clock = next
				}
			}
			if st := u.plan.At(u.clock); st.Cruise > 0 {
				rate = scenario.Params(over.Params).Number("kmh", 82) / st.Cruise
			}
		}
		u.clock += dt * rate
		st := u.plan.At(u.clock)
		u.pos, u.heading, u.kmh, u.ignition, u.working, u.activity = st.Pos, st.Heading, st.Kmh*rate, st.Ignition, st.Working, st.Label
	}
	u.odometer += world.Distance(prev, u.pos)
	r.power(u, active, dt)
}

// transitions starts and ends the behaviours that take a unit off its itinerary.
func (r *SimulationRuntime) transitions(u *unit, active []Behaviour, now time.Time) {
	if u.mode != "auto" {
		return
	}
	for _, b := range active {
		if b.Type == scenario.GeofenceExit && !u.exits[b.ID] && u.detour == nil {
			u.exits[b.ID] = true
			p := scenario.Params(b.Params)
			out := exitPath(u.pos, p.Number("beyond_m", 250))
			back := world.Reverse(out)
			back[len(back)-1] = r.returnPoint(u)
			u.detour = &detour{legs: []leg{
				newLeg(out, 40, p.Number("wait_s", 90), true, "Leaving the operating area"),
				newLeg(back, 35, 0, true, "Driving back to its route"),
			}}
		}
	}
	work, working := find(active, scenario.Work)
	switch {
	case working && u.workOwner != work.ID && u.detour == nil:
		p := scenario.Params(work.Params)
		code := p.String("compartment", "SLG-C07")
		outline, ok := world.Compartment(code)
		if !ok {
			u.lastError = "work: no compartment or block " + code
			u.workOwner = work.ID
			return
		}
		it := fleet.CompartmentWork(outline, p.Number("kmh", 4.5))
		u.workOwner = work.ID
		u.detour = &detour{legs: []leg{newLeg(roadPath(u.pos, it.At(0).Pos), 12, 0, true, "To compartment "+code)}, done: func(u *unit) {
			u.work, u.workClock, u.workCode = it, 0, code
		}}
	case !working && u.workOwner != 0:
		u.workOwner, u.work, u.workCode = 0, nil, ""
		u.detour = &detour{legs: []leg{newLeg(roadPath(u.pos, r.returnPoint(u)), 12, 0, true, "Back to its route")}}
	}
}

// returnPoint is where a unit rejoins its day: its compartment lane when it works one, otherwise
// its itinerary where it left it.
func (r *SimulationRuntime) returnPoint(u *unit) world.LonLat {
	if u.work != nil {
		return u.work.At(u.workClock).Pos
	}
	return u.plan.At(u.clock).Pos
}

// exitPath leads from p out of the operating area, beyond metres past its nearest edge. A
// straight line that would cross the conservation zone goes out through the north gate instead.
func exitPath(p world.LonLat, beyond float64) []world.LonLat {
	out, _ := world.ExitPoint(p, world.OperatingArea, beyond)
	if !crosses(p, out, world.ConservationZone) {
		return []world.LonLat{p, out}
	}
	gate := world.GateRun()
	path := append([]world.LonLat{p}, world.Route(p, gate[0])...)
	return append(path, gate[1:]...)
}

func crosses(a, b world.LonLat, ring []world.LonLat) bool {
	for i := 0; i <= 40; i++ {
		f := float64(i) / 40
		if world.Inside(world.LonLat{a[0] + (b[0]-a[0])*f, a[1] + (b[1]-a[1])*f}, ring) {
			return true
		}
	}
	return false
}

// roadPath leads from a to b: straight when close, over the haul roads otherwise.
func roadPath(a, b world.LonLat) []world.LonLat {
	if world.Distance(a, b) < 800 && !crosses(a, b, world.ConservationZone) {
		return []world.LonLat{a, b}
	}
	path := []world.LonLat{a}
	for _, p := range world.Route(a, b) {
		if world.Distance(path[len(path)-1], p) > 1 {
			path = append(path, p)
		}
	}
	if world.Distance(path[len(path)-1], b) > 1 {
		path = append(path, b)
	}
	return path
}

func (d *detour) advance(u *unit, dt float64) bool {
	for dt > 1e-9 && d.i < len(d.legs) {
		l := &d.legs[d.i]
		if d.dist < l.length && l.kmh > 0 {
			v := l.kmh / 3.6
			if d.dist+v*dt >= l.length {
				dt -= (l.length - d.dist) / v
				d.dist = l.length
			} else {
				d.dist += v * dt
				dt = 0
			}
			u.pos, u.heading = world.Along(l.path, d.dist)
			u.kmh, u.ignition, u.working, u.activity = l.kmh, true, false, l.label
			continue
		}
		u.kmh, u.ignition, u.working, u.activity = 0, l.ignition, false, l.label
		if d.waited+dt < l.wait {
			d.waited += dt
			return false
		}
		dt -= l.wait - d.waited
		d.i, d.dist, d.waited = d.i+1, 0, 0
	}
	return d.i >= len(d.legs)
}

// power runs the electrical model: a 24 V or 12 V system charging while the engine runs and
// resting when off, and during a power cut 0 V outside with the tracker's own battery draining.
func (r *SimulationRuntime) power(u *unit, active []Behaviour, dt float64) {
	_, cut := find(active, scenario.PowerCut)
	u.powerCut = cut
	if cut {
		u.extV = 0
		u.battery = math.Max(3.55, u.battery-dt*0.00025)
		return
	}
	target := restingVolts(u.kind, u.ignition) + wobble(u.dev.ID, u.lastTick, 0.12)
	u.extV += (target - u.extV) * math.Min(1, dt/4)
	u.battery = math.Min(4.12, u.battery+dt*0.0002)
}

func restingVolts(k *fleet.Kind, ignition bool) float64 {
	switch {
	case k.Volts >= 24 && ignition:
		return 27.8
	case k.Volts >= 24:
		return 25.2
	case ignition:
		return 14.1
	}
	return 12.6
}

// wobble is a small smooth variation, the same for the same unit and minute.
func wobble(id int64, t time.Time, amp float64) float64 {
	return amp * math.Sin(float64(t.Unix())/37+float64(id)*1.7)
}

// ---- records --------------------------------------------------------------------------------

// record builds the unit's telemetry-v1 record with the attributes its profile reports.
func (r *SimulationRuntime) record(u *unit, now time.Time) telemetry.Record {
	ign := u.ignition && !u.powerCut
	moving := u.kmh > 0.5
	speed := 0.0
	if moving {
		speed = math.Max(0.5, u.kmh*(1+0.025*math.Sin(float64(now.Unix())/9+float64(u.dev.ID))))
	}
	attrs := map[string]any{}
	put := func(key string, v any) {
		if u.kind.Reports(key) {
			attrs[key] = v
		}
	}
	put(telemetry.Ignition, ign)
	put(telemetry.Movement, moving)
	put(telemetry.ExternalVoltage, telemetry.Round(u.extV, 2))
	put(telemetry.BatteryVoltage, telemetry.Round(u.battery, 2))
	put(telemetry.PowerCut, u.powerCut)
	put(telemetry.GNSSStatus, 1)
	put(telemetry.GSMSignal, gsm(u.pos, u.dev.ID, now))
	put(telemetry.Odometer, math.Round(u.odometer))
	put(telemetry.DIN1, u.working)
	rec := telemetry.Record{
		HardwareID: u.dev.IMEI, Time: now.UTC().Truncate(time.Millisecond), Attributes: attrs,
		Position: &telemetry.Position{Lat: u.pos.Lat(), Lon: u.pos.Lon(), AltitudeM: altitude(u.pos), SpeedKmh: telemetry.Round(speed, 1),
			HeadingDeg: u.heading, Satellites: 9 + int(fleet.Fraction(u.dev.ID, now.Unix()/30)*6), FixValid: true},
	}
	if ign != u.lastIgn {
		rec.EventIO = 239
	}
	if u.powerCut && !u.lastCut {
		rec.EventIO, rec.Priority = 66, 1
	}
	u.lastIgn, u.lastCut = ign, u.powerCut
	u.lastAttrs = attrs
	u.reports++
	return rec
}

// gsm is the signal level: 4 across the estates, weaker in the river valley and on the
// concession's far edge, varying by a level from minute to minute.
func gsm(p world.LonLat, id int64, t time.Time) int {
	level := 4
	if (p.Lat() < -2.045 && p.Lon() < 104.06) || p.Lon() > 104.195 {
		level = 3
	}
	switch f := fleet.Fraction(id, t.Unix()/60); {
	case f < 0.2:
		level++
	case f > 0.85:
		level--
	}
	return max(1, min(5, level))
}

// altitude is a gentle synthetic terrain, 10 to 40 m, as on plantation land in Musi Banyuasin.
func altitude(p world.LonLat) float64 {
	return math.Round(24 + 9*math.Sin(p.Lon()*420) + 6*math.Cos(p.Lat()*380))
}

// filter applies the sending behaviours to a due record: a silent unit sends nothing, an
// intermittent one drops a share of records or holds them for a burst.
func (r *SimulationRuntime) filter(u *unit, active []Behaviour, rec telemetry.Record, now time.Time) []telemetry.Record {
	if _, silent := find(active, scenario.SignalLoss); silent {
		for name, st := range u.outputs {
			st.Status = "silent"
			u.outputs[name] = st
		}
		return nil
	}
	if b, ok := find(active, scenario.Intermittent); ok {
		p := scenario.Params(b.Params)
		if burst := p.Number("burst_s", 0); burst > 0 {
			if len(u.held) == 0 {
				u.heldSince = now
			}
			u.held = append(u.held, rec)
			if now.Sub(u.heldSince).Seconds() < burst {
				return nil
			}
			out := u.held
			u.held = nil
			return out
		}
		if fleet.Fraction(u.dev.ID, u.reports, 7) < p.Number("drop_pct", 60)/100 {
			return nil
		}
	}
	if len(u.held) > 0 {
		out := append(u.held, rec)
		u.held = nil
		return out
	}
	return []telemetry.Record{rec}
}

func (r *SimulationRuntime) snapshot(u *unit, now time.Time, status string) TelemetryUpdate {
	clock := u.clock
	attrs := u.lastAttrs
	if attrs == nil {
		attrs = map[string]any{telemetry.Odometer: math.Round(u.odometer)}
	}
	return TelemetryUpdate{ID: u.dev.ID, Status: status, Latitude: u.pos.Lat(), Longitude: u.pos.Lon(), Speed: telemetry.Round(u.kmh, 1),
		Heading: math.Mod(math.Round(u.heading), 360), Ignition: u.ignition && !u.powerCut, Attributes: attrs, PlanClock: &clock, At: now}
}

// ---- outputs --------------------------------------------------------------------------------

func (r *SimulationRuntime) enqueue(name string, d Delivery) {
	disp := r.dispatchers[name]
	if disp == nil {
		return
	}
	select {
	case disp.queue <- d:
	default:
		r.result(name, d, fmt.Errorf("output queue full"))
	}
}

func (r *SimulationRuntime) worker(d *dispatcher) {
	for {
		var batch []Delivery
		select {
		case <-r.ctx.Done():
			return
		case first := <-d.queue:
			batch = append(batch, first)
		}
	fill:
		for len(batch) < d.policy.batch {
			select {
			case next := <-d.queue:
				batch = append(batch, next)
			default:
				break fill
			}
		}
		errs := d.out.Deliver(r.ctx, batch)
		for i, del := range batch {
			var err error
			if i < len(errs) {
				err = errs[i]
			}
			r.result(d.out.Name(), del, err)
		}
	}
}

func (r *SimulationRuntime) result(name string, d Delivery, err error) {
	now := r.opts.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	st := r.stats[name]
	u := r.units[d.DeviceID]
	n := int64(len(d.Records))
	if err == nil {
		if st != nil {
			st.sent += n
			st.lastOK = now
		}
		if u != nil {
			u.outputs[name] = OutputState{Configured: true, Status: "sending", LastOK: d.Records[len(d.Records)-1].Time}
			u.lastError = ""
		}
		return
	}
	status, text := describe(err)
	if st != nil {
		if status == "rejected" {
			st.rejected += n
		} else {
			st.failed += n
			st.lastError = text
		}
	}
	if u != nil {
		prev := u.outputs[name]
		u.outputs[name] = OutputState{Configured: true, Status: status, LastOK: prev.LastOK, LastError: text}
		u.lastError = name + ": " + text
	}
}

// Inject sends a hand-made sample right away to the device's output and waits for the answer,
// so a broken integration shows as an error (HTTP 502) instead of a silent drop.
func (r *SimulationRuntime) Inject(ctx context.Context, d Device) error {
	kind, ok := fleet.KindByKey(d.Kind)
	if !ok {
		kind = fleet.DefaultKind
	}
	rec := telemetry.Record{HardwareID: d.IMEI, Time: d.UpdatedAt.UTC().Truncate(time.Millisecond),
		Position:   &telemetry.Position{Lat: d.Latitude, Lon: d.Longitude, SpeedKmh: d.Speed, HeadingDeg: d.Heading, Satellites: 10, FixValid: true},
		Attributes: map[string]any{telemetry.Ignition: d.Ignition, telemetry.Movement: d.Speed > 0}}
	if rec.Time.IsZero() {
		rec.Time = r.opts.Now().UTC().Truncate(time.Millisecond)
	}
	del := Delivery{DeviceID: d.ID, IMEI: d.IMEI, Kind: kind.Key, Estate: d.Estate, Records: []telemetry.Record{rec}}
	for _, name := range fleet.Transports {
		disp := r.dispatchers[name]
		if disp == nil || !routes(d.Output, name) {
			continue
		}
		errs := disp.out.Deliver(ctx, []Delivery{del})
		var err error
		if len(errs) > 0 {
			err = errs[0]
		}
		r.result(name, del, err)
		if err != nil {
			return err
		}
	}
	return nil
}

// ---- control --------------------------------------------------------------------------------

func (r *SimulationRuntime) unitState(u *unit, now time.Time) string {
	switch {
	case !u.running || u.paused:
		return "stopped"
	}
	for _, b := range u.behaviours {
		if b.Type == scenario.SignalLoss && !now.Before(b.StartsAt) && now.Before(b.EndsAt) {
			return "silent"
		}
	}
	switch {
	case u.kmh > 0.5:
		return "moving"
	case u.ignition && !u.powerCut:
		return "idle"
	}
	return "parked"
}

// State returns one device's runtime state.
func (r *SimulationRuntime) State(id int64) SimulationState {
	now := r.opts.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	u := r.units[id]
	if u == nil {
		return SimulationState{DeviceID: id, Mode: "auto", State: "stopped", Outputs: map[string]OutputState{}, Behaviours: []BehaviourView{}}
	}
	return r.stateOf(u, now)
}

func (r *SimulationRuntime) stateOf(u *unit, now time.Time) SimulationState {
	s := SimulationState{DeviceID: u.dev.ID, Running: u.running, Paused: u.paused, Mode: u.mode, Speed: telemetry.Round(u.kmh, 1), Heading: math.Round(u.heading),
		Preset: u.preset, LastTick: u.lastTick, LastError: u.lastError, Outputs: map[string]OutputState{}, Output: u.dev.Output,
		State: r.unitState(u, now), Activity: u.activity, Behaviours: []BehaviourView{}, Attributes: u.lastAttrs}
	if u.mode != "auto" {
		s.Speed = u.manualKmh
	}
	if u.target != nil {
		lat, lon := u.target.Lat(), u.target.Lon()
		s.TargetLatitude, s.TargetLongitude = &lat, &lon
	}
	for k, v := range u.outputs {
		s.Outputs[k] = v
	}
	for _, b := range u.behaviours {
		spec, _ := scenario.SpecFor(b.Type)
		s.Behaviours = append(s.Behaviours, BehaviourView{Behaviour: b, Name: spec.Name, Active: !now.Before(b.StartsAt) && now.Before(b.EndsAt)})
	}
	return s
}

// Apply starts, pauses, stops or steers one device by hand.
func (r *SimulationRuntime) Apply(id int64, in SimulationControl) (SimulationState, error) {
	now := r.opts.Now()
	r.mu.Lock()
	u := r.units[id]
	if u == nil {
		r.mu.Unlock()
		d, err := r.store.GetDevice(r.ctx, id)
		if err != nil {
			return SimulationState{}, fmt.Errorf("device not found")
		}
		r.AddDevice(d)
		r.mu.Lock()
		u = r.units[id]
	}
	defer r.mu.Unlock()
	if in.Mode != "" && in.Mode != "auto" && in.Mode != "manual" && in.Mode != "target" {
		return SimulationState{}, fmt.Errorf("invalid simulation mode")
	}
	switch in.Action {
	case "start", "resume", "pause", "stop", "control", "":
	default:
		return SimulationState{}, fmt.Errorf("invalid simulation action")
	}
	if in.Mode != "" && in.Mode != u.mode {
		if in.Mode == "auto" && u.mode != "auto" {
			u.detour = &detour{legs: []leg{newLeg(roadPath(u.pos, r.returnPoint(u)), 30, 0, true, "Back to its route")}}
		}
		if in.Mode != "auto" {
			u.detour = nil
			u.manualHeading = u.heading
		}
		u.mode = in.Mode
	}
	if in.Speed > 0 && in.Speed <= 180 {
		u.manualKmh = in.Speed
	} else if in.Speed == 0 && in.Action == "control" && u.mode != "auto" {
		u.manualKmh = 0
	}
	if in.Heading >= 0 && in.Heading < 360 && u.mode == "manual" && in.Action == "control" {
		u.manualHeading = in.Heading
	}
	if in.TargetLatitude != nil && in.TargetLongitude != nil {
		t := world.LonLat{*in.TargetLongitude, *in.TargetLatitude}
		u.target = &t
	} else if u.mode != "target" {
		u.target = nil
	}
	u.preset = in.Preset
	var update *TelemetryUpdate
	switch in.Action {
	case "start", "resume":
		u.running, u.paused = true, false
		u.nextReport = now
	case "pause":
		u.paused = true
	case "stop":
		u.running, u.paused = false, false
		u.kmh = 0
		up := r.snapshot(u, now, "offline")
		up.Ignition, up.Speed = false, 0
		update = &up
	}
	out := r.stateOf(u, now)
	if update != nil {
		go func() { _ = r.store.PersistTelemetry(r.ctx, []TelemetryUpdate{*update}) }()
	}
	return out, nil
}

// AddDevice takes a new device into the runtime, stopped until it is started.
func (r *SimulationRuntime) AddDevice(d Device) {
	now := r.opts.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.units[d.ID]; ok {
		return
	}
	u := r.newUnit(d, now)
	u.nextReport = now
	r.units[d.ID] = u
	r.reorder()
}

// RefreshDevice takes a changed device (name, output, kind or estate) into the runtime.
func (r *SimulationRuntime) RefreshDevice(d Device) {
	now := r.opts.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	u := r.units[d.ID]
	if u == nil {
		return
	}
	if d.Kind != u.dev.Kind {
		fresh := r.newUnit(d, now)
		fresh.running, fresh.paused, fresh.nextReport, fresh.behaviours = u.running, u.paused, u.nextReport, u.behaviours
		r.units[d.ID] = fresh
		return
	}
	d.PlanClock = u.dev.PlanClock
	u.dev = d
	for name := range u.outputs {
		if !routes(d.Output, name) {
			delete(u.outputs, name)
		}
	}
	for _, name := range fleet.Transports {
		if _, seen := u.outputs[name]; !seen && r.Configured(name) && routes(d.Output, name) {
			u.outputs[name] = OutputState{Configured: true, Status: "ready"}
		}
	}
}

// RemoveDevice forgets a deleted device.
func (r *SimulationRuntime) RemoveDevice(id int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.units, id)
	r.reorder()
}

func (r *SimulationRuntime) candidates() []scenario.Candidate {
	out := make([]scenario.Candidate, 0, len(r.units))
	for _, u := range r.units {
		out = append(out, scenario.Candidate{ID: u.dev.ID, Name: u.dev.Name, Kind: u.kind.Key, Estate: u.dev.Estate, Output: u.dev.Output})
	}
	return out
}

// Select resolves a target to device IDs.
func (r *SimulationRuntime) Select(t scenario.Target) []int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return t.Select(r.candidates())
}

// Give gives a behaviour to the target's units now or after a delay, for a duration (0 uses the
// behaviour's default). It returns the behaviours created.
func (r *SimulationRuntime) Give(t scenario.Target, kind string, params scenario.Params, delay, duration time.Duration) ([]Behaviour, error) {
	return r.give(t, kind, params, r.opts.Now().Add(delay), duration, "manual")
}

func (r *SimulationRuntime) give(t scenario.Target, kind string, params scenario.Params, start time.Time, duration time.Duration, source string) ([]Behaviour, error) {
	spec, ok := scenario.SpecFor(kind)
	if !ok {
		return nil, fmt.Errorf("unknown behaviour %q", kind)
	}
	p, err := scenario.Normalize(kind, params)
	if err != nil {
		return nil, err
	}
	if kind == scenario.Work {
		if _, ok := world.Compartment(p.String("compartment", "")); !ok {
			return nil, fmt.Errorf("work: no compartment or block %q", p.String("compartment", ""))
		}
	}
	if duration <= 0 {
		duration = time.Duration(spec.Duration) * time.Second
	}
	r.mu.Lock()
	ids := t.Select(r.candidates())
	var bs []Behaviour
	for _, id := range ids {
		d := duration
		if kind == scenario.GeofenceExit && d <= 0 {
			// Out and back at about 38 km/h plus the wait, with a margin.
			exit, _ := world.ExitPoint(r.units[id].pos, world.OperatingArea, p.Number("beyond_m", 250))
			secs := 2*world.Distance(r.units[id].pos, exit)/(38/3.6) + p.Number("wait_s", 90) + 120
			d = time.Duration(secs * float64(time.Second))
		}
		bs = append(bs, Behaviour{DeviceID: id, Type: kind, Params: p, StartsAt: start, EndsAt: start.Add(d), Source: source})
	}
	r.mu.Unlock()
	if len(bs) == 0 {
		return nil, nil
	}
	created, err := r.store.CreateBehaviours(r.ctx, bs)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	for _, b := range created {
		if u := r.units[b.DeviceID]; u != nil {
			u.behaviours = append(u.behaviours, b)
		}
	}
	r.mu.Unlock()
	return created, nil
}

// ClearBehaviours ends every behaviour of the target's units at once.
func (r *SimulationRuntime) ClearBehaviours(t scenario.Target) (int, error) {
	r.mu.Lock()
	ids := t.Select(r.candidates())
	filter := BehaviourFilter{DeviceIDs: ids}
	if t.Empty() {
		filter = BehaviourFilter{All: true}
	}
	for _, id := range ids {
		if u := r.units[id]; u != nil {
			u.behaviours = nil
		}
	}
	r.mu.Unlock()
	return r.store.DeleteBehaviours(r.ctx, filter)
}

// SetOutputs routes the target's units to an output.
func (r *SimulationRuntime) SetOutputs(t scenario.Target, output string) (int, error) {
	if !fleet.ValidOutput(output) {
		return 0, fmt.Errorf("output must be mqtt, teltonika, http-push, all or none")
	}
	ids := r.Select(t)
	n, err := r.store.SetOutput(r.ctx, ids, output)
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		if d, err := r.store.GetDevice(r.ctx, id); err == nil {
			r.RefreshDevice(d)
		}
	}
	return n, nil
}

// Run starts or stops the target's units.
func (r *SimulationRuntime) Run(t scenario.Target, action string) (int, error) {
	if action != "start" && action != "stop" {
		return 0, fmt.Errorf("action must be start or stop")
	}
	n := 0
	for _, id := range r.Select(t) {
		if _, err := r.Apply(id, SimulationControl{Action: action}); err == nil {
			n++
		}
	}
	return n, nil
}

// Reset puts the whole fleet back to T0 (plan S6): every unit at its seeded phase, odometer and
// battery, driving its itinerary, no behaviours, no timeline.
func (r *SimulationRuntime) Reset() error {
	now := r.opts.Now()
	if err := r.store.ResetFleet(r.ctx, now); err != nil {
		return err
	}
	r.mu.Lock()
	r.run = nil
	r.epoch = now
	var updates []TelemetryUpdate
	i := 0
	for _, id := range r.order {
		u := r.units[id]
		fresh := r.newUnit(u.dev, now)
		fresh.clock = fresh.plan.Phase
		fresh.odometer = fresh.odo0
		st := fresh.plan.At(fresh.clock)
		fresh.pos, fresh.heading, fresh.kmh, fresh.ignition, fresh.working, fresh.activity = st.Pos, st.Heading, st.Kmh, st.Ignition, st.Working, st.Label
		fresh.running = true
		fresh.nextReport = now.Add(time.Duration(int64(r.opts.Interval) * int64(i) / int64(max(1, len(r.order)))))
		r.units[id] = fresh
		updates = append(updates, r.snapshot(fresh, now, "online"))
		i++
	}
	r.mu.Unlock()
	return r.store.PersistTelemetry(r.ctx, updates)
}

// StartScenario runs a timeline from now. A timeline already running is stopped first.
func (r *SimulationRuntime) StartScenario(name string) error {
	sc, ok := scenario.ByName(name)
	if !ok {
		return fmt.Errorf("unknown scenario %q", name)
	}
	if err := r.StopScenario(); err != nil {
		return err
	}
	now := r.opts.Now()
	r.mu.Lock()
	r.run = &scenarioRun{sc: sc, started: now, fired: make([]bool, len(sc.Events))}
	epoch := r.epoch
	r.mu.Unlock()
	return r.store.SaveFleetState(r.ctx, FleetState{Epoch: epoch, Scenario: sc.Name, ScenarioStartedAt: &now})
}

// StopScenario stops the timeline and ends the behaviours it gave.
func (r *SimulationRuntime) StopScenario() error {
	r.mu.Lock()
	run := r.run
	r.run = nil
	epoch := r.epoch
	if run != nil {
		source := "scenario:" + run.sc.Name
		for _, u := range r.units {
			kept := u.behaviours[:0]
			for _, b := range u.behaviours {
				if b.Source != source {
					kept = append(kept, b)
				}
			}
			u.behaviours = kept
		}
	}
	r.mu.Unlock()
	if run != nil {
		if _, err := r.store.DeleteBehaviours(r.ctx, BehaviourFilter{Source: "scenario:" + run.sc.Name}); err != nil {
			return err
		}
	}
	return r.store.SaveFleetState(r.ctx, FleetState{Epoch: epoch})
}

// Fleet returns the fleet view.
func (r *SimulationRuntime) Fleet() FleetView {
	now := r.opts.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	v := FleetView{Epoch: r.epoch, Now: now, IntervalS: r.opts.Interval.Seconds(), Counts: map[string]int{}, Kinds: map[string]int{}, Units: make([]FleetUnit, 0, len(r.order))}
	devices := map[string]int{}
	for _, id := range r.order {
		u := r.units[id]
		state := r.unitState(u, now)
		v.Counts[state]++
		v.Counts["total"]++
		v.Kinds[u.kind.Key]++
		delivery := "off"
		for _, name := range fleet.Transports {
			if routes(u.dev.Output, name) {
				devices[name]++
				if st, ok := u.outputs[name]; ok && delivery != "error" && delivery != "rejected" {
					delivery = st.Status
				}
			}
		}
		fu := FleetUnit{ID: id, Name: u.dev.Name, Kind: u.kind.Key, Output: u.dev.Output, Estate: u.dev.Estate, Lat: telemetry.Round(u.pos.Lat(), 6), Lon: telemetry.Round(u.pos.Lon(), 6),
			Heading: math.Round(u.heading), Speed: telemetry.Round(u.kmh, 1), State: state, Delivery: delivery}
		for _, b := range u.behaviours {
			if !now.Before(b.StartsAt) && now.Before(b.EndsAt) {
				fu.Behaviours = append(fu.Behaviours, b.Type)
			}
		}
		v.Units = append(v.Units, fu)
	}
	for _, name := range fleet.Transports {
		s := OutputSummary{Name: name, Configured: r.Configured(name), Devices: devices[name]}
		if st := r.stats[name]; st != nil {
			s.Sent, s.Failed, s.Rejected, s.LastError = st.sent, st.failed, st.rejected, st.lastError
			if !st.lastOK.IsZero() {
				t := st.lastOK
				s.LastOK = &t
			}
		}
		if d := r.dispatchers[name]; d != nil {
			s.Queue = len(d.queue)
		}
		v.Outputs = append(v.Outputs, s)
	}
	if r.run != nil {
		elapsed := now.Sub(r.run.started)
		sv := &ScenarioView{Name: r.run.sc.Name, Title: r.run.sc.Title, StartedAt: r.run.started, ElapsedS: math.Floor(elapsed.Seconds()), LengthS: r.run.sc.Length.Seconds(), Finished: elapsed >= r.run.sc.Length}
		for i, ev := range r.run.sc.Events {
			sv.Events = append(sv.Events, ScenarioEventView{AtS: ev.At.Seconds(), Type: ev.Type, Note: ev.Note, Fired: r.run.fired[i]})
		}
		v.Scenario = sv
	}
	return v
}
