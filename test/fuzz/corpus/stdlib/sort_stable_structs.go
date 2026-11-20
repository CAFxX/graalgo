// Targets sort.Stable / sort.SliceStable / slices.SortStableFunc on structs
// keyed by multiple fields: stability witnesses (original index must stay
// ascending within equal keys), multi-key comparators, sort.Slice on
// struct values with embedded arrays, and stable sorts of 10k records.
package main

import (
	"cmp"
	"slices"
	"sort"
	"strings"
)

type rec struct {
	name  string
	age   int8
	score float32
	tag   [2]byte
	idx   int
}

type byAge []rec

func (a byAge) Len() int           { return len(a) }
func (a byAge) Less(i, j int) bool { return a[i].age < a[j].age }
func (a byAge) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }

func gen(n int, seed uint32) []rec {
	out := make([]rec, n)
	names := []string{"ann", "bob", "cy", "dee", "eve", "fay", "gus"}
	for i := range out {
		seed = seed*1664525 + 1013904223
		out[i] = rec{
			name:  names[seed>>8%7],
			age:   int8(seed >> 12 % 9),
			score: float32(seed>>20%5) / 2,
			tag:   [2]byte{byte('a' + seed>>3%3), byte('x' + seed>>5%2)},
			idx:   i,
		}
	}
	return out
}

func stableWitness(a []rec, eq func(x, y rec) bool) bool {
	for i := 1; i < len(a); i++ {
		if eq(a[i-1], a[i]) && a[i-1].idx > a[i].idx {
			return false
		}
	}
	return true
}

func sum(a []rec) uint64 {
	var h uint64 = 14695981039346656037
	for _, r := range a {
		h = (h ^ uint64(r.idx)) * 1099511628211
		h = (h ^ uint64(r.age)) * 1099511628211
	}
	return h
}

func main() {
	for _, n := range []int{0, 1, 3, 10, 21, 64, 200, 1000, 10000} {
		base := gen(n, uint32(n)+5)

		a := slices.Clone(base)
		sort.Stable(byAge(a))
		w1 := stableWitness(a, func(x, y rec) bool { return x.age == y.age })

		b := slices.Clone(base)
		sort.SliceStable(b, func(i, j int) bool {
			if b[i].name != b[j].name {
				return b[i].name < b[j].name
			}
			return b[i].score > b[j].score
		})
		w2 := stableWitness(b, func(x, y rec) bool { return x.name == y.name && x.score == y.score })

		c := slices.Clone(base)
		slices.SortStableFunc(c, func(x, y rec) int {
			return cmp.Or(
				cmp.Compare(x.tag[0], y.tag[0]),
				-cmp.Compare(x.tag[1], y.tag[1]),
				strings.Compare(x.name, y.name),
			)
		})
		w3 := stableWitness(c, func(x, y rec) bool { return x.tag == y.tag && x.name == y.name })

		// stable sort by key2 then key1 == lexicographic by (key1,key2)
		d := slices.Clone(base)
		sort.SliceStable(d, func(i, j int) bool { return d[i].score < d[j].score })
		sort.SliceStable(d, func(i, j int) bool { return d[i].age < d[j].age })
		e := slices.Clone(base)
		sort.Slice(e, func(i, j int) bool {
			if e[i].age != e[j].age {
				return e[i].age < e[j].age
			}
			if e[i].score != e[j].score {
				return e[i].score < e[j].score
			}
			return e[i].idx < e[j].idx
		})
		same := true
		for i := range d {
			if d[i].idx != e[i].idx {
				same = false
			}
		}
		println(n, sum(a), w1, sum(b), w2, sum(c), w3, sum(d), same)
		if n == 10 || n == 21 {
			for _, r := range c {
				print(r.idx, ":", string(r.tag[:]), ":", r.name, " ")
			}
			println()
			for _, r := range b {
				print(r.idx, ":", r.name, " ")
			}
			println()
		}
		// unstable sort: result is deterministic for the same algorithm
		f := slices.Clone(base)
		sort.Sort(byAge(f))
		g := slices.Clone(base)
		slices.SortFunc(g, func(x, y rec) int { return cmp.Compare(x.age, y.age) })
		println("unstable", sum(f), sum(g), sort.IsSorted(byAge(f)), slices.IsSortedFunc(g, func(x, y rec) int { return int(x.age) - int(y.age) }))
	}
	// all-equal: stable must be identity
	eq := make([]rec, 500)
	for i := range eq {
		eq[i].idx = i
	}
	sort.Stable(byAge(eq))
	id := true
	for i := range eq {
		if eq[i].idx != i {
			id = false
		}
	}
	println("alleq", id)
	// reverse-sorted
	rv := make([]rec, 777)
	for i := range rv {
		rv[i] = rec{age: int8(100 - i%100), idx: i}
	}
	sort.Stable(byAge(rv))
	println("rv", sum(rv), stableWitness(rv, func(x, y rec) bool { return x.age == y.age }), rv[0].idx, rv[776].idx)
}
