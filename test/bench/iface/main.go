// Benchmark: interface method dispatch (monomorphic and bimorphic sites).
package main

import (
	"fmt"
	"os"
	"time"
)

type Shape interface{ Area() int }

type Sq struct{ s int }
type Rect struct{ w, h int }

func (q Sq) Area() int    { return q.s * q.s }
func (r *Rect) Area() int { return r.w * r.h }

func sum(shapes []Shape, rounds int) int {
	t := 0
	for k := 0; k < rounds; k++ {
		for _, s := range shapes {
			t += s.Area()
		}
	}
	return t
}

func main() {
	mono := make([]Shape, 1000)
	bi := make([]Shape, 1000)
	for i := range mono {
		mono[i] = Sq{i & 15}
		if i&1 == 0 {
			bi[i] = Sq{i & 7}
		} else {
			bi[i] = &Rect{i & 3, i & 5}
		}
	}
	var r int
	best := time.Duration(1 << 62)
	for i := 0; i < 15; i++ {
		t0 := time.Now()
		r = sum(mono, 300) + sum(bi, 300)
		if d := time.Since(t0); d < best {
			best = d
		}
	}
	fmt.Println(r)
	fmt.Fprintf(os.Stderr, "BENCH iface %d\n", best.Nanoseconds())
}
