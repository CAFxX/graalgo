// Matrix multiply over [][]float64 (2-D slices of slices).
package main

import "graalgo/bench/h"

const n = 120

func mat(seed float64) [][]float64 {
	m := make([][]float64, n)
	for i := range m {
		m[i] = make([]float64, n)
		for j := range m[i] {
			m[i][j] = seed * float64(i-j) / float64(i+j+1)
		}
	}
	return m
}

var a, b = mat(1.5), mat(-0.25)

func mul(a, b [][]float64) [][]float64 {
	c := make([][]float64, n)
	for i := 0; i < n; i++ {
		ci := make([]float64, n)
		ai := a[i]
		for k := 0; k < n; k++ {
			aik := ai[k]
			bk := b[k]
			for j := 0; j < n; j++ {
				ci[j] += aik * bk[j]
			}
		}
		c[i] = ci
	}
	return c
}

func main() {
	h.Run("matmul", 30, func() int {
		c := mul(a, b)
		s := 0.0
		for i := 0; i < n; i++ {
			s += c[i][i] + c[i][n-1-i]
		}
		return int(s * 1000)
	})
}
