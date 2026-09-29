package fleet

import "fmt"

// Unit is one seeded device of the demo fleet.
type Unit struct {
	Kind   *Kind
	Number int    // within the kind, from 1
	Name   string // HT-012
	IMEI   string
	Model  string
	Output string
	Estate string
}

// Roster lists the seeded fleet for a composition and seed, in a stable order: kinds in roster
// order, units by number. Names, IMEIs and estates depend only on the kind and the number, so a
// unit keeps its identity when the composition changes.
func Roster(c Composition, seed int64) []Unit {
	var out []Unit
	for _, k := range Kinds {
		for n := 1; n <= c[k.Key]; n++ {
			out = append(out, Unit{
				Kind: k, Number: n, Name: UnitName(k, n), IMEI: SeededIMEI(k, n),
				Model: k.Model(n), Output: k.DefaultOutput(n), Estate: Plan(k, n, seed).Estate,
			})
		}
	}
	return out
}

// UnitName is a seeded unit's readable name: HT-012, TR-004, DZ-002.
func UnitName(k *Kind, n int) string { return fmt.Sprintf("%s-%03d", k.Code, n) }

// IMEIPrefix starts every seeded IMEI. Hexa.Sensor's own simulator fleet uses 3563070424…, so
// the two fleets can share the estates without an identity collision (plan §5).
const IMEIPrefix = "3598150"

// SeededIMEI is a seeded unit's 15-digit IMEI: the prefix, the kind's digit, the unit number in
// six digits and a Luhn check digit, so the kind can be read from the IMEI.
func SeededIMEI(k *Kind, n int) string {
	body := fmt.Sprintf("%s%d%06d", IMEIPrefix, k.Digit, n%1000000)
	return body + string(rune('0'+luhn(body)))
}

// luhn returns the check digit for a digit string.
func luhn(body string) int {
	sum := 0
	double := true
	for i := len(body) - 1; i >= 0; i-- {
		d := int(body[i] - '0')
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	return (10 - sum%10) % 10
}
