package main

import "sort"

// so08: custom Interface implementations — descending order, string
// length order, and Reverse composition; IsSorted as oracle.

type descInts []int // descending Less

func (d descInts) Len() int           { return len(d) }
func (d descInts) Less(i, j int) bool { return d[i] > d[j] }
func (d descInts) Swap(i, j int)      { d[i], d[j] = d[j], d[i] }

type byLen []string // order by string length, then lexicographic

func (s byLen) Len() int { return len(s) }
func (s byLen) Less(i, j int) bool {
	if len(s[i]) != len(s[j]) {
		return len(s[i]) < len(s[j])
	}
	return s[i] < s[j]
}
func (s byLen) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

func main() {
	d := descInts{3, 1, 4, 1, 5, 9, 2, 6}
	sort.Sort(d)
	for i, x := range d {
		if i > 0 {
			print(" ")
		}
		print(x)
	}
	println() // 9 6 5 4 3 2 1 1
	println(sort.IsSorted(d)) // true

	s := byLen{"pear", "fig", "a", "banana", "kiwi", "plum"}
	sort.Stable(s)
	for i, x := range s {
		if i > 0 {
			print(" ")
		}
		print(x)
	}
	println() // a fig kiwi pear plum banana
	println(sort.IsSorted(s)) // true

	// Reverse of a custom descending order = ascending.
	e := descInts{5, 3, 8, 1}
	sort.Sort(sort.Reverse(e))
	for i, x := range e {
		if i > 0 {
			print(" ")
		}
		print(x)
	}
	println() // 1 3 5 8
}
