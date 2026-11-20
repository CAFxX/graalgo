// Spectral norm (benchmarks game): float64 loops, small function calls.
package main

import (
	"math"

	"graalgo/bench/h"
)

func a(i, j int) float64 { return 1.0 / float64((i+j)*(i+j+1)/2+i+1) }

func times(v []float64, u []float64) {
	for i := range v {
		s := 0.0
		for j := range u {
			s += a(i, j) * u[j]
		}
		v[i] = s
	}
}

func timesT(v []float64, u []float64) {
	for i := range v {
		s := 0.0
		for j := range u {
			s += a(j, i) * u[j]
		}
		v[i] = s
	}
}

func atimes(v, u, tmp []float64) {
	times(tmp, u)
	timesT(v, tmp)
}

func work() int {
	const n = 200
	u := make([]float64, n)
	v := make([]float64, n)
	tmp := make([]float64, n)
	for i := range u {
		u[i] = 1
	}
	for i := 0; i < 10; i++ {
		atimes(v, u, tmp)
		atimes(u, v, tmp)
	}
	var vbv, vv float64
	for i := range v {
		vbv += u[i] * v[i]
		vv += v[i] * v[i]
	}
	return int(math.Sqrt(vbv/vv) * 1e9)
}

func main() { h.Run("spectralnorm", 20, work) }
