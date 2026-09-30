package fleet

import (
	"math"
	"sort"

	"hexa-simulator/internal/world"
)

// Step is one piece of a unit's day: a drive along a path, or a stop.
type Step struct {
	Path     []world.LonLat // nil for a stop
	Kmh      float64        // cruise speed of a drive
	Accel    float64        // m/s²; 0 drives at constant speed
	Ignition bool
	Working  bool // implement down (din1)
	Label    string

	length, duration, start float64
	at                      world.LonLat // a stop's position
	heading                 float64      // a stop's heading
}

// Driving reports whether the step moves.
func (s *Step) Driving() bool { return s.Path != nil }

// Itinerary is a unit's repeating day. Evaluating it at a time is pure, so the same seed and
// clock always put a unit in the same place (plan S2: "deterministic from a seed").
type Itinerary struct {
	Steps  []Step
	Cycle  float64 // seconds
	Home   world.LonLat
	Estate string
	// Phase is where in the cycle the unit starts at T0.
	Phase float64
}

// State is where a unit is and what it does at one moment of its itinerary.
type State struct {
	Pos      world.LonLat
	Heading  float64
	Kmh      float64
	Cruise   float64 // the current drive's cruise speed, 0 on a stop
	Ignition bool
	Working  bool
	Step     int
	Label    string
}

// At evaluates the itinerary at t seconds of its clock.
func (it *Itinerary) At(t float64) State {
	if len(it.Steps) == 0 || it.Cycle <= 0 {
		return State{Pos: it.Home}
	}
	tau := math.Mod(t, it.Cycle)
	if tau < 0 {
		tau += it.Cycle
	}
	i := sort.Search(len(it.Steps), func(i int) bool { return it.Steps[i].start > tau }) - 1
	if i < 0 {
		i = 0
	}
	s := &it.Steps[i]
	local := tau - s.start
	if !s.Driving() {
		return State{Pos: s.at, Heading: s.heading, Ignition: s.Ignition, Working: s.Working, Step: i, Label: s.Label}
	}
	d, v := s.profile(local)
	pos, heading := world.Along(s.Path, d)
	return State{Pos: pos, Heading: heading, Kmh: v * 3.6, Cruise: s.Kmh, Ignition: s.Ignition, Working: s.Working && v > 0, Step: i, Label: s.Label}
}

// NextDrive returns the clock time at which the next drive after t starts, and whether there
// is one.
func (it *Itinerary) NextDrive(t float64) (float64, bool) {
	if len(it.Steps) == 0 || it.Cycle <= 0 {
		return t, false
	}
	base := t - math.Mod(t, it.Cycle)
	if math.Mod(t, it.Cycle) < 0 {
		base -= it.Cycle
	}
	for lap := 0; lap < 2; lap++ {
		for _, s := range it.Steps {
			if start := base + float64(lap)*it.Cycle + s.start; s.Driving() && start >= t {
				return start, true
			}
		}
	}
	return t, false
}

// profile returns the distance covered and the speed (m/s) at local seconds into a drive:
// accelerate, cruise and brake, or at constant speed when Accel is 0.
func (s *Step) profile(local float64) (float64, float64) {
	v := s.Kmh / 3.6
	if v <= 0 || s.length <= 0 {
		return 0, 0
	}
	local = math.Max(0, math.Min(local, s.duration))
	if s.Accel <= 0 {
		return math.Min(s.length, v*local), v
	}
	a := s.Accel
	ta := v / a
	da := v * v / (2 * a)
	if 2*da >= s.length {
		peak := math.Sqrt(a * s.length)
		ta = peak / a
		if local < ta {
			return 0.5 * a * local * local, a * local
		}
		left := s.duration - local
		return s.length - 0.5*a*left*left, a * left
	}
	switch {
	case local < ta:
		return 0.5 * a * local * local, a * local
	case local < s.duration-ta:
		return da + v*(local-ta), v
	default:
		left := s.duration - local
		return s.length - 0.5*a*left*left, a * left
	}
}

func (s *Step) computeDuration() {
	v := s.Kmh / 3.6
	s.length = world.Length(s.Path)
	if v <= 0 || s.length <= 0 {
		s.duration = 0
		return
	}
	if s.Accel <= 0 {
		s.duration = s.length / v
		return
	}
	ta := v / s.Accel
	da := v * v / (2 * s.Accel)
	if 2*da >= s.length {
		s.duration = 2 * math.Sqrt(s.length/s.Accel)
		return
	}
	s.duration = 2*ta + (s.length-2*da)/v
}

// builder assembles an itinerary from a start position.
type builder struct {
	steps   []Step
	pos     world.LonLat
	heading float64
}

func newBuilder(start world.LonLat, heading float64) *builder {
	return &builder{pos: start, heading: heading}
}

// drive adds a drive along path. A path that does not start where the unit is gets a straight
// connector first.
func (b *builder) drive(path []world.LonLat, kmh, accel float64, label string, working bool) {
	if len(path) == 0 {
		return
	}
	p := path
	if world.Distance(b.pos, path[0]) > 0.5 {
		p = append([]world.LonLat{b.pos}, path...)
	}
	if world.Length(p) < 1 {
		return
	}
	s := Step{Path: p, Kmh: kmh, Accel: accel, Ignition: true, Working: working, Label: label}
	s.computeDuration()
	b.steps = append(b.steps, s)
	b.pos = p[len(p)-1]
	for i := len(p) - 1; i > 0; i-- {
		if world.Distance(p[i-1], p[i]) > 0.1 {
			b.heading = world.Bearing(p[i-1], p[i])
			break
		}
	}
}

// stop adds a stop where the unit is.
func (b *builder) stop(seconds float64, ignition bool, label string) {
	if seconds <= 0 {
		return
	}
	b.steps = append(b.steps, Step{Ignition: ignition, Label: label, duration: seconds, at: b.pos, heading: b.heading})
}

// road drives from where the unit is to dest over the road network: off road to the nearest
// road at offKmh, along the roads at kmh, then off road to dest.
func (b *builder) road(dest world.LonLat, kmh, accel, offKmh float64, label string) {
	path := world.Route(b.pos, dest)
	if world.Distance(b.pos, path[0]) > 15 {
		b.drive([]world.LonLat{b.pos, path[0]}, offKmh, 0, label, false)
	}
	b.drive(path, kmh, accel, label, false)
	if world.Distance(b.pos, dest) > 15 {
		b.drive([]world.LonLat{b.pos, dest}, offKmh, 0, label, false)
	} else {
		b.pos = dest
	}
}

func (b *builder) build(home world.LonLat, estate string) *Itinerary {
	it := &Itinerary{Steps: b.steps, Home: home, Estate: estate}
	t := 0.0
	for i := range it.Steps {
		it.Steps[i].start = t
		t += it.Steps[i].duration
	}
	it.Cycle = t
	return it
}
