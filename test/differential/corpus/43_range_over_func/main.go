package main

import "fmt"

// range-over-func (Go 1.23+): deterministic custom iterators.

// squares is a Seq-style (single value) iterator.
func squares(n int) func(yield func(int) bool) {
	return func(yield func(int) bool) {
		for i := 0; i < n; i++ {
			if !yield(i * i) {
				return
			}
		}
	}
}

// pairs is a Seq2-style (two value) iterator over index/name pairs.
func pairs(n int) func(yield func(int, string) bool) {
	return func(yield func(int, string) bool) {
		for i := 0; i < n; i++ {
			if !yield(i, fmt.Sprint("v", i)) {
				return
			}
		}
	}
}

func main() {
	// range over a single-value sequence
	sum := 0
	for sq := range squares(6) {
		sum += sq
	}
	fmt.Println(sum) // 0+1+4+9+16+25 = 55

	// break exercises the yield=false path
	for sq := range squares(10) {
		if sq > 20 {
			break
		}
		fmt.Print(sq, " ")
	}
	fmt.Println() // 0 1 4 9 16

	// range over a two-value sequence
	total := 0
	for i, s := range pairs(4) {
		fmt.Print(i, "=", s, " ")
		total += i
	}
	fmt.Println()      // 0=v0 1=v1 2=v2 3=v3
	fmt.Println(total) // 0+1+2+3 = 6

	// continue inside the loop body
	for sq := range squares(6) {
		if sq%2 == 1 {
			continue
		}
		fmt.Print(sq, " ")
	}
	fmt.Println() // 0 4 16

	// break out of a two-value range
	for i, s := range pairs(10) {
		if i == 2 {
			break
		}
		fmt.Print(s, " ")
	}
	fmt.Println() // v0 v1
}
