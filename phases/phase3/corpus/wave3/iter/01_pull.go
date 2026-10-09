package main

import (
	"fmt"
	"iter"
	"slices"
)

func main() {
	// iter.Pull: convert push iterator to pull iterator
	next, stop := iter.Pull(slices.Values([]int{10, 20, 30}))
	defer stop()
	for {
		v, ok := next()
		if !ok {
			break
		}
		fmt.Println(v) // 10 20 30
	}
}
