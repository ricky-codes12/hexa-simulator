// Package world is the synthetic forestry world the simulator drives its fleet on.
//
// Hexa.Sensor owns this world (demo fleet plan §0): every coordinate below is copied from its
// seed-demo set at demo revision 3, the Musi Banyuasin layout of hexa-sensor ADR 0034
// (internal/rules/forestrydemo.go, internal/work/forestrydemo.go and
// web/src/lib/live/demo/world.ts at 10ecfc2). Units therefore drive on the roads and work in the
// blocks that Sensor draws, and trip the geofences Sensor seeds. When Sensor moves its world,
// update this package and Revision together. Every name and line is synthetic.
package world

import (
	"fmt"
	"math"
)

// Revision is Hexa.Sensor's demo world revision this package mirrors (its DemoRevision label).
const Revision = "3"

// LonLat is a WGS84 position as [longitude, latitude], the GeoJSON order.
type LonLat [2]float64

// Lon and Lat name the two halves.
func (p LonLat) Lon() float64 { return p[0] }
func (p LonLat) Lat() float64 { return p[1] }

// Estate is one of Sensor's estates by code.
type Estate struct{ Code, Name string }

// Estates are Sensor's seed-demo estates.
var Estates = []Estate{{"KNG", "Kenanga Estate"}, {"MRT", "Meranti Estate"}, {"SLG", "Sialang Concession"}}

// EstateName returns an estate's name, or the code when it is unknown.
func EstateName(code string) string {
	for _, e := range Estates {
		if e.Code == code {
			return e.Name
		}
	}
	return code
}

var (
	Kenanga = []LonLat{{104.01, -1.981}, {104.045, -1.976}, {104.082, -1.979}, {104.084, -2.021}, {104.082, -2.066}, {104.045, -2.07}, {104.013, -2.064}, {104.008, -2.026}, {104.01, -1.981}}
	Meranti = []LonLat{{104.082, -1.979}, {104.122, -1.982}, {104.156, -1.988}, {104.158, -2.026}, {104.153, -2.058}, {104.116, -2.062}, {104.082, -2.066}, {104.084, -2.021}, {104.082, -1.979}}
	// Sialang is the harvesting concession east of Meranti. Its east edge is the straight line
	// on 104.208°.
	Sialang = []LonLat{{104.156, -1.988}, {104.184, -1.991}, {104.208, -1.998}, {104.208, -2.054}, {104.18, -2.06}, {104.153, -2.058}, {104.158, -2.026}, {104.156, -1.988}}
	// OperatingArea is geofence OPS: both estates and the concession. Sensor raises "Left the
	// operating area" for any unit outside it.
	OperatingArea = closed(LonLat{104.01, -1.981}, LonLat{104.045, -1.976}, LonLat{104.082, -1.979}, LonLat{104.122, -1.982}, LonLat{104.156, -1.988},
		LonLat{104.184, -1.991}, LonLat{104.208, -1.998}, LonLat{104.208, -2.054}, LonLat{104.18, -2.06}, LonLat{104.153, -2.058},
		LonLat{104.116, -2.062}, LonLat{104.082, -2.066}, LonLat{104.045, -2.07}, LonLat{104.013, -2.064}, LonLat{104.008, -2.026})
	// ConservationZone is geofence CZ-SUNGAI-KECIL, the river buffer no machine may enter.
	ConservationZone = closed(LonLat{104.013, -2.04}, LonLat{104.026, -2.036}, LonLat{104.036, -2.046}, LonLat{104.034, -2.062}, LonLat{104.015, -2.06})
	MillYard         = rect(104.074, -2.026, 104.081, -2.018)
	NurserySite      = rect(104.125, -1.9985, 104.141, -1.9895)
	// CompartmentC07 is compartment SLG-C07 (3.3 ha), where Sensor's work order WO-SLG-0142 runs.
	CompartmentC07 = []LonLat{{104.18712, -2.04297}, {104.1889, -2.04293}, {104.18896, -2.04368}, {104.18887, -2.04442}, {104.18709, -2.04438}, {104.18704, -2.0437}, {104.18712, -2.04297}}
)

// Sites named on Sensor's map.
var (
	Mill      = LonLat{104.0775, -2.022}
	Landing   = LonLat{104.182, -2.026}  // the concession's log landing
	NurseryNW = LonLat{104.125, -1.9895} // north-west corner of the nursery site
	Nursery   = LonLat{104.133, -1.994}  // middle of the nursery site
)

// Bounds is the south-west and north-east corner of the map Sensor shows for this world.
var Bounds = [2]LonLat{{103.998, -2.078}, {104.215, -1.964}}

// Road is one drawn road.
type Road struct {
	Name  string
	Class string // haul | public
	Line  []LonLat
}

var (
	spineWest      = []LonLat{Mill, {104.05, -2.02}, {104.025, -2.021}, {104.012, -2.024}}
	trunkNorth     = []LonLat{Mill, {104.07, -2.006}, {104.062, -1.991}}
	spineNorth     = append(append([]LonLat(nil), trunkNorth...), LonLat{104.035, -1.993}, LonLat{104.015, -1.994})
	spineSouth     = []LonLat{Mill, {104.07, -2.041}, {104.05, -2.051}, {104.042, -2.054}}
	spineEast      = []LonLat{Mill, {104.1, -2.021}, {104.125, -2.024}, {104.152, -2.026}}
	spineNorthEast = []LonLat{Mill, {104.1, -2.021}, {104.11, -2.004}, {104.126, -1.999}}
	spineSouthEast = []LonLat{Mill, {104.1, -2.021}, {104.115, -2.041}, {104.14, -2.051}}
	// concessionRoad runs from the east haul road to the log landing.
	concessionRoad = []LonLat{{104.152, -2.026}, {104.167, -2.0255}, Landing}
	// gateRun leaves through the north gate onto the public road.
	gateRun    = []LonLat{{104.062, -1.991}, {104.055, -1.9775}, {104.055, -1.968}, {104.08, -1.9672}}
	publicRoad = []LonLat{{103.998, -1.967}, {104.055, -1.968}, {104.1, -1.966}, {104.17, -1.969}, {104.215, -1.972}}
	river      = []LonLat{{104, -2.028}, {104.012, -2.034}, {104.02, -2.04}, {104.026, -2.048}, {104.023, -2.056}, {104.03, -2.066}, {104.038, -2.078}}
)

// Roads are the haul roads as Sensor draws them, plus the north gate and the public road.
var Roads = []Road{
	{"West haul road", "haul", spineWest},
	{"North haul road", "haul", spineNorth},
	{"South haul road", "haul", spineSouth},
	{"East haul road", "haul", spineEast},
	{"North-east haul road", "haul", spineNorthEast[1:]},
	{"South-east haul road", "haul", spineSouthEast[1:]},
	{"North gate", "haul", gateRun[:3]},
	{"Concession road", "haul", concessionRoad},
	{"Public road", "public", publicRoad},
}

// HaulRoutes are the loaded runs between the mill and where logs are picked up: the six estate
// spines and the log landing. Each starts at the mill.
var HaulRoutes = []struct {
	Name string
	Line []LonLat
}{
	{"West", spineWest},
	{"North", spineNorth},
	{"South", spineSouth},
	{"East", spineEast},
	{"North-east", spineNorthEast},
	{"South-east", spineSouthEast},
	{"Log landing", append(append([]LonLat(nil), spineEast...), concessionRoad[1:]...)},
}

// GateRun is the drive from the north junction out of the gate and east on the public road.
func GateRun() []LonLat { return append([]LonLat(nil), gateRun...) }

// Block is a planting block as the console draws it.
type Block struct {
	Code   string // K-01, M-07
	Estate string // KNG | MRT
	Ring   []LonLat
	// West, South, East and North bound the block's drawn rectangle.
	West, South, East, North float64
}

// Centre is the middle of the block.
func (b Block) Centre() LonLat { return LonLat{(b.West + b.East) / 2, (b.South + b.North) / 2} }

var blocks = buildBlocks()

// Blocks returns the planting blocks: a 0.012° grid clipped to each estate by whole blocks,
// minus the river zone. This is the console's blocks() in world.ts, step for step, so the
// codes and outlines match Sensor's map.
func Blocks() []Block { return append([]Block(nil), blocks...) }

// BlockByCode finds a block by code.
func BlockByCode(code string) (Block, bool) {
	for _, b := range blocks {
		if b.Code == code {
			return b, true
		}
	}
	return Block{}, false
}

func buildBlocks() []Block {
	var out []Block
	const step = 0.012
	k, m := 0, 0
	for lon := 104.008; lon < 104.158; lon += step {
		for lat := -2.07; lat < -1.976; lat += step {
			c := LonLat{lon + step/2, lat + step/2}
			corners := []LonLat{{lon, lat}, {lon + step, lat}, {lon, lat + step}, {lon + step, lat + step}}
			inK := allInside(corners, Kenanga)
			inM := !inK && allInside(corners, Meranti)
			if !inK && !inM {
				continue
			}
			restricted := Inside(c, ConservationZone)
			for _, p := range corners {
				restricted = restricted || Inside(p, ConservationZone)
			}
			if restricted {
				continue
			}
			b := Block{West: lon + 0.0006, South: lat + 0.0006, East: lon + step - 0.0006, North: lat + step - 0.0006}
			if inK {
				k++
				b.Code, b.Estate = code("K", k), "KNG"
			} else {
				m++
				b.Code, b.Estate = code("M", m), "MRT"
			}
			b.Ring = rect(b.West, b.South, b.East, b.North)
			out = append(out, b)
		}
	}
	return out
}

func code(prefix string, n int) string { return fmt.Sprintf("%s-%02d", prefix, n) }

func allInside(points []LonLat, ring []LonLat) bool {
	for _, p := range points {
		if !Inside(p, ring) {
			return false
		}
	}
	return true
}

// Fence is one of Sensor's seed-demo geofences.
type Fence struct {
	Code, Name, Kind, Estate string
	Ring                     []LonLat
}

// Fences are the geofences Sensor's seed-demo installs (rules and work sets), by code.
func Fences() []Fence {
	return []Fence{
		{"OPS", "Operating area", "area", "", OperatingArea},
		{"KNG", "Kenanga Estate", "estate", "KNG", Kenanga},
		{"MRT", "Meranti Estate", "estate", "MRT", Meranti},
		{"SLG", "Sialang Concession", "estate", "SLG", Sialang},
		{"CZ-SUNGAI-KECIL", "River conservation zone", "restricted", "KNG", ConservationZone},
		{"MILL", "Mill yard", "site", "KNG", MillYard},
		{"NURSERY", "Nursery", "site", "MRT", NurserySite},
		{"SLG-C07", "Compartment C07", "compartment", "SLG", CompartmentC07},
	}
}

// Compartment returns the outline of a work area by code: a compartment geofence (SLG-C07) or
// a planting block (K-01, M-07).
func Compartment(code string) ([]LonLat, bool) {
	if code == "SLG-C07" || code == "C07" {
		return CompartmentC07, true
	}
	if b, ok := BlockByCode(code); ok {
		return b.Ring, true
	}
	return nil, false
}

// EstateAt returns the estate a point belongs to, as Sensor's simulator decides it.
func EstateAt(p LonLat) string {
	if Inside(p, Sialang) && !Inside(p, Meranti) {
		return "SLG"
	}
	if Inside(p, Meranti) && !Inside(p, Kenanga) {
		return "MRT"
	}
	return "KNG"
}

// ---- geometry --------------------------------------------------------------------------------

const earthR = 6_371_000.0

func rad(d float64) float64 { return d * math.Pi / 180 }

// Distance is the distance in metres between two nearby points (equirectangular).
func Distance(a, b LonLat) float64 {
	x := rad(b[0]-a[0]) * math.Cos(rad((a[1]+b[1])/2))
	y := rad(b[1] - a[1])
	return math.Hypot(x, y) * earthR
}

// Bearing is the compass bearing from a to b in degrees, 0 to 360.
func Bearing(a, b LonLat) float64 {
	x := rad(b[0]-a[0]) * math.Cos(rad((a[1]+b[1])/2))
	y := rad(b[1] - a[1])
	return math.Mod(math.Atan2(x, y)*180/math.Pi+360, 360)
}

// Length is a polyline's length in metres.
func Length(line []LonLat) float64 {
	total := 0.0
	for i := 1; i < len(line); i++ {
		total += Distance(line[i-1], line[i])
	}
	return total
}

// Along returns the point d metres along a polyline and the bearing of its segment there.
func Along(line []LonLat, d float64) (LonLat, float64) {
	if len(line) == 0 {
		return LonLat{}, 0
	}
	if len(line) == 1 {
		return line[0], 0
	}
	left := math.Max(0, d)
	for i := 1; i < len(line); i++ {
		a, b := line[i-1], line[i]
		seg := Distance(a, b)
		if left <= seg || i == len(line)-1 {
			f := 0.0
			if seg > 0 {
				f = math.Min(1, left/seg)
			}
			return LonLat{a[0] + (b[0]-a[0])*f, a[1] + (b[1]-a[1])*f}, Bearing(a, b)
		}
		left -= seg
	}
	return line[len(line)-1], 0
}

// Inside reports whether p lies inside a ring (ray casting, as Sensor's geo helpers do it).
func Inside(p LonLat, ring []LonLat) bool {
	hit := false
	for i, j := 0, len(ring)-1; i < len(ring); j, i = i, i+1 {
		xi, yi, xj, yj := ring[i][0], ring[i][1], ring[j][0], ring[j][1]
		if (yi > p[1]) != (yj > p[1]) && p[0] < (xj-xi)*(p[1]-yi)/(yj-yi)+xi {
			hit = !hit
		}
	}
	return hit
}

// Offset moves p by metres towards a compass bearing.
func Offset(p LonLat, metres, heading float64) LonLat {
	dy := metres * math.Cos(rad(heading)) / earthR
	dx := metres * math.Sin(rad(heading)) / (earthR * math.Cos(rad(p[1])))
	return LonLat{p[0] + dx*180/math.Pi, p[1] + dy*180/math.Pi}
}

// Reverse returns a reversed copy of a polyline.
func Reverse(line []LonLat) []LonLat {
	out := make([]LonLat, len(line))
	for i, p := range line {
		out[len(line)-1-i] = p
	}
	return out
}

// DistanceToEdge is the distance in metres from p to the nearest edge of a ring.
func DistanceToEdge(p LonLat, ring []LonLat) float64 {
	best := math.Inf(1)
	for i := 1; i < len(ring); i++ {
		q, _ := project(p, ring[i-1], ring[i])
		best = math.Min(best, Distance(p, q))
	}
	return best
}

// ExitPoint returns a point `beyond` metres outside the nearest edge of a ring, seen from p,
// and the bearing from p towards it.
func ExitPoint(p LonLat, ring []LonLat, beyond float64) (LonLat, float64) {
	best, bestD := LonLat{}, math.Inf(1)
	for i := 1; i < len(ring); i++ {
		q, _ := project(p, ring[i-1], ring[i])
		if d := Distance(p, q); d < bestD {
			best, bestD = q, d
		}
	}
	h := Bearing(p, best)
	if bestD < 1 {
		h = Bearing(Centroid(ring), p)
	}
	return Offset(best, beyond, h), h
}

// Centroid is the mean of a ring's distinct vertices.
func Centroid(ring []LonLat) LonLat {
	n := len(ring)
	if n > 1 && ring[0] == ring[n-1] {
		n--
	}
	var c LonLat
	for _, p := range ring[:n] {
		c[0] += p[0]
		c[1] += p[1]
	}
	if n > 0 {
		c[0] /= float64(n)
		c[1] /= float64(n)
	}
	return c
}

// project returns the point of segment ab nearest to p, and its fraction along the segment.
func project(p, a, b LonLat) (LonLat, float64) {
	cos := math.Cos(rad(p[1]))
	ax, ay := (a[0]-p[0])*cos, a[1]-p[1]
	bx, by := (b[0]-p[0])*cos, b[1]-p[1]
	dx, dy := bx-ax, by-ay
	l := dx*dx + dy*dy
	f := 0.0
	if l > 0 {
		f = math.Max(0, math.Min(1, -(ax*dx+ay*dy)/l))
	}
	return LonLat{a[0] + (b[0]-a[0])*f, a[1] + (b[1]-a[1])*f}, f
}

func closed(points ...LonLat) []LonLat { return append(points, points[0]) }

func rect(w, s, e, n float64) []LonLat {
	return []LonLat{{w, n}, {e, n}, {e, s}, {w, s}, {w, n}}
}

// Span returns the westmost and eastmost points where the parallel at lat crosses a ring.
func Span(ring []LonLat, lat float64) (west, east float64, ok bool) {
	west, east = math.Inf(1), math.Inf(-1)
	for i := 1; i < len(ring); i++ {
		a, b := ring[i-1], ring[i]
		if (a[1] > lat) == (b[1] > lat) {
			continue
		}
		x := a[0] + (b[0]-a[0])*(lat-a[1])/(b[1]-a[1])
		west, east = math.Min(west, x), math.Max(east, x)
	}
	return west, east, east > west
}
