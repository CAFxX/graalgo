// Fixed-size 2-D arrays ([N][N]int values): array indexing, array copies.
package main

import "graalgo/bench/h"

const n = 48

type mat [n][n]int

func mul(a, b *mat) mat {
	var c mat
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			s := 0
			for k := 0; k < n; k++ {
				s += a[i][k] * b[k][j]
			}
			c[i][j] = s
		}
	}
	return c
}

func work() int {
	var a, b mat
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			a[i][j] = (i*j)%7 - 3
			b[i][j] = (i+j)%5 - 2
		}
	}
	for r := 0; r < 16; r++ {
		c := mul(&a, &b)
		a = c
		for i := 0; i < n; i++ {
			for j := 0; j < n; j++ {
				a[i][j] %= 1000
			}
		}
	}
	return a[1][2] + a[n-1][n-1] + a[7][3]
}

func main() { h.Run("arraymat", 30, work) }
