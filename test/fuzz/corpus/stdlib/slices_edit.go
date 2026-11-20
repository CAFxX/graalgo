// Targets slices editing functions: Insert/Delete/DeleteFunc/Replace incl.
// out-of-range panics (recovered messages), Compact/CompactFunc (zeroing of
// the tail), Grow (negative panic, capacity), Clip, Index/IndexFunc,
// Contains/ContainsFunc, Reverse, Equal/EqualFunc with NaN, Concat, Repeat.
package main

import (
	"math"
	"slices"
	"strings"
)

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
	println(name, "ok")
}

func show(name string, a []int) {
	print(name, " len=", len(a), " [")
	for _, v := range a {
		print(" ", v)
	}
	println(" ]")
}

func main() {
	base := []int{0, 1, 2, 3, 4, 5}
	show("ins0", slices.Insert(slices.Clone(base), 0, -1, -2))
	show("ins3", slices.Insert(slices.Clone(base), 3, 9))
	show("insEnd", slices.Insert(slices.Clone(base), 6, 7, 8))
	show("insNone", slices.Insert(slices.Clone(base), 2))
	// Insert into a slice with spare capacity that overlaps
	g := make([]int, 4, 10)
	copy(g, []int{1, 2, 3, 4})
	g2 := slices.Insert(g, 1, g[2:4]...)
	show("insOverlap", g2)
	try("ins-oob", func() { slices.Insert([]int{1, 2}, 3, 5) })
	try("ins-neg", func() { slices.Insert([]int{1, 2}, -1, 5) })
	try("ins-oob-empty", func() { slices.Insert([]int{1, 2}, 3) })

	d := slices.Clone(base)
	d2 := slices.Delete(d, 1, 3)
	show("del", d2)
	show("delTail", d) // tail zeroed
	show("del0", slices.Delete(slices.Clone(base), 2, 2))
	try("del-rev", func() { slices.Delete(slices.Clone(base), 4, 2) })
	try("del-oob", func() { slices.Delete(slices.Clone(base), 1, 7) })
	try("del-neg", func() { slices.Delete(slices.Clone(base), -1, 2) })
	dd := slices.Clone(base)
	dd2 := slices.DeleteFunc(dd, func(v int) bool { return v%2 == 1 })
	show("delf", dd2)
	show("delfTail", dd)

	show("rep", slices.Replace(slices.Clone(base), 1, 3, 7, 8, 9, 10))
	show("repShrink", slices.Replace(slices.Clone(base), 0, 5, 42))
	rr := slices.Clone(base)
	rr2 := slices.Replace(rr, 1, 4, 99)
	show("repShrink2", rr2)
	show("repTail", rr)
	try("rep-oob", func() { slices.Replace(slices.Clone(base), 2, 9, 1) })
	try("rep-rev", func() { slices.Replace(slices.Clone(base), 3, 2) })

	c := []int{1, 1, 2, 2, 2, 3, 1, 1, 4}
	c2 := slices.Compact(c)
	show("compact", c2)
	show("compactTail", c)
	show("compactNil", slices.Compact([]int(nil)))
	ss := []string{"a", "A", "b", "B", "b", "c"}
	ss2 := slices.CompactFunc(ss, strings.EqualFold)
	println(strings.Join(ss2, ","), len(ss2), "[", strings.Join(ss, "|"), "]")

	gr := []int{1, 2}
	gr = slices.Grow(gr, 10)
	println("grow", len(gr), cap(gr) >= 12)
	gr = slices.Clip(gr)
	println("clip", len(gr), cap(gr))
	try("grow-neg", func() { slices.Grow([]int{1}, -1) })
	var gn []int
	gn = slices.Grow(gn, 0)
	println("grow0 nil", gn == nil)

	println(slices.Index(base, 3), slices.Index(base, 9), slices.IndexFunc(base, func(v int) bool { return v > 3 }))
	println(slices.Contains(base, 5), slices.Contains(base, -1), slices.ContainsFunc(base, func(v int) bool { return v < 0 }))
	nan := math.NaN()
	fl := []float64{1, nan, 3}
	println(slices.Index(fl, nan), slices.Contains(fl, nan), slices.IndexFunc(fl, func(v float64) bool { return v != v }))
	println(slices.Equal(fl, fl), slices.Equal([]float64{0}, []float64{math.Copysign(0, -1)}))
	println(slices.EqualFunc(fl, fl, func(a, b float64) bool { return a == b || (a != a && b != b) }))
	println(slices.Equal([]int(nil), []int{}), slices.Equal([]int{1}, []int{1, 2}))
	println(slices.Compare([]int{1, 2}, []int{1, 2, 0}), slices.Compare([]int{2}, []int{1, 9}), slices.Compare([]float64{nan}, []float64{0}))
	println(slices.Compare([]float64{nan}, []float64{nan}), slices.Compare([]string{"a"}, []string{"a"}))

	rv := slices.Clone(base)
	slices.Reverse(rv)
	show("rev", rv)
	rs := []string{"x"}
	slices.Reverse(rs)
	slices.Reverse([]int(nil))

	show("concat", slices.Concat([]int{1}, nil, []int{2, 3}, []int{}, base))
	cc := slices.Concat[[]int]()
	println("concat-none", cc == nil, len(cc))
	show("repeat", slices.Repeat([]int{1, 2}, 3))
	show("repeat0", slices.Repeat([]int{1, 2}, 0))
	try("repeat-neg", func() { slices.Repeat([]int{1}, -1) })
	try("repeat-ovf", func() { slices.Repeat([]int{1, 2}, math.MaxInt/2+1) })

	// Insert grows through many reallocations
	var acc []int
	for i := 0; i < 300; i++ {
		acc = slices.Insert(acc, i/2, i)
	}
	var h uint64 = 14695981039346656037
	for _, v := range acc {
		h = (h ^ uint64(v)) * 1099511628211
	}
	for len(acc) > 3 {
		acc = slices.Delete(acc, len(acc)/3, len(acc)/3+2)
		h = (h ^ uint64(acc[len(acc)/2])) * 1099511628211
	}
	println("acc", h, len(acc))
}
