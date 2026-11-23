// allocmix: an allocation-heavy service-shaped heap: a long-lived binary
// tree and a 200k-entry map of records (live heap ~100 MB under gc) while
// every unit of work allocates short-lived trees, replaces map records and
// appends value structs to a slice. Each unit is timed: besides the round
// times it reports the tail latency of a unit (GC pauses, safepoints and
// deoptimizations included) over the last half of the rounds:
//
//	STAT allocmix unit_p50_us / unit_p99_us / unit_p999_us / unit_max_us
package main

import (
	"os"
	"sort"
	"strconv"

	"graalgo/bench/h"
)

type node struct{ l, r *node }

func tree(d int) *node {
	if d == 0 {
		return &node{}
	}
	return &node{tree(d - 1), tree(d - 1)}
}

func (n *node) count() int {
	if n.l == nil {
		return 1
	}
	return 1 + n.l.count() + n.r.count()
}

type rec struct {
	id    int
	name  string
	vals  [6]int64
	owner *node
}

type point struct{ x, y, z float64 }

const (
	mapSize = 200000
	units   = 400
)

var (
	longTree *node
	recs     map[int]*rec
	names    [64]string
	seed     uint32 = 1
	perRound [][]int32
)

func rnd(n int) int { seed = seed*1664525 + 1013904223; return int(seed>>8) % n }

func init() {
	longTree = tree(20)
	recs = make(map[int]*rec, mapSize)
	for i := range names {
		names[i] = "rec-" + string(rune('a'+i%26)) + string(rune('A'+i/26))
	}
	for i := 0; i < mapSize; i++ {
		recs[i] = &rec{id: i, name: names[i%64]}
	}
}

func unit(u int) int {
	t := tree(10).count()
	for i := 0; i < 500; i++ {
		k := rnd(mapSize)
		r := &rec{id: k, name: names[k%64], owner: longTree.l}
		r.vals[k%6] = int64(u)
		recs[k] = r
	}
	pts := make([]point, 0, 16)
	for i := 0; i < 2000; i++ {
		pts = append(pts, point{float64(i), float64(u), 1})
	}
	return t + len(pts) + int(pts[len(pts)/2].x)
}

func work() int {
	seed = 1
	lat := make([]int32, units)
	sum := 0
	for u := 0; u < units; u++ {
		t0 := h.Now()
		sum += unit(u)
		lat[u] = int32((h.Now() - t0) / 1000)
	}
	perRound = append(perRound, lat)
	return sum + longTree.count()
}

func main() {
	rounds := 10
	if v, err := strconv.Atoi(os.Getenv("ALLOCMIX_ROUNDS")); err == nil {
		rounds = v
	}
	h.Run("allocmix", rounds, work)
	var all []int
	for _, r := range perRound[len(perRound)/2:] {
		for _, d := range r {
			all = append(all, int(d))
		}
	}
	sort.Ints(all)
	q := func(p float64) int { return all[int(p*float64(len(all)-1))] }
	println("STAT allocmix unit_p50_us", q(0.50))
	println("STAT allocmix unit_p99_us", q(0.99))
	println("STAT allocmix unit_p999_us", q(0.999))
	println("STAT allocmix unit_max_us", all[len(all)-1])
}
