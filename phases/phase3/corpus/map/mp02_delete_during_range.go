package main

import "fmt"

func main() {
	// Deleting the current key during range is safe in Go. The
	// order-independent property: every key was visited exactly once
	// (sum 0+1+...+9 = 45) and the map is empty afterwards.
	m := map[int]int{}
	for i := 0; i < 10; i++ {
		m[i] = i * i
	}
	sum := 0
	count := 0
	for k := range m {
		sum += k
		count++
		delete(m, k)
	}
	fmt.Println(count, sum, len(m))
}
