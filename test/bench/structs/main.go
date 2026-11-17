// Benchmark: struct field reads/writes via values and pointers.
package main

import (
	"fmt"
	"os"
	"time"
)

type vec struct{ x, y, z int }

type particle struct {
	pos, vel vec
	mass     int
}

func step(ps []particle) {
	for i := range ps {
		p := &ps[i]
		p.pos.x += p.vel.x
		p.pos.y += p.vel.y
		p.pos.z += p.vel.z
		if p.pos.x > 1000 || p.pos.x < -1000 {
			p.vel.x = -p.vel.x
		}
	}
}

func energy(ps []particle) int {
	e := 0
	for _, p := range ps {
		e += p.mass * (p.vel.x*p.vel.x + p.vel.y*p.vel.y + p.vel.z*p.vel.z)
	}
	return e
}

func work() int {
	ps := make([]particle, 1000)
	for i := range ps {
		ps[i] = particle{vec{i, i * 2, i * 3}, vec{i&7 - 3, i&3 - 1, 1}, i&15 + 1}
	}
	for k := 0; k < 300; k++ {
		step(ps)
	}
	return energy(ps) + ps[17].pos.x
}

func main() {
	var r int
	best := time.Duration(1 << 62)
	for i := 0; i < 15; i++ {
		t0 := time.Now()
		r = work()
		if d := time.Since(t0); d < best {
			best = d
		}
	}
	fmt.Println(r)
	fmt.Fprintf(os.Stderr, "BENCH structs %d\n", best.Nanoseconds())
}
