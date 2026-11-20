// atomic.Value / atomic.Pointer / sync.Map compare-and-swap over every kind
// of interface payload: floats (NaN never ==, -0 == +0), structs, arrays,
// strings built at run time, pointers; plus the inconsistent-type and
// uncomparable-old panics.
package main

import (
	"math"
	"strconv"
	"sync"
	"sync/atomic"
)

type pt struct {
	x, y float64
}

type arr [3]int8

func try(name string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			switch v := r.(type) {
			case string:
				println(name, "panic string:", v)
			case interface{ Error() string }:
				println(name, "panic error:", v.Error())
			default:
				println(name, "panic other")
			}
		}
	}()
	f()
}

func main() {
	nan := math.NaN()
	negz := math.Copysign(0, -1)

	var v atomic.Value
	v.Store(nan)
	println("cas nan->1", v.CompareAndSwap(nan, 1.0))
	println("swap nan", math.IsNaN(v.Swap(2.0).(float64)))
	v.Store(negz)
	println("cas +0 on -0", v.CompareAndSwap(0.0, 3.0), v.Load().(float64))
	v.Store(0.0)
	println("cas -0 on +0", v.CompareAndSwap(negz, negz))
	println("signbit after", math.Signbit(v.Load().(float64)))

	var vs atomic.Value
	s1 := strconv.Itoa(12345)
	vs.Store(s1)
	s2 := "123" + strconv.Itoa(45)
	println("cas runtime string", vs.CompareAndSwap(s2, "x"+s1), vs.Load().(string))
	println("cas stale", vs.CompareAndSwap(s1, "y"))

	var vp atomic.Value
	vp.Store(pt{1, nan})
	println("cas struct with nan", vp.CompareAndSwap(pt{1, nan}, pt{2, 2}))
	vp.Store(pt{negz, 1})
	println("cas struct -0", vp.CompareAndSwap(pt{0, 1}, pt{5, 6}))
	println("struct now", vp.Load().(pt).x, vp.Load().(pt).y)

	var va atomic.Value
	va.Store(arr{1, 2, 3})
	println("cas array", va.CompareAndSwap(arr{1, 2, 3}, arr{4, 5, 6}))
	println("cas array stale", va.CompareAndSwap(arr{1, 2, 3}, arr{7, 7, 7}))
	a := va.Load().(arr)
	a[0] = 100
	println("loaded copy", va.Load().(arr)[0], a[0])

	try("inconsistent", func() { va.CompareAndSwap(arr{4, 5, 6}, [3]int8{}) })
	try("store inconsistent", func() { va.Store("s") })
	try("store nil", func() { va.Store(nil) })
	try("cas nil new", func() { va.CompareAndSwap(arr{4, 5, 6}, nil) })

	var vsl atomic.Value
	vsl.Store([]int{1})
	try("cas uncomparable", func() { vsl.CompareAndSwap([]int{1}, []int{2}) })

	var empty atomic.Value
	println("empty load nil", empty.Load() == nil)
	println("empty cas nil old", empty.CompareAndSwap(nil, 7))
	println("empty now", empty.Load().(int))

	var ap atomic.Pointer[pt]
	p1 := &pt{1, 2}
	p2 := &pt{1, 2}
	ap.Store(p1)
	println("ptr cas equal-content", ap.CompareAndSwap(p2, p2))
	println("ptr cas same", ap.CompareAndSwap(p1, p2))
	println("ptr old", ap.Swap(nil) == p2, ap.Load() == nil)

	var m sync.Map
	m.Store("k", nan)
	println("map cas nan", m.CompareAndSwap("k", nan, 1.0))
	println("map cad nan", m.CompareAndDelete("k", nan))
	m.Store("z", negz)
	println("map cas +0", m.CompareAndSwap("z", 0.0, 9.0))
	got, _ := m.Load("z")
	println("map z", got.(float64))
	m.Store(nan, 1)
	m.Store(nan, 2)
	n := 0
	m.Range(func(k, v any) bool { n++; return true })
	println("map entries with nan keys", n)
	_, ok := m.Load(nan)
	println("nan key loadable", ok)
	m.Store(arr{1}, "a")
	m.Store(pt{negz, 0}, "p")
	x, ok := m.Load(pt{0, 0})
	println("struct key -0 lookup", ok, x.(string))
	old, loaded := m.Swap(arr{1}, "b")
	println("swap arr key", loaded, old.(string))
	try("map unhashable", func() { m.Store([]int{1}, 1) })
	try("map cas uncomparable", func() { m.CompareAndSwap(arr{1}, []int{}, 1) })
	m.Store("sl", []int{1})
	try("map cas uncomparable stored", func() { m.CompareAndSwap("sl", 1, 2) })
	v2, ok := m.LoadAndDelete(arr{1})
	println("lad", ok, v2.(string))
	m.Clear()
	n = 0
	m.Range(func(k, v any) bool { n++; return true })
	println("after clear", n)
}
