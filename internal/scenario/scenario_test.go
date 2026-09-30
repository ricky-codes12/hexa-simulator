package scenario

import "testing"

func TestTimelinesAreOrderedAndValid(t *testing.T) {
	for _, sc := range Scenarios {
		for i, ev := range sc.Events {
			if i > 0 && ev.At < sc.Events[i-1].At {
				t.Fatalf("%s: event %q comes before the one above it", sc.Name, ev.Note)
			}
			if ev.At > sc.Length {
				t.Fatalf("%s: event %q is after the end", sc.Name, ev.Note)
			}
			if ev.Type == "" {
				continue
			}
			if _, err := Normalize(ev.Type, ev.Params); err != nil {
				t.Fatalf("%s: %v", sc.Name, err)
			}
			if ev.Target.Empty() {
				t.Fatalf("%s: event %q targets the whole fleet", sc.Name, ev.Note)
			}
		}
	}
}

func TestNormalizeFillsDefaultsAndRejectsUnknownSettings(t *testing.T) {
	p, err := Normalize(Overspeed, nil)
	if err != nil || p.Number("kmh", 0) != 82 {
		t.Fatalf("params=%v err=%v", p, err)
	}
	if _, err := Normalize(Overspeed, Params{"kmh": 40.0}); err == nil {
		t.Fatal("an overspeed under the limit must be refused")
	}
	if _, err := Normalize(Park, Params{"colour": "red"}); err == nil {
		t.Fatal("an unknown setting must be refused")
	}
	if p, _ := Normalize(Work, Params{"compartment": "K-03"}); p.String("compartment", "") != "K-03" {
		t.Fatalf("work params %v", p)
	}
}

func TestTargetSelectsByNameKindAndLimit(t *testing.T) {
	all := []Candidate{{1, "PU-002", "pickup", "KNG", "mqtt"}, {2, "PU-001", "pickup", "KNG", "mqtt"}, {3, "PU-003", "pickup", "MRT", "mqtt"}, {4, "HT-012", "haul-truck", "KNG", "mqtt"}}
	if got := (Target{Kind: "pickup", Estate: "KNG", Limit: 1}).Select(all); len(got) != 1 || got[0] != 2 {
		t.Fatalf("got %v, want the first by name (PU-001)", got)
	}
	if got := (Target{Names: []string{"ht-012"}}).Select(all); len(got) != 1 || got[0] != 4 {
		t.Fatalf("names are matched without case: %v", got)
	}
}
