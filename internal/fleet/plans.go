package fleet

import (
	"fmt"
	"math"

	"hexa-simulator/internal/world"
)

const goldenRatio = 0.6180339887498949

// StandbyTractor is the tractor number parked next to compartment SLG-C07 until a work
// behaviour sends it into the compartment (the demo timeline's "TR-004 starts compartment C07").
const StandbyTractor = 4

// Plan builds unit n's itinerary for a kind. n counts from 1 within the kind: seeded units use
// their roster number, operator-created devices a number derived from their ID. The same kind,
// n and seed always give the same itinerary.
func Plan(k *Kind, n int, seed int64) *Itinerary {
	r := newRNG(seed, int64(k.Digit), int64(n))
	var it *Itinerary
	switch k.Key {
	case "farm-tractor":
		it = tractor(n, r)
	case "dozer":
		it = dozer(r)
	case "excavator":
		it = excavator(n, r)
	case "pickup":
		it = pickup(n, r)
	case "water-truck", "fuel-bowser":
		it = rounds(k.Key, n, r)
	default:
		it = haulTruck(n, r)
	}
	// Spread units of a kind over their cycle, so a route has traffic in both directions.
	frac := math.Mod(float64(n)*goldenRatio+0.07*r.float(), 1)
	it.Phase = frac * it.Cycle
	return it
}

// haulSlots weights the routes: the log landing carries three trucks for each estate spine.
var haulSlots = []int{6, 0, 1, 6, 2, 3, 6, 4, 5}

func haulTruck(n int, r *rng) *Itinerary {
	route := world.HaulRoutes[haulSlots[(n-1)%len(haulSlots)]]
	load := route.Line
	if route.Name != "Log landing" {
		// Estate spines: load somewhere on the outer half of the road, beside a block.
		load = cut(route.Line, world.Length(route.Line)*r.between(0.45, 1))
	}
	b := newBuilder(world.Mill, world.Bearing(load[0], load[1]))
	b.stop(r.between(60, 150), true, "At the weighbridge")
	b.stop(r.between(120, 300), false, "Unloading at the mill")
	b.drive(load, r.between(44, 54), 0.35, "Empty to "+route.Name, false)
	b.stop(r.between(45, 120), true, "Waiting at the loader")
	b.stop(r.between(240, 540), false, "Loading")
	b.drive(world.Reverse(load), r.between(30, 40), 0.22, "Loaded to the mill", false)
	return b.build(world.Mill, world.EstateAt(load[len(load)-1]))
}

func tractor(n int, r *rng) *Itinerary {
	if n == StandbyTractor {
		at := StandbyPoint()
		b := newBuilder(at, 90)
		b.stop(3600, false, "Standby at compartment C07")
		return b.build(at, "SLG")
	}
	blocks := world.Blocks()
	k := n - 1
	if n > StandbyTractor {
		k--
	}
	blk := blocks[k%len(blocks)]
	slot := (k / len(blocks)) % 2
	lanes := 12 + r.intn(7)
	laneLen := r.between(520, 820)
	const spacing = 6.0
	origin := at(world.LonLat{blk.West, blk.North}, r.between(30, 90), 40+float64(slot)*560+r.between(0, 60))
	work := r.between(6, 10)
	b := newBuilder(at(origin, 0, spacing/2), 90)
	for i := 0; i < lanes; i++ {
		y := spacing/2 + spacing*float64(i)
		x0, x1, out := 0.0, laneLen, laneLen+5
		if i%2 == 1 {
			x0, x1, out = laneLen, 0, -5
		}
		b.drive([]world.LonLat{at(origin, x0, y), at(origin, x1, y)}, work+r.between(-0.6, 0.6), 0, fmt.Sprintf("Lane %d", i+1), true)
		if i == lanes-1 {
			break
		}
		ny := y + spacing
		b.drive([]world.LonLat{at(origin, x1, y), at(origin, out, y), at(origin, out, ny), at(origin, x1, ny)}, 4, 0, "Headland turn", false)
	}
	b.drive([]world.LonLat{b.pos, at(origin, 0, spacing/2)}, 11, 0, "Back to lane 1", false)
	b.stop(r.between(300, 900), false, "Break")
	return b.build(at(origin, 0, spacing/2), blk.Estate)
}

// StandbyPoint is where the standby tractor waits: 25 m north of compartment C07's north-west
// corner, outside the compartment.
func StandbyPoint() world.LonLat { return world.Offset(world.CompartmentC07[0], 25, 0) }

// CompartmentWork builds lane work across a compartment or block: east-west lanes 6 m apart from
// the north edge, each clipped to the outline with a 4 m headland, worked in a serpentine with
// the implement up on the turns. It is the work behaviour's itinerary.
func CompartmentWork(outline []world.LonLat, workKmh float64) *Itinerary {
	w, s, _, n := bbox(outline)
	const spacing, margin = 6.0, 4.0
	nw := world.LonLat{w, n}
	height := world.Distance(nw, world.LonLat{w, s})
	type lane struct{ a, b world.LonLat }
	var lanes []lane
	for i := 0; len(lanes) < 40; i++ {
		y := margin + spacing/2 + spacing*float64(i)
		if y > height-margin {
			break
		}
		lat := world.Offset(nw, y, 180)[1]
		west, east, ok := world.Span(outline, lat)
		if !ok {
			continue
		}
		a := world.Offset(world.LonLat{west, lat}, margin, 90)
		b := world.Offset(world.LonLat{east, lat}, margin, 270)
		if world.Distance(a, b) > 900 {
			b = world.Offset(a, 900, 90)
		}
		if b[0] > a[0] {
			lanes = append(lanes, lane{a, b})
		}
	}
	if len(lanes) == 0 {
		c := world.Centroid(outline)
		lanes = append(lanes, lane{c, c})
	}
	first := lanes[0].a
	bl := newBuilder(first, 90)
	for i, l := range lanes {
		from, to := l.a, l.b
		if i%2 == 1 {
			from, to = l.b, l.a
		}
		bl.drive([]world.LonLat{from, to}, workKmh, 0, fmt.Sprintf("Lane %d", i+1), true)
		if i == len(lanes)-1 {
			break
		}
		next := lanes[i+1].b
		if i%2 == 1 {
			next = lanes[i+1].a
		}
		bl.drive([]world.LonLat{to, next}, 3, 0, "Headland turn", false)
	}
	bl.drive([]world.LonLat{bl.pos, first}, 8, 0, "Back to lane 1", false)
	return bl.build(first, world.EstateAt(first))
}

func dozer(r *rng) *Itinerary {
	home := interiorPoint(r, world.Sialang, 350, func(p world.LonLat) bool {
		return !world.Inside(p, world.Meranti) && world.Distance(p, world.Centroid(world.CompartmentC07)) > 250
	})
	b := newBuilder(home, r.between(0, 360))
	pushes := 4 + r.intn(4)
	for i := 0; i < pushes; i++ {
		tip := world.Offset(home, r.between(20, 60), r.between(0, 360))
		b.drive([]world.LonLat{home, tip}, r.between(3, 5), 0, "Pushing", false)
		b.drive([]world.LonLat{tip, home}, r.between(3, 6), 0, "Reversing", false)
		if r.float() < 0.5 {
			b.stop(r.between(180, 720), true, "Idling")
		}
	}
	b.stop(r.between(600, 1500), false, "Break")
	return b.build(home, "SLG")
}

func excavator(n int, r *rng) *Itinerary {
	route := world.HaulRoutes[(n-1)%6].Line
	pt, h := world.Along(route, world.Length(route)*r.between(0.55, 0.98))
	side := 90.0
	if r.float() < 0.5 {
		side = -90
	}
	off := r.between(50, 110)
	home := pt
	for _, s := range []float64{side, -side} {
		if p := world.Offset(pt, off, h+s); clearOfZones(pt, p) && world.DistanceToEdge(p, world.ConservationZone) > 150 {
			home = p
			break
		}
	}
	b := newBuilder(home, h)
	moves := 3 + r.intn(4)
	for i := 0; i < moves; i++ {
		tip := world.Offset(home, r.between(6, 18), r.between(0, 360))
		b.drive([]world.LonLat{home, tip}, r.between(1.5, 3), 0, "Repositioning", false)
		b.stop(r.between(240, 720), true, "Loading logs")
		b.drive([]world.LonLat{tip, home}, r.between(1.5, 3), 0, "Repositioning", false)
	}
	b.stop(r.between(600, 1200), false, "Break")
	return b.build(home, world.EstateAt(home))
}

var officeEstates = []string{"KNG", "MRT", "SLG"}

// office is where an estate's staff park: the mill yard, the nursery and the log landing.
func office(estate string) (world.LonLat, string) {
	switch estate {
	case "MRT":
		return world.Nursery, "the nursery"
	case "SLG":
		return world.Offset(world.Landing, 80, 0), "the log landing"
	}
	return world.Offset(world.Mill, 90, 225), "the mill office"
}

func pickup(n int, r *rng) *Itinerary {
	estate := officeEstates[(n-1)%len(officeEstates)]
	home, _ := office(estate)
	var dests []stopPoint
	if estate == "SLG" {
		dests = append(dests, stopPoint{world.Offset(world.CompartmentC07[0], 40, 0), "compartment C07"})
		dests = append(dests, stopPoint{interiorPoint(r, world.Sialang, 350, func(p world.LonLat) bool { return !world.Inside(p, world.Meranti) }), "the harvest area"})
	} else {
		dests = append(dests, blockStops(r, estate, 2)...)
	}
	b := newBuilder(home, 0)
	b.stop(r.between(600, 1500), false, "At the estate office")
	for _, d := range dests {
		b.road(d.at, r.between(38, 50), 0.8, 18, "To "+d.name)
		b.stop(r.between(300, 900), false, "Inspecting "+d.name)
	}
	b.road(home, r.between(38, 50), 0.8, 18, "Back to the office")
	return b.build(home, estate)
}

type stopPoint struct {
	at   world.LonLat
	name string
}

// blockStops picks planting blocks of an estate whose centre can be reached off road from the
// nearest haul road without crossing the conservation zone.
func blockStops(r *rng, estate string, count int) []stopPoint {
	var candidates []world.Block
	for _, b := range world.Blocks() {
		if b.Estate == estate && clearOfZones(world.Snap(b.Centre()), b.Centre()) {
			candidates = append(candidates, b)
		}
	}
	var out []stopPoint
	for i := 0; i < count && len(candidates) > 0; i++ {
		j := r.intn(len(candidates))
		out = append(out, stopPoint{candidates[j].Centre(), "block " + candidates[j].Code})
		candidates = append(candidates[:j], candidates[j+1:]...)
	}
	return out
}

func rounds(kind string, n int, r *rng) *Itinerary {
	estate := officeEstates[(n-1)%len(officeEstates)]
	base := world.Offset(world.Mill, 110, 160)
	var dests []stopPoint
	switch {
	case estate == "SLG" && kind == "fuel-bowser":
		dests = append(dests, stopPoint{world.Landing, "the log landing"})
		for i := 0; i < 2; i++ {
			dests = append(dests, stopPoint{interiorPoint(r, world.Sialang, 400, func(p world.LonLat) bool { return !world.Inside(p, world.Meranti) }), "the dozers"})
		}
	case estate == "SLG":
		road := world.HaulRoutes[6].Line
		for i := 0; i < 2; i++ {
			p, _ := world.Along(road, world.Length(road)*r.between(0.72, 0.97))
			dests = append(dests, stopPoint{p, "the concession road"})
		}
		dests = append(dests, stopPoint{world.Landing, "the log landing"})
	default:
		for i := 0; i < 3; i++ {
			route := estateRoute(r, estate)
			p, _ := world.Along(route.Line, world.Length(route.Line)*r.between(0.4, 0.95))
			label := "the " + lower(route.Name) + " road"
			if kind == "fuel-bowser" {
				label = "the loaders on the " + lower(route.Name) + " road"
			}
			dests = append(dests, stopPoint{p, label})
		}
	}
	work := "Watering"
	if kind == "fuel-bowser" {
		work = "Refuelling"
	}
	b := newBuilder(base, 0)
	b.stop(r.between(600, 1200), false, "At the workshop")
	for _, d := range dests {
		b.road(d.at, r.between(34, 44), 0.3, 15, "To "+d.name)
		b.stop(r.between(150, 300), true, work+" at "+d.name)
	}
	b.road(base, r.between(34, 44), 0.3, 15, "Back to the workshop")
	return b.build(base, estate)
}

// estateRoute picks a haul route whose far end lies in the estate.
func estateRoute(r *rng, estate string) struct {
	Name string
	Line []world.LonLat
} {
	var in []int
	for i, route := range world.HaulRoutes[:6] {
		if world.EstateAt(route.Line[len(route.Line)-1]) == estate {
			in = append(in, i)
		}
	}
	if len(in) == 0 {
		return world.HaulRoutes[r.intn(6)]
	}
	return world.HaulRoutes[in[r.intn(len(in))]]
}

// interiorPoint picks a point of a ring at least margin metres from its edge that ok accepts.
func interiorPoint(r *rng, ring []world.LonLat, margin float64, ok func(world.LonLat) bool) world.LonLat {
	w, s, e, n := bbox(ring)
	for attempt := 0; attempt < 400; attempt++ {
		p := world.LonLat{r.between(w, e), r.between(s, n)}
		if world.Inside(p, ring) && world.DistanceToEdge(p, ring) >= margin && (ok == nil || ok(p)) {
			return p
		}
	}
	return world.Centroid(ring)
}

// clearOfZones reports whether the straight line a–b stays inside the operating area and out of
// the conservation zone, so an off-road leg cannot trip Sensor's zone rules.
func clearOfZones(a, b world.LonLat) bool {
	for i := 0; i <= 24; i++ {
		f := float64(i) / 24
		p := world.LonLat{a[0] + (b[0]-a[0])*f, a[1] + (b[1]-a[1])*f}
		if world.Inside(p, world.ConservationZone) || !world.Inside(p, world.OperatingArea) {
			return false
		}
	}
	return true
}

// at is the point x metres east and y metres south of origin.
func at(origin world.LonLat, x, y float64) world.LonLat {
	return world.Offset(world.Offset(origin, x, 90), y, 180)
}

// cut returns the first d metres of a polyline.
func cut(line []world.LonLat, d float64) []world.LonLat {
	out := []world.LonLat{line[0]}
	left := d
	for i := 1; i < len(line); i++ {
		seg := world.Distance(line[i-1], line[i])
		if left <= seg {
			f := 0.0
			if seg > 0 {
				f = left / seg
			}
			a, b := line[i-1], line[i]
			return append(out, world.LonLat{a[0] + (b[0]-a[0])*f, a[1] + (b[1]-a[1])*f})
		}
		out = append(out, line[i])
		left -= seg
	}
	return out
}

func bbox(ring []world.LonLat) (w, s, e, n float64) {
	w, s, e, n = math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, p := range ring {
		w, e = math.Min(w, p[0]), math.Max(e, p[0])
		s, n = math.Min(s, p[1]), math.Max(n, p[1])
	}
	return
}

func lower(s string) string {
	if s == "" {
		return s
	}
	b := []byte(s)
	if b[0] >= 'A' && b[0] <= 'Z' {
		b[0] += 'a' - 'A'
	}
	return string(b)
}
