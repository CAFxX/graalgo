// Slice traversal: indexed and range loops over []int, bounds checks.
package main

import "graalgo/bench/h"

var data = func() []int {
	s := make([]int, 1<<20)
	for i := range s {
		s[i] = i*7 + 3
	}
	return s
}()

func sumIndex(s []int) int {
	t := 0
	for i := 0; i < len(s); i++ {
		t += s[i]
	}
	return t
}

func sumRange(s []int) int {
	t := 0
	for _, v := range s {
		t ^= v
	}
	return t
}

func main() {
	h.Run("slicesum", 30, func() int {
		t := 0
		for k := 0; k < 4; k++ {
			t += sumIndex(data) + sumRange(data)
		}
		return t
	})
}
