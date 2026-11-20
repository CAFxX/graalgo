// Mandelbrot: float64 inner loop with an early exit, nested loops.
package main

import "graalgo/bench/h"

func work() int {
	const size = 240
	const iters = 50
	count := 0
	for y := 0; y < size; y++ {
		ci := 2*float64(y)/size - 1
		for x := 0; x < size; x++ {
			cr := 2*float64(x)/size - 1.5
			zr, zi := 0.0, 0.0
			i := 0
			for ; i < iters && zr*zr+zi*zi <= 4; i++ {
				zr, zi = zr*zr-zi*zi+cr, 2*zr*zi+ci
			}
			if i == iters {
				count++
			}
		}
	}
	return count
}

func main() { h.Run("mandelbrot", 30, work) }
