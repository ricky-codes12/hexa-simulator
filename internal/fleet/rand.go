package fleet

// rng is SplitMix64: small, fast and fixed forever, so a seed gives the same fleet on every Go
// version (math/rand makes no such promise for new generators).
type rng struct{ state uint64 }

func newRNG(parts ...int64) *rng {
	r := &rng{state: 0x9e3779b97f4a7c15}
	for _, p := range parts {
		r.state ^= uint64(p)
		r.next()
	}
	return r
}

func (r *rng) next() uint64 {
	r.state += 0x9e3779b97f4a7c15
	z := r.state
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

// float returns a number in [0, 1).
func (r *rng) float() float64 { return float64(r.next()>>11) / (1 << 53) }

// between returns a number in [lo, hi).
func (r *rng) between(lo, hi float64) float64 { return lo + (hi-lo)*r.float() }

// intn returns an integer in [0, n).
func (r *rng) intn(n int) int {
	if n <= 0 {
		return 0
	}
	return int(r.next() % uint64(n))
}

// Hash mixes values into a stable 64-bit number, for per-record decisions such as which
// records an intermittent unit drops.
func Hash(parts ...int64) uint64 { return newRNG(parts...).next() }

// Fraction returns a stable number in [0, 1) from values.
func Fraction(parts ...int64) float64 { return float64(Hash(parts...)>>11) / (1 << 53) }
