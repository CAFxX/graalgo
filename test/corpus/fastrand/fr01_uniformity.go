package main

import (
	"fmt"
	_ "unsafe"
)

//go:linkname fastrandn runtime.fastrandn
func fastrandn(n uint32) uint32

func main() {
	// Bucket uniformity: 100k samples into 10 buckets. Each bucket
	// expects ~10k; assert all within [5k, 15k] (very wide — a broken
	// RNG fails by orders of magnitude, not by noise).
	var counts [10]int
	for i := 0; i < 100000; i++ {
		counts[fastrandn(10)]++
	}
	min, max := counts[0], counts[0]
	for _, c := range counts[1:] {
		if c < min {
			min = c
		}
		if c > max {
			max = c
		}
	}
	ok := min >= 5000 && max <= 15000
	fmt.Println("uniform:", ok)
}
