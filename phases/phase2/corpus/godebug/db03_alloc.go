package main

import "fmt"

func main() {
	var total int
	for i := 0; i < 200; i++ {
		b := make([]byte, 100000)
		for j := range b {
			b[j] = byte(j)
		}
		total += len(b)
	}
	fmt.Println(total)
}
