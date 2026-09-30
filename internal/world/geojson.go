package world

import "encoding/json"

// Label is a map label Sensor places on this world.
type Label struct {
	Text string
	At   LonLat
	Kind string // estate | site
}

// Labels are the names Sensor's map writes on the world.
var Labels = []Label{
	{"Kenanga Estate", LonLat{104.032, -2.004}, "estate"},
	{"Meranti Estate", LonLat{104.132, -2.034}, "estate"},
	{"Sialang Concession", LonLat{104.184, -2.004}, "estate"},
	{"Log landing", LonLat{104.182, -2.0305}, "site"},
	{"Mill", LonLat{104.0775, -2.0272}, "site"},
	{"Nursery", LonLat{104.133, -1.9875}, "site"},
	{"Conservation zone", LonLat{104.024, -2.0525}, "site"},
}

type feature struct {
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties"`
	Geometry   geometry       `json:"geometry"`
}

type geometry struct {
	Type        string `json:"type"`
	Coordinates any    `json:"coordinates"`
}

// GeoJSON returns the world as one FeatureCollection for the console map. Every feature has a
// `layer`: estate, fence, block, road, water or label.
func GeoJSON() []byte { return geojson }

var geojson = buildGeoJSON()

func buildGeoJSON() []byte {
	var fs []feature
	polygon := func(props map[string]any, ring []LonLat) {
		fs = append(fs, feature{"Feature", props, geometry{"Polygon", [][]LonLat{ring}}})
	}
	for _, f := range Fences() {
		layer := "fence"
		if f.Kind == "estate" {
			layer = "estate"
		}
		polygon(map[string]any{"layer": layer, "code": f.Code, "name": f.Name, "kind": f.Kind, "estate": f.Estate}, f.Ring)
	}
	for _, b := range blocks {
		polygon(map[string]any{"layer": "block", "code": b.Code, "estate": b.Estate}, b.Ring)
	}
	for _, r := range Roads {
		fs = append(fs, feature{"Feature", map[string]any{"layer": "road", "class": r.Class, "name": r.Name}, geometry{"LineString", r.Line}})
	}
	fs = append(fs, feature{"Feature", map[string]any{"layer": "water", "name": "Sungai Kecil"}, geometry{"LineString", river}})
	for _, l := range Labels {
		fs = append(fs, feature{"Feature", map[string]any{"layer": "label", "text": l.Text, "kind": l.Kind}, geometry{"Point", l.At}})
	}
	out, err := json.Marshal(map[string]any{
		"type":     "FeatureCollection",
		"bbox":     []float64{Bounds[0][0], Bounds[0][1], Bounds[1][0], Bounds[1][1]},
		"revision": Revision,
		"features": fs,
	})
	if err != nil {
		panic(err)
	}
	return out
}
