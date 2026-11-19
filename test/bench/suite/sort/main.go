// Sorting: sort.Ints, slices.Sort and sort.Slice on pseudo-random ints.
package main

import (
	"slices"
	"sort"

	"graalgo/bench/h"
)

var src = func() []int {
	s := make([]int, 50_000)
	x := uint32(12345)
	for i := range s {
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		s[i] = int(x % 1_000_000)
	}
	return s
}()

func work() int {
	a := slices.Clone(src)
	sort.Ints(a)
	b := slices.Clone(src)
	slices.Sort(b)
	c := slices.Clone(src)
	sort.Slice(c, func(i, j int) bool { return c[i] > c[j] })
	return a[100] + b[len(b)/2] + c[7]
}

func main() { h.Run("sort", 20, work) }
