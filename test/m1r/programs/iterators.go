package main

import (
	"cmp"
	"maps"
	"slices"
)

func show[T any](label string, s []T, f func(T)) {
	print(label, " [")
	for _, v := range s {
		f(v)
		print(" ")
	}
	println("]", len(s))
}
func pi(i int)    { print(i) }
func ps(s string) { print(s) }

// Function iterators inside upstream's code: slices and maps range over
// the sequences they are given and return sequences of their own.
func main() {
	m := map[string]int{"a": 1, "b": 2, "c": 3, "d": 4}
	show("keys", slices.Sorted(maps.Keys(m)), ps)
	vals := slices.Collect(maps.Values(m))
	slices.Sort(vals)
	show("vals", vals, pi)
	sum := 0
	for k, v := range maps.All(m) {
		sum += v * len(k)
	}
	println(sum)
	m2 := maps.Collect(slices.All([]string{"x", "y", "z"}))
	println(len(m2), m2[0], m2[2])
	maps.Insert(m, maps.All(map[string]int{"e": 5}))
	println(len(m))

	s := []int{5, 2, 8, 1, 9}
	for i, v := range slices.Backward(s) {
		print(i, ":", v, " ")
	}
	println()
	for c := range slices.Chunk([]int{1, 2, 3, 4, 5}, 2) {
		show("chunk", c, pi)
	}
	show("appseq", slices.AppendSeq([]int{0}, slices.Values([]int{4, 5})), pi)
	show("sortedfunc", slices.SortedFunc(slices.Values(s), func(a, b int) int { return cmp.Compare(b, a) }), pi)
	show("stable", slices.SortedStableFunc(slices.Values([]string{"bb", "a", "cc", "d"}), func(a, b string) int { return cmp.Compare(len(a), len(b)) }), ps)
	n := 0
	for v := range slices.Values(s) {
		if v == 8 {
			break
		}
		n += v
	}
	println(n)
}
