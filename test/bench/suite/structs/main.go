// Structs: field access through values and pointers, value and pointer
// receivers, &s[i] in a loop, struct copies.
package main

import "graalgo/bench/h"

type vec struct{ x, y, z int }

func (v vec) dot(w vec) int { return v.x*w.x + v.y*w.y + v.z*w.z }
func (v vec) add(w vec) vec { return vec{v.x + w.x, v.y + w.y, v.z + w.z} }
func (v *vec) scale(k int)  { v.x *= k; v.y *= k; v.z *= k }
func (v *vec) clamp(lim int) {
	if v.x > lim || v.x < -lim {
		v.x %= lim
	}
}

type particle struct {
	pos, vel vec
	mass     int
}

func step(ps []particle) {
	for i := range ps {
		p := &ps[i]
		p.pos = p.pos.add(p.vel)
		p.pos.clamp(1000)
		if p.pos.x > 900 || p.pos.x < -900 {
			p.vel.scale(-1)
		}
	}
}

func energy(ps []particle) int {
	e := 0
	for _, p := range ps {
		e += p.mass * p.vel.dot(p.vel)
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

func main() { h.Run("structs", 30, work) }
