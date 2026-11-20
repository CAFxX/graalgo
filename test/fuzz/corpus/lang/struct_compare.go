// Targets struct and array == with nested arrays, strings, interfaces,
// floats (NaN, -0), blank fields (ignored in ==), zero-size types, struct
// conversion between identical underlying types, anonymous structs, and the
// "comparing uncomparable type" panic through interfaces.
package main

import "math"

type P struct {
	X, Y float64
}

type Q struct {
	X, Y float64
}

type Blank struct {
	A int
	_ int
	B string
	_ [2]float64
}

type Nested struct {
	Name string
	Pts  [3]P
	Any  any
	Ptr  *int
}

type Empty struct{}

type ZS struct {
	E Empty
	Z [0]int
}

type WithSlice struct {
	S []int
}

func cmpAny(a, b any) (res string) {
	defer func() {
		if r := recover(); r != nil {
			res = "panic: " + r.(interface{ Error() string }).Error()
		}
	}()
	if a == b {
		return "eq"
	}
	return "ne"
}

//go:noinline
func neg0() float64 { return math.Copysign(0, -1) }

//go:noinline
func nan() float64 { return math.NaN() }

func b2s(b bool) string {
	if b {
		return "T"
	}
	return "F"
}

func main() {
	p1 := P{1, 2}
	p2 := P{1, 2}
	println(b2s(p1 == p2), b2s(p1 != p2))
	pz := P{0, 0}
	pn := P{neg0(), 0}
	println(b2s(pz == pn), math.Signbit(pn.X))
	pnan := P{nan(), 0}
	println(b2s(pnan == pnan), b2s(pnan != pnan))
	cp := pnan
	println(b2s(cp == pnan))

	// conversion between identical underlying types
	q := Q(p1)
	println(b2s(P(q) == p1), q.X, q.Y)
	anon := struct{ X, Y float64 }{1, 2}
	println(b2s(anon == p1), b2s(Q(anon) == q))
	var pa = P(anon)
	pa.X = 9
	println(anon.X, pa.X)

	// blank fields ignored
	var b1, b2 Blank
	b1.A, b1.B = 3, "x"
	b2.A, b2.B = 3, "x"
	println(b2s(b1 == b2))
	b2.B = "y"
	println(b2s(b1 == b2))

	// nested with arrays, interfaces, pointers
	i1, i2 := 5, 5
	n1 := Nested{"n", [3]P{{1, 1}, {2, 2}, {3, 3}}, "str", &i1}
	n2 := n1
	println(b2s(n1 == n2))
	n2.Ptr = &i2
	println(b2s(n1 == n2), b2s(*n1.Ptr == *n2.Ptr))
	n2.Ptr = &i1
	n2.Pts[2].Y = neg0()
	n1.Pts[2].Y = 0
	println(b2s(n1 == n2))
	n2.Pts[2].Y = 3
	n1.Pts[2].Y = 3
	n2.Any = 7
	println(b2s(n1 == n2))
	n1.Any = int64(7)
	println(b2s(n1 == n2))
	n1.Any = 7
	println(b2s(n1 == n2))
	n1.Any = nan()
	n2.Any = n1.Any
	println(b2s(n1 == n2)) // NaN inside interface: not equal
	n1.Any = P{1, 2}
	n2.Any = P{1, 2}
	println(b2s(n1 == n2))
	n2.Any = Q{1, 2}
	println(b2s(n1 == n2))

	// uncomparable dynamic types inside interface in struct
	n1.Any = []int{1}
	n2.Any = []int{1}
	println(cmpAny(n1, n2))
	n2.Any = 3 // different dynamic types: no panic
	println(cmpAny(n1, n2))
	println(cmpAny(WithSlice{}, WithSlice{}))
	println(cmpAny(map[int]int{}, 1))
	println(cmpAny(func() {}, func() {}))
	println(cmpAny([2]any{1, []int{}}, [2]any{1, []int{}}))
	println(cmpAny([2]any{1, []int{}}, [2]any{2, []int{}}))
	println(cmpAny(struct{ A, B any }{1, map[int]int{}}, struct{ A, B any }{1, map[int]int{}}))
	println(cmpAny(struct{ A, B any }{2, map[int]int{}}, struct{ A, B any }{1, map[int]int{}}))

	// arrays of strings / floats
	s1 := [3]string{"a", "bb", "ccc"}
	s2 := s1
	println(b2s(s1 == s2))
	s2[2] = "cc" + "c"
	println(b2s(s1 == s2))
	s2[2] = "ccd"
	println(b2s(s1 == s2))
	f1 := [2]float32{float32(neg0()), 1}
	f2 := [2]float32{0, 1}
	println(b2s(f1 == f2))
	c1 := [1]complex128{complex(nan(), 0)}
	println(b2s(c1 == c1))

	// zero-size
	var z1, z2 ZS
	println(b2s(z1 == z2), b2s(Empty{} == Empty{}), cmpAny(Empty{}, ZS{}), cmpAny(Empty{}, Empty{}))
	var arr0a, arr0b [0]func()
	_ = arr0a
	_ = arr0b
	println(len(arr0a))

	// interface holding arrays/structs
	var ia, ib any = [3]int{1, 2, 3}, [3]int{1, 2, 3}
	println(b2s(ia == ib))
	ib = [3]int{1, 2, 4}
	println(b2s(ia == ib))
	ib = [3]int64{1, 2, 3}
	println(b2s(ia == ib))
	ia, ib = P{neg0(), 1}, P{0, 1}
	println(b2s(ia == ib))

	// map with struct keys incl. -0 and NaN
	m := map[P]int{}
	m[P{0, 0}] = 1
	m[P{neg0(), 0}]++
	m[P{nan(), 0}] = 5
	m[P{nan(), 0}] = 6
	println(len(m), m[P{0, 0}], m[P{nan(), 0}])
	mb := map[Blank]int{}
	mb[b1]++
	mb[Blank{A: 3, B: "x"}]++
	println(len(mb), mb[b1])
	ma := map[any]int{}
	ma[1]++
	ma[int8(1)]++
	ma[P{1, 2}]++
	ma[Q{1, 2}]++
	ma[[2]string{"a", "b"}]++
	ma[[2]string{"a", "b"}]++
	println(len(ma), ma[[2]string{"a", "b"}])
	println(mapPanic(ma))
}

func mapPanic(m map[any]int) (s string) {
	defer func() {
		if r := recover(); r != nil {
			s = "panic: " + r.(interface{ Error() string }).Error()
		}
	}()
	m[[]int{1}] = 1
	return "no panic"
}
