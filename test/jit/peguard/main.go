// PE regression guard program (lane T0.B): each function below exercises
// one node family on a hot path. test/jit/run_pe_guard.sh runs it with
// eager compilation and fails if any of them is not compiled or any
// compilation fails (e.g. a PermanentBailoutException from partial
// evaluation wandering into JDK code).
package main

import (
	"fmt"
	"strings"
)

type Shape interface{ Area() int }

type Sq struct{ s int }
type Rect struct{ w, h int }

func (q Sq) Area() int    { return q.s * q.s }
func (r *Rect) Area() int { return r.w * r.h }

type point struct{ x, y int }

// guardCall: direct recursive calls.
func guardCall(n int) int {
	if n < 2 {
		return n
	}
	return guardCall(n-1) + guardCall(n-2)
}

// guardLoop: nested loops, arithmetic, switch.
func guardLoop(n int) int {
	s := 0
	for i := 0; i < n; i++ {
		switch i & 3 {
		case 0:
			s += i
		case 1:
			s -= i >> 1
		default:
			s ^= i
		}
	}
	return s
}

// guardIface: monomorphic + bimorphic interface dispatch.
func guardIface(shapes []Shape) int {
	t := 0
	for _, s := range shapes {
		t += s.Area()
	}
	return t
}

// guardStruct: struct field reads/writes through pointers, composite literals.
func guardStruct(ps []point) int {
	for i := range ps {
		p := &ps[i]
		p.x += p.y
		ps[i] = point{p.x, p.y + 1}
	}
	return ps[len(ps)-1].x
}

// guardAppend: append, slices, strings.Builder.
func guardAppend(n int) int {
	var b strings.Builder
	var xs []int
	for i := 0; i < n; i++ {
		xs = append(xs, i)
		b.WriteByte(byte('a' + i%26))
	}
	return len(xs) + len(b.String())
}

// guardMap: map assign/lookup.
func guardMap(n int) int {
	m := make(map[int]int)
	for i := 0; i < n; i++ {
		m[i&255] += i
	}
	return len(m) + m[7]
}

// guardDefer: defer on a hot path.
func guardDefer(x int) (r int) {
	defer func() { r += x }()
	return x * 2
}

func main() {
	shapes := []Shape{Sq{2}, &Rect{2, 3}, Sq{4}}
	ps := make([]point, 64)
	total := 0
	for round := 0; round < 3000; round++ {
		total += guardCall(10)
		total += guardLoop(200)
		total += guardIface(shapes)
		total += guardStruct(ps)
		total += guardAppend(40)
		total += guardMap(40)
		total += guardDefer(round)
	}
	fmt.Println(total)
}
