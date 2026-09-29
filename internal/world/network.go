package world

import (
	"container/heap"
	"math"
)

// The road network units drive on between sites: the estate haul roads and the concession road.
// The north gate and the public road are left out, so no routine trip leaves the operating
// area; only a geofence-exit behaviour does.

type edge struct {
	to int
	w  float64
}

type network struct {
	nodes []LonLat
	adj   [][]edge
}

var roads = buildNetwork()

func buildNetwork() *network {
	n := &network{}
	index := map[LonLat]int{}
	node := func(p LonLat) int {
		if i, ok := index[p]; ok {
			return i
		}
		index[p] = len(n.nodes)
		n.nodes = append(n.nodes, p)
		n.adj = append(n.adj, nil)
		return len(n.nodes) - 1
	}
	for _, r := range [][]LonLat{spineWest, spineNorth, spineSouth, spineEast, spineNorthEast, spineSouthEast, concessionRoad} {
		for i := 1; i < len(r); i++ {
			a, b := node(r[i-1]), node(r[i])
			if a == b {
				continue
			}
			w := Distance(r[i-1], r[i])
			n.adj[a] = append(n.adj[a], edge{b, w})
			n.adj[b] = append(n.adj[b], edge{a, w})
		}
	}
	return n
}

// Snap returns the point of the road network nearest to p.
func Snap(p LonLat) LonLat {
	q, _, _ := roads.snap(p)
	return q
}

// snap returns the nearest road point and the segment (as node indexes) it lies on.
func (n *network) snap(p LonLat) (LonLat, int, int) {
	best, bu, bv, bestD := p, -1, -1, math.Inf(1)
	for u := range n.adj {
		for _, e := range n.adj[u] {
			if e.to < u {
				continue
			}
			q, _ := project(p, n.nodes[u], n.nodes[e.to])
			if d := Distance(p, q); d < bestD {
				best, bu, bv, bestD = q, u, e.to, d
			}
		}
	}
	return best, bu, bv
}

// Route returns the road path between the road points nearest to a and b, from the first to
// the last. Callers add the off-road legs from a and to b themselves.
func Route(a, b LonLat) []LonLat {
	pa, ua, va := roads.snap(a)
	pb, ub, vb := roads.snap(b)
	if ua < 0 || ub < 0 {
		return []LonLat{a, b}
	}
	if (ua == ub && va == vb) || (ua == vb && va == ub) {
		return []LonLat{pa, pb}
	}
	// Two extra nodes split the segments a and b snap to.
	count := len(roads.nodes)
	src, dst := count, count+1
	nodes := append(append([]LonLat(nil), roads.nodes...), pa, pb)
	adj := make([][]edge, count+2)
	for i := range roads.adj {
		adj[i] = append([]edge(nil), roads.adj[i]...)
	}
	link := func(x, y int) {
		w := Distance(nodes[x], nodes[y])
		adj[x] = append(adj[x], edge{y, w})
		adj[y] = append(adj[y], edge{x, w})
	}
	link(src, ua)
	link(src, va)
	link(dst, ub)
	link(dst, vb)

	dist := make([]float64, len(nodes))
	prev := make([]int, len(nodes))
	for i := range dist {
		dist[i], prev[i] = math.Inf(1), -1
	}
	dist[src] = 0
	q := &queue{{src, 0}}
	for q.Len() > 0 {
		it := heap.Pop(q).(item)
		if it.d > dist[it.n] {
			continue
		}
		if it.n == dst {
			break
		}
		for _, e := range adj[it.n] {
			if d := it.d + e.w; d < dist[e.to] {
				dist[e.to], prev[e.to] = d, it.n
				heap.Push(q, item{e.to, d})
			}
		}
	}
	if math.IsInf(dist[dst], 1) {
		return []LonLat{pa, pb}
	}
	var path []LonLat
	for at := dst; at >= 0; at = prev[at] {
		path = append(path, nodes[at])
	}
	return Reverse(path)
}

type item struct {
	n int
	d float64
}

type queue []item

func (q queue) Len() int           { return len(q) }
func (q queue) Less(i, j int) bool { return q[i].d < q[j].d }
func (q queue) Swap(i, j int)      { q[i], q[j] = q[j], q[i] }
func (q *queue) Push(x any)        { *q = append(*q, x.(item)) }
func (q *queue) Pop() any          { old := *q; it := old[len(old)-1]; *q = old[:len(old)-1]; return it }
