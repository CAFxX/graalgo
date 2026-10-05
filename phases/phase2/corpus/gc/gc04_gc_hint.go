package main

import (
	"fmt"
	"runtime"
)

func main() {
	s := make([]int, 1000)
	for i := range s {
		s[i] = i
	}
	runtime.GC()
	sum := 0
	for _, v := range s {
		sum += v
	}
	fmt.Println("ok", sum)
}
