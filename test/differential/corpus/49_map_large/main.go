package main

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"sort"
)

// check exercises a map through every iteration path that must agree:
// native range (GoMap lowering), reflect MapRange (the header view over
// real reflect/map.go), maps.Keys/Values, and maps.Clone/Equal. All
// output is sorted or order-independent, so the program is deterministic.
//
// r64 (C2): no test anywhere exercised a >8-entry map through the
// header view. A wrong directory image would yield silently wrong
// iteration here (missing or duplicate entries), caught by the
// count/sum/key checks — not by a loud failure.
func check(label string, m map[string]int) {
	// Native range.
	nativeCount, nativeSum := 0, 0
	for _, v := range m {
		nativeCount++
		nativeSum += v
	}
	fmt.Println(label, "native", nativeCount, nativeSum)

	// Reflect MapRange — the header-view path.
	mv := reflect.ValueOf(m)
	it := mv.MapRange()
	keys := []string{}
	rangeSum := 0
	for it.Next() {
		keys = append(keys, it.Key().String())
		rangeSum += int(it.Value().Int())
	}
	sort.Strings(keys)
	fmt.Println(label, "maprange", len(keys), rangeSum, keys)

	// maps.Keys / maps.Values, sorted for determinism.
	fmt.Println(label, "keys", slices.Sorted(maps.Keys(m)))
	fmt.Println(label, "values", slices.Sorted(maps.Values(m)))

	// Clone + Equal, then diverge the clone.
	c := maps.Clone(m)
	fmt.Println(label, "clone-equal", maps.Equal(m, c), len(c))
	c["zz"] = 999
	fmt.Println(label, "clone-diverged", maps.Equal(m, c))

	// NOTE (r64): MapIndex is deliberately not exercised here:
	// reflect.MapIndex on string keys needs the mapaccess_faststr
	// linkname, which graalgo has never bound (fails identically on
	// a 3-entry map — a pre-existing gap, not a directory-path
	// defect). It belongs to its own follow-up lane, not to this
	// C2 probe, whose surface is MapRange + Keys/Values + Clone/Equal.
}

func main() {
	// 9 entries: just past a single bucket (8 slots) — the directory path.
	m9 := map[string]int{}
	for i := 0; i < 9; i++ {
		m9[fmt.Sprintf("k%02d", i)] = (i + 1) * 7
	}
	check("m9", m9)

	// 20 entries: past 17, forcing 2+ tables / dirLen 4 in the header view.
	m20 := map[string]int{}
	for i := 0; i < 20; i++ {
		m20[fmt.Sprintf("k%02d", i)] = (i + 1) * 7
	}
	check("m20", m20)
}
