package main

import "sort"

// so01: sort.Stable keeps the original order of equal elements.
// (key, id) pairs sorted by key: ids must stay ascending within a key.

type pair struct {
	key int
	id  int
}

type byKey []pair

func (p byKey) Len() int           { return len(p) }
func (p byKey) Less(i, j int) bool { return p[i].key < p[j].key }
func (p byKey) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

func main() {
	ps := []pair{
		{3, 0}, {1, 1}, {3, 2}, {2, 3}, {1, 4}, {3, 5}, {2, 6},
	}
	sort.Stable(byKey(ps))
	for _, p := range ps {
		print(p.key, ":", p.id, " ")
	}
	println()
	// Verify stability: ids ascending within each key run.
	ok := true
	for i := 1; i < len(ps); i++ {
		if ps[i].key == ps[i-1].key && ps[i].id < ps[i-1].id {
			ok = false
		}
		if ps[i].key < ps[i-1].key {
			ok = false
		}
	}
	println(ok) // true
	// Stable on an already-sorted and a reverse-sorted input.
	qs := []pair{{1, 0}, {1, 1}, {2, 2}, {2, 3}}
	sort.Stable(byKey(qs))
	println(qs[0].id, qs[1].id, qs[2].id, qs[3].id) // 0 1 2 3
}
