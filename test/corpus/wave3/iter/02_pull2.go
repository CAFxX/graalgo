package main

import (
	"fmt"
	"iter"
	"slices"
)

func main() {
	// iter.Pull2: convert push Seq2 iterator to pull iterator
	next, stop := iter.Pull2(slices.All([]string{"a", "b"}))
	defer stop()
	for {
		k, v, ok := next()
		if !ok {
			break
		}
		fmt.Println(k, v) // 0 a, 1 b
	}
}
