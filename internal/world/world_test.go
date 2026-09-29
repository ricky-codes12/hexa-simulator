package world

import (
	"encoding/json"
	"testing"
)

func TestBlocksMatchSensorGrid(t *testing.T) {
	bs := Blocks()
	if len(bs) < 30 {
		t.Fatalf("blocks=%d, want the console's estate grid", len(bs))
	}
	seen := map[string]bool{}
	for _, b := range bs {
		if seen[b.Code] {
			t.Fatalf("duplicate block %s", b.Code)
		}
		seen[b.Code] = true
		estate := Kenanga
		if b.Estate == "MRT" {
			estate = Meranti
		}
		for _, p := range b.Ring {
			if !Inside(p, estate) {
				t.Fatalf("block %s corner %v outside %s", b.Code, p, b.Estate)
			}
			if Inside(p, ConservationZone) {
				t.Fatalf("block %s touches the conservation zone", b.Code)
			}
		}
	}
	if !seen["K-01"] || !seen["M-01"] {
		t.Fatalf("missing first block codes: %v", seen)
	}
}

func TestRouteStaysOnRoadsInsideOperatingArea(t *testing.T) {
	west := spineWest[len(spineWest)-1]
	path := Route(west, Landing)
	if len(path) < 4 {
		t.Fatalf("path=%v", path)
	}
	if path[0] != west || path[len(path)-1] != Landing {
		t.Fatalf("path ends %v %v", path[0], path[len(path)-1])
	}
	throughMill := false
	for _, p := range path {
		throughMill = throughMill || p == Mill
		if !Inside(p, OperatingArea) {
			t.Fatalf("route leaves the operating area at %v", p)
		}
	}
	if !throughMill {
		t.Fatal("west to landing must pass the mill junction")
	}
	if l := Length(path); l < 15000 || l > 22000 {
		t.Fatalf("route length=%.0f m", l)
	}
}

func TestExitPointIsOutsideOperatingArea(t *testing.T) {
	p, _ := ExitPoint(LonLat{104.062, -1.995}, OperatingArea, 250)
	if Inside(p, OperatingArea) {
		t.Fatalf("exit point %v is inside OPS", p)
	}
	if d := Distance(LonLat{104.062, -1.995}, p); d > 2500 {
		t.Fatalf("exit point %.0f m away", d)
	}
}

func TestGeoJSONHasEveryLayer(t *testing.T) {
	var fc struct {
		Features []struct {
			Properties map[string]any `json:"properties"`
		} `json:"features"`
	}
	if err := json.Unmarshal(GeoJSON(), &fc); err != nil {
		t.Fatal(err)
	}
	layers := map[string]int{}
	for _, f := range fc.Features {
		layers[f.Properties["layer"].(string)]++
	}
	for _, l := range []string{"estate", "fence", "block", "road", "water", "label"} {
		if layers[l] == 0 {
			t.Fatalf("layer %s missing: %v", l, layers)
		}
	}
	if layers["estate"] != 3 {
		t.Fatalf("estates=%d", layers["estate"])
	}
}
