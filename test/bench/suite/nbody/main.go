// N-body (benchmarks game): float64 struct fields, math.Sqrt.
package main

import (
	"math"

	"graalgo/bench/h"
)

const (
	solarMass   = 4 * math.Pi * math.Pi
	daysPerYear = 365.24
)

type body struct{ x, y, z, vx, vy, vz, mass float64 }

func system() []body {
	bs := []body{
		{0, 0, 0, 0, 0, 0, solarMass},
		{4.84143144246472090e+00, -1.16032004402742839e+00, -1.03622044471123109e-01, 1.66007664274403694e-03 * daysPerYear, 7.69901118419740425e-03 * daysPerYear, -6.90460016972063023e-05 * daysPerYear, 9.54791938424326609e-04 * solarMass},
		{8.34336671824457987e+00, 4.12479856412430479e+00, -4.03523417114321381e-01, -2.76742510726862411e-03 * daysPerYear, 4.99852801234917238e-03 * daysPerYear, 2.30417297573763929e-05 * daysPerYear, 2.85885980666130812e-04 * solarMass},
		{1.28943695621391310e+01, -1.51111514016986312e+01, -2.23307578892655734e-01, 2.96460137564761618e-03 * daysPerYear, 2.37847173959480950e-03 * daysPerYear, -2.96589568540237556e-05 * daysPerYear, 4.36624404335156298e-05 * solarMass},
		{1.53796971148509165e+01, -2.59193146099879641e+01, 1.79258772950371181e-01, 2.68067772490389322e-03 * daysPerYear, 1.62824170038242295e-03 * daysPerYear, -9.51592254519715870e-05 * daysPerYear, 5.15138902046611451e-05 * solarMass},
	}
	var px, py, pz float64
	for _, b := range bs {
		px += b.vx * b.mass
		py += b.vy * b.mass
		pz += b.vz * b.mass
	}
	bs[0].vx, bs[0].vy, bs[0].vz = -px/solarMass, -py/solarMass, -pz/solarMass
	return bs
}

func advance(bs []body, dt float64) {
	for i := range bs {
		bi := &bs[i]
		for j := i + 1; j < len(bs); j++ {
			bj := &bs[j]
			dx, dy, dz := bi.x-bj.x, bi.y-bj.y, bi.z-bj.z
			d2 := dx*dx + dy*dy + dz*dz
			mag := dt / (d2 * math.Sqrt(d2))
			bi.vx -= dx * bj.mass * mag
			bi.vy -= dy * bj.mass * mag
			bi.vz -= dz * bj.mass * mag
			bj.vx += dx * bi.mass * mag
			bj.vy += dy * bi.mass * mag
			bj.vz += dz * bi.mass * mag
		}
	}
	for i := range bs {
		b := &bs[i]
		b.x += dt * b.vx
		b.y += dt * b.vy
		b.z += dt * b.vz
	}
}

func energy(bs []body) float64 {
	e := 0.0
	for i, b := range bs {
		e += 0.5 * b.mass * (b.vx*b.vx + b.vy*b.vy + b.vz*b.vz)
		for j := i + 1; j < len(bs); j++ {
			c := bs[j]
			dx, dy, dz := b.x-c.x, b.y-c.y, b.z-c.z
			e -= b.mass * c.mass / math.Sqrt(dx*dx+dy*dy+dz*dz)
		}
	}
	return e
}

func work() int {
	bs := system()
	for i := 0; i < 100_000; i++ {
		advance(bs, 0.01)
	}
	return int(energy(bs) * 1e9)
}

func main() { h.Run("nbody", 20, work) }
