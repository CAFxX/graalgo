// Interface dispatch at a site that sees 1 dynamic type(s) (mono).
package main

import "graalgo/bench/h"

type Shape interface{ Area() int }

type t0 struct{ a int }
type t1 struct{ a, b int }
type t2 struct{ a int }
type t3 struct{ a int }
type t4 struct{ a int }
type t5 struct{ a int }
type t6 struct{ a int }
type t7 struct{ a int }

func (s t0) Area() int  { return s.a * s.a }
func (s *t1) Area() int { return s.a * s.b }
func (s t2) Area() int  { return s.a + 2 }
func (s t3) Area() int  { return s.a ^ 3 }
func (s t4) Area() int  { return s.a << 1 }
func (s t5) Area() int  { return s.a - 5 }
func (s t6) Area() int  { return s.a & 6 }
func (s *t7) Area() int { return s.a | 7 }

func mk(i, kinds int) Shape {
	a := i & 15
	switch i % kinds {
	case 0:
		return t0{a}
	case 1:
		return &t1{a, i & 3}
	case 2:
		return t2{a}
	case 3:
		return t3{a}
	case 4:
		return t4{a}
	case 5:
		return t5{a}
	case 6:
		return t6{a}
	default:
		return &t7{a}
	}
}

func sum(shapes []Shape) int {
	t := 0
	for k := 0; k < 300; k++ {
		for _, s := range shapes {
			t += s.Area()
		}
	}
	return t
}

func main() {
	shapes := make([]Shape, 1000)
	for i := range shapes {
		shapes[i] = mk(i, 1)
	}
	h.Run("ifacemono", 30, func() int { return sum(shapes) })
}
