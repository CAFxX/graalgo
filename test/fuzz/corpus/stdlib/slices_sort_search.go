// Targets slices.Sort / SortFunc / IsSorted / BinarySearch / BinarySearchFunc
// / Min / Max / MinFunc / MaxFunc on ints, unsigned, floats with NaN and -0,
// strings and named types; generic instantiation across many element kinds
// and Min/Max panics on empty slices.
package main

import (
	"cmp"
	"math"
	"slices"
	"strings"
)

type celsius float32
type id uint16

func try(name string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(interface{ Error() string }); ok {
				println(name, "panic:", e.Error())
			} else if s, ok := r.(string); ok {
				println(name, "panic:", s)
			} else {
				println(name, "panic: other")
			}
		}
	}()
	f()
}

func hashOf[T cmp.Ordered](a []T, key func(T) uint64) uint64 {
	var h uint64 = 14695981039346656037
	for _, v := range a {
		h = (h ^ key(v)) * 1099511628211
	}
	return h
}

func run[T cmp.Ordered](name string, a []T, key func(T) uint64, probes []T) {
	b := slices.Clone(a)
	slices.Sort(b)
	c := slices.Clone(a)
	slices.SortFunc(c, func(x, y T) int { return cmp.Compare(y, x) })
	print(name, " ", hashOf(b, key), " ", hashOf(c, key), " ", slices.IsSorted(b), " ", slices.IsSorted(a), " ")
	for _, p := range probes {
		i, ok := slices.BinarySearch(b, p)
		j, ok2 := slices.BinarySearchFunc(c, p, func(e, t T) int { return cmp.Compare(t, e) })
		print(i, ok, "/", j, ok2, " ")
	}
	println()
	if len(a) > 0 {
		println(name, "minmax", key(slices.Min(a)), key(slices.Max(a)), slices.Index(b, slices.Min(a)), slices.Index(b, slices.Max(a)))
	}
}

func main() {
	var r uint64 = 7
	nx := func() uint64 { r ^= r << 13; r ^= r >> 7; r ^= r << 17; return r }
	var i8 []int8
	var u32 []uint32
	var i64 []int64
	var str []string
	var cs []celsius
	var ids []id
	var up []uintptr
	for i := 0; i < 1500; i++ {
		v := nx()
		i8 = append(i8, int8(v))
		u32 = append(u32, uint32(v>>7))
		i64 = append(i64, int64(v))
		str = append(str, strings.Repeat("ab", int(v%4))+string(rune('a'+v>>9%26)))
		cs = append(cs, celsius(int64(v>>40)%1000)/10)
		ids = append(ids, id(v>>3))
		up = append(up, uintptr(v>>33))
	}
	run("i8", i8, func(v int8) uint64 { return uint64(v) }, []int8{-128, 0, 127, 5})
	run("u32", u32, func(v uint32) uint64 { return uint64(v) }, []uint32{0, u32[3], ^uint32(0)})
	run("i64", i64, func(v int64) uint64 { return uint64(v) }, []int64{math.MinInt64, i64[9], 0})
	run("str", str, func(v string) uint64 { return uint64(len(v))<<8 | uint64(v[len(v)-1]) }, []string{"", "a", "ababz", "zz"})
	run("cs", cs, func(v celsius) uint64 { return uint64(math.Float32bits(float32(v))) }, []celsius{-99.9, 0, 50, 1000})
	run("ids", ids, func(v id) uint64 { return uint64(v) }, []id{0, ids[100], 65535})
	run("up", up, func(v uintptr) uint64 { return uint64(v) }, []uintptr{0, up[2]})

	nan := math.NaN()
	negz := math.Copysign(0, -1)
	fl := []float64{2, nan, negz, 0, -1, math.Inf(-1), nan, 1, math.Inf(1)}
	fb := func(v float64) uint64 { return math.Float64bits(v) }
	run("f64", fl, func(v float64) uint64 {
		if v != v {
			return 1
		}
		return fb(v)
	}, []float64{nan, negz, 0, 1, math.Inf(1)})
	// Min/Max propagate NaN
	println(math.IsNaN(slices.Min(fl)), math.IsNaN(slices.Max(fl)))
	println(fb(slices.Min([]float64{0, negz})), fb(slices.Min([]float64{negz, 0})), fb(slices.Max([]float64{0, negz})), fb(slices.Max([]float64{negz, 0})))
	println(fb(min(0, negz)), fb(max(negz, 0)), math.IsNaN(min(1, nan)), math.IsNaN(max(nan, 1)))
	println(math.IsNaN(slices.Max([]float64{1, 2, nan})), slices.Max([]float64{1, 3, 2}) == 3)
	// MinFunc/MaxFunc return first minimal / first maximal
	type kv struct {
		k string
		v int
	}
	kvs := []kv{{"a", 3}, {"b", 1}, {"c", 3}, {"d", 1}, {"e", 2}}
	byV := func(x, y kv) int { return cmp.Compare(x.v, y.v) }
	println(slices.MinFunc(kvs, byV).k, slices.MaxFunc(kvs, byV).k)
	// BinarySearchFunc with a different target type
	sorted := []kv{{"a", 1}, {"b", 3}, {"c", 3}, {"d", 7}}
	for _, t := range []int{0, 1, 3, 4, 7, 8} {
		i, ok := slices.BinarySearchFunc(sorted, t, func(e kv, t int) int { return cmp.Compare(e.v, t) })
		print(i, ok, " ")
	}
	println()
	try("min-empty", func() { slices.Min([]int{}) })
	try("max-empty", func() { slices.Max([]string(nil)) })
	try("minfunc-empty", func() { slices.MinFunc([]kv{}, byV) })
	try("maxfunc-empty", func() { slices.MaxFunc([]kv{}, byV) })
	// sorted NaN slice: BinarySearch finds NaN at 0
	sf := []float64{nan, nan, -1, 0, 3}
	println(slices.IsSorted(sf))
	i, ok := slices.BinarySearch(sf, nan)
	println(i, ok)
	i, ok = slices.BinarySearch(sf, 0)
	println(i, ok)
	println(slices.IsSorted([]float64{1, nan}), slices.IsSorted([]float64{nan, 1}))
}
