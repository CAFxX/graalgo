package main

import "fmt"

// range-over-func (Go 1.23+): deterministic custom iterator
func squares(n int) func(yield func(int) bool) {
	return func(yield func(int) bool) {
		for i := 0; i < n; i++ {
			if !yield(i * i) {
				return
			}
		}
	}
}

func main() {
	sum := 0
	for sq := range squares(6) {
		sum += sq
	}
	fmt.Println(sum) // 0+1+4+9+16+25 = 55
	for sq := range squares(10) {
		if sq > 20 {
			break // early exit exercises yield=false path
		}
		fmt.Print(sq, " ")
	}
	fmt.Println()
}
