// map[any]V whose keys are interfaces holding floats, complex numbers,
// arrays and structs with NaN / -0 parts; delete, clear, maps.Clone,
// delete-during-range and unhashable keys hidden inside arrays/structs.
package main

import (
	"maps"
	"math"
	"slices"
	"strconv"
)

type fk struct {
	a float32
	b int16
}

type wrap struct {
	v any
}

func try(name string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			println(name, "->", r.(interface{ Error() string }).Error())
		}
	}()
	f()
}

func sortedVals(m map[any]int) string {
	var vs []int
	for _, v := range m {
		vs = append(vs, v)
	}
	slices.Sort(vs)
	s := ""
	for _, v := range vs {
		s += strconv.Itoa(v) + ","
	}
	return s
}

func main() {
	nan := math.NaN()
	negz := math.Copysign(0, -1)
	m := map[any]int{}
	m[nan] = 1
	m[nan] = 2
	m[float32(nan)] = 3
	m[complex(nan, 0)] = 4
	m[complex(0, negz)] = 5
	m[complex(0, 0)] = 6
	m[[2]float64{nan, 1}] = 7
	m[[2]float64{nan, 1}] = 8
	m[fk{float32(negz), 1}] = 9
	m[fk{0, 1}] = 10
	m[0.0] = 11
	m[negz] = 12
	m[0] = 13
	m[int64(0)] = 14
	println("len", len(m), sortedVals(m))
	_, ok := m[nan]
	println("nan lookup", ok)
	println("complex +0 lookup", m[complex(0, 0)], m[complex(negz, negz)])
	println("struct lookup", m[fk{0, 1}])
	println("float lookup", m[0.0], m[negz], m[0], m[int64(0)])

	var zsign, csign bool
	for k := range m {
		if f, isF := k.(float64); isF && f == 0 {
			zsign = math.Signbit(f)
		}
		if c, isC := k.(complex128); isC && c == 0 {
			csign = math.Signbit(imag(c))
		}
	}
	println("zero key signbit", zsign, "complex zero key imag signbit", csign)

	c := maps.Clone(m)
	println("clone len", len(c), sortedVals(c))
	delete(c, nan)
	println("clone delete nan len", len(c))

	deleted := 0
	for k := range c {
		if _, isA := k.([2]float64); isA {
			delete(c, k)
			deleted++
		}
	}
	println("range-delete arrays", deleted, len(c))

	n := 0
	for k, v := range m {
		if v%2 == 0 {
			delete(m, k)
			n++
		}
	}
	println("deleted even", n, "left", len(m), sortedVals(m))

	clear(m)
	println("after clear", len(m))
	m[nan] = 1
	clear(m)
	println("after clear nan", len(m))

	try("array of slice key", func() { m[[1]any{[]int{1}}] = 1 })
	try("struct with map key", func() { m[wrap{map[int]int{}}] = 1 })
	try("lookup unhashable", func() { _ = m[[]byte("x")] })
	try("delete unhashable", func() { delete(m, func() {}) })
	var nilm map[any]int
	try("nil map lookup unhashable", func() { _ = nilm[wrap{[]int{}}] })
	println("nil map len", len(nilm), nilm[nan])
	delete(nilm, nan)

	pk := map[[2]any]string{}
	pk[[2]any{1, "a"}] = "x"
	pk[[2]any{1, "a"}] = "y"
	pk[[2]any{int8(1), "a"}] = "z"
	pk[[2]any{nan, nil}] = "n1"
	pk[[2]any{nan, nil}] = "n2"
	pk[[2]any{nil, nil}] = "nil"
	println("[2]any len", len(pk), pk[[2]any{1, "a"}], pk[[2]any{nil, nil}])
	b := []byte("hello")
	pk[[2]any{string(b), 2}] = "h"
	b[0] = 'j'
	println("string key copied", pk[[2]any{"hello", 2}], pk[[2]any{"jello", 2}] == "")
	try("[2]any unhashable", func() { pk[[2]any{1, []int{}}] = "" })
}
