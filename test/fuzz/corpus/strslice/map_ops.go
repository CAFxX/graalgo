// Targets map operations: nil map read ok / write panic, comma-ok, delete
// of absent keys, deletion and insertion during iteration (only
// order-independent facts printed), map of slices / maps, big maps, growth.
package main

import (
	"maps"
	"slices"
	"strconv"
)

func protect(name string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(interface{ Error() string }); ok {
				println(name, "panic:", e.Error())
			} else if s, ok := r.(string); ok {
				println(name, "panic str:", s)
			}
		}
	}()
	f()
}

type rec struct {
	n    int
	tags []string
}

func main() {
	var nm map[string]int
	v, ok := nm["x"]
	println("nilread", v, ok, len(nm))
	delete(nm, "x")
	for range nm {
		println("never")
	}
	protect("nilwrite", func() { nm["x"] = 1 })
	protect("nilinc", func() { nm["y"]++ })
	var nmm map[int]map[int]int
	println("nilnested", nmm[1][2], len(nmm[5]))
	protect("nilnestedwrite", func() {
		mm := map[int]map[int]int{}
		mm[1][2] = 3
	})
	// comma ok with zero values
	m := map[string]int{"zero": 0}
	a, ok1 := m["zero"]
	b, ok2 := m["missing"]
	println("commaok", a, ok1, b, ok2)
	delete(m, "missing")
	delete(m, "zero")
	println("afterdelete", len(m))
	// deleting during iteration: all entries deleted, each visited once
	dm := map[int]int{}
	for i := 0; i < 1000; i++ {
		dm[i] = i
	}
	visited := 0
	for k := range dm {
		delete(dm, k)
		visited++
	}
	println("delall", visited, len(dm))
	// delete others during iteration: removed entries not produced later
	dm2 := map[int]bool{}
	for i := 0; i < 200; i++ {
		dm2[i] = true
	}
	seen := map[int]bool{}
	for k := range dm2 {
		seen[k] = true
		partner := k ^ 1
		delete(dm2, partner)
	}
	bothSeen := 0
	for k := range seen {
		if seen[k^1] {
			bothSeen++
		}
	}
	println("pairs", len(dm2), len(seen), bothSeen)
	// insertion during iteration: count bounded, original keys all seen
	im := map[int]int{0: 0, 1: 1, 2: 2, 3: 3}
	origSeen := 0
	iters := 0
	for k := range im {
		if k < 4 {
			origSeen++
		}
		iters++
		if len(im) < 64 {
			im[k+100] = 1
		}
	}
	println("insert", origSeen, iters >= 4, len(im) >= 4, len(im) <= 68)
	// value modification via reassign
	vm := map[string]rec{"a": {1, nil}}
	r := vm["a"]
	r.n = 99
	r.tags = append(r.tags, "t")
	println("structval", vm["a"].n, len(vm["a"].tags))
	vm["a"] = r
	println("reassigned", vm["a"].n, vm["a"].tags[0])
	pm := map[string]*rec{"a": {1, nil}}
	pm["a"].n = 42
	println("ptrval", pm["a"].n)
	// map of slices sharing backing arrays
	ms := map[string][]int{}
	base := make([]int, 0, 10)
	ms["x"] = append(base, 1)
	ms["y"] = append(base, 2)
	println("sliceshare", ms["x"][0], ms["y"][0])
	ms["z"] = append(ms["z"], 5, 6, 7)
	println("appendmissing", len(ms["z"]), ms["z"][2])
	// map of maps
	mm := map[string]map[string]int{}
	for i := 0; i < 50; i++ {
		outer := "o" + strconv.Itoa(i%7)
		if mm[outer] == nil {
			mm[outer] = map[string]int{}
		}
		mm[outer]["i"+strconv.Itoa(i%3)] += i
	}
	outers := slices.Sorted(maps.Keys(mm))
	for _, o := range outers {
		inners := slices.Sorted(maps.Keys(mm[o]))
		print(o, ":")
		for _, in := range inners {
			print(" ", in, "=", mm[o][in])
		}
		println()
	}
	// alias of maps
	alias := mm["o1"]
	alias["new"] = 1
	println("alias", len(mm["o1"]), mm["o1"]["new"])
	// big map
	big := make(map[int]int)
	for i := 0; i < 100000; i++ {
		big[i*7919%100003] = i
	}
	s := 0
	for k, v := range big {
		s += k ^ v
	}
	println("big", len(big), s)
	for i := 0; i < 100000; i += 2 {
		delete(big, i*7919%100003)
	}
	s = 0
	for k := range big {
		s += k
	}
	println("bighalf", len(big), s)
	// string keys big
	sm := map[string]int{}
	for i := 0; i < 20000; i++ {
		sm[strconv.Itoa(i)] = i
	}
	hit := 0
	for i := 0; i < 40000; i += 3 {
		if _, ok := sm[strconv.Itoa(i)]; ok {
			hit++
		}
	}
	println("strbig", len(sm), hit)
	// maps.Clone / Equal / DeleteFunc / Copy
	c := maps.Clone(sm)
	println("clone", len(c), maps.Equal(c, sm))
	maps.DeleteFunc(c, func(k string, v int) bool { return v%2 == 1 })
	println("deletefunc", len(c), maps.Equal(c, sm))
	maps.Copy(c, map[string]int{"x": -1, "0": 1000})
	println("copy", len(c), c["0"], c["x"])
	var nilc map[string]int
	println("clonenil", maps.Clone(nilc) == nil)
	// compound assignment on map elements
	cnt := map[rune]int{}
	for _, r := range "mississippi\xff\xfe" {
		cnt[r]++
	}
	ks := slices.Sorted(maps.Keys(cnt))
	for _, k := range ks {
		print(k, "=", cnt[k], " ")
	}
	println()
	// len after many delete/insert cycles
	cy := map[int]int{}
	for round := 0; round < 20; round++ {
		for i := 0; i < 500; i++ {
			cy[i+round] = i
		}
		for i := 0; i < 500; i += 3 {
			delete(cy, i+round)
		}
	}
	println("cycles", len(cy))
}
