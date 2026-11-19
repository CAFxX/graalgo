// Targets the maps package on several key kinds: Clone (nil stays nil),
// Copy (overwrite), DeleteFunc, Equal/EqualFunc incl. NaN values and NaN
// keys, Insert from a Seq2, Collect, Keys/Values; output is always sorted.
// Also exercises clear() and builtin map semantics with NaN and -0 keys.
package main

import (
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
)

type pt struct{ x, y int16 }

func dumpSI(name string, m map[string]int) {
	ks := slices.Sorted(maps.Keys(m))
	var sb strings.Builder
	for _, k := range ks {
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(strconv.Itoa(m[k]))
		sb.WriteByte(' ')
	}
	println(name, len(m), sb.String())
}

func main() {
	m := map[string]int{"a": 1, "b": 2, "c": 3, "d": 4}
	c := maps.Clone(m)
	c["e"] = 5
	dumpSI("orig", m)
	dumpSI("clone", c)
	var nm map[string]int
	nc := maps.Clone(nm)
	println("clone-nil", nc == nil, len(nc))
	maps.Copy(m, map[string]int{"a": 10, "z": 26})
	dumpSI("copy", m)
	maps.DeleteFunc(m, func(k string, v int) bool { return v%2 == 0 })
	dumpSI("delf", m)
	println(maps.Equal(m, map[string]int{"a": 10, "c": 3}), maps.Equal(m, c))
	println(maps.Equal(map[string]int{}, nm), maps.Equal(nm, nm))
	println(maps.EqualFunc(m, map[string]string{"a": "10", "c": "3"}, func(v int, s string) bool { return strconv.Itoa(v) == s }))

	nan := math.NaN()
	fm := map[string]float64{"x": nan}
	println("nan-val-equal", maps.Equal(fm, fm), maps.EqualFunc(fm, fm, func(a, b float64) bool { return a != a && b != b }))
	nk := map[float64]int{}
	nk[nan] = 1
	nk[nan] = 2
	nk[0] = 3
	nk[math.Copysign(0, -1)] = 4
	nk[1] = 5
	println("nan-keys", len(nk), nk[nan], nk[0])
	nkc := maps.Clone(nk)
	println("nan-clone", len(nkc), maps.Equal(nk, nkc))
	sum := 0
	nanCount := 0
	for k, v := range nk {
		if k != k {
			nanCount++
		}
		sum += v
	}
	println("nan-iter", nanCount, sum)
	delete(nk, nan)
	println("nan-del", len(nk))
	clear(nk)
	println("cleared", len(nk))

	pm := map[pt][]int{}
	for i := 0; i < 500; i++ {
		k := pt{int16(i % 13), int16(i % 7)}
		pm[k] = append(pm[k], i)
	}
	pks := slices.SortedFunc(maps.Keys(pm), func(a, b pt) int {
		if a.x != b.x {
			return int(a.x) - int(b.x)
		}
		return int(a.y) - int(b.y)
	})
	var h uint64 = 14695981039346656037
	for _, k := range pks {
		for _, v := range pm[k] {
			h = (h ^ uint64(v)) * 1099511628211
		}
		h = (h ^ uint64(k.x)<<16 ^ uint64(k.y)) * 1099511628211
	}
	println("pm", len(pm), h, pks[0].x, pks[0].y, len(pm[pks[5]]))
	pc := maps.Clone(pm)
	pc[pks[0]][0] = -1 // shallow: shares the slice
	println("shallow", pm[pks[0]][0])

	ins := map[int]string{}
	maps.Insert(ins, slices.All([]string{"zero", "one", "two"}))
	maps.Insert(ins, maps.All(map[int]string{1: "uno", 7: "siete"}))
	ik := slices.Sorted(maps.Keys(ins))
	for _, k := range ik {
		print(k, ":", ins[k], " ")
	}
	println()
	col := maps.Collect(maps.All(ins))
	println(maps.Equal(col, ins), len(col))
	vs := slices.Sorted(maps.Values(ins))
	println(strings.Join(vs, ","))

	// large map grow/shrink checksum
	big := map[uint32]uint32{}
	var s uint32 = 1
	for i := 0; i < 20000; i++ {
		s = s*1103515245 + 12345
		big[s%5000] += s >> 16
	}
	maps.DeleteFunc(big, func(k, v uint32) bool { return k%3 == 0 })
	bk := slices.Sorted(maps.Keys(big))
	h = 14695981039346656037
	for _, k := range bk {
		h = (h ^ uint64(k)<<32 ^ uint64(big[k])) * 1099511628211
	}
	println("big", len(big), h)
	bc := maps.Clone(big)
	bc[bk[0]]++
	println(maps.Equal(big, bc), maps.EqualFunc(big, bc, func(a, b uint32) bool { return a/4 == b/4 || true }))

	// interface keys
	im := map[any]int{1: 1, "1": 2, 1.0: 3, int8(1): 4, true: 5, [2]int{1, 1}: 6}
	println("iface", len(im), im[1], im["1"], im[1.0], im[int8(1)], im[true], im[[2]int{1, 1}])
	ic := maps.Clone(im)
	println(maps.Equal(im, ic))
	maps.DeleteFunc(ic, func(k any, v int) bool { _, ok := k.(int); return ok })
	println(len(ic), ic[1])
}
