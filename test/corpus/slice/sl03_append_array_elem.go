package main

import "fmt"

// P3 bug-042: append to a slice-of-arrays must materialize elements so
// double-index stores work. The growslice zero-size fast path was
// dropping appended-to elements (elemSizeBytes(ARRAY) was 0).
func main() {
	// Base repro: append then double-index store.
	rows := [][2]int{{1, 2}}
	rows = append(rows, [2]int{3, 4})
	rows[0][0] = 99
	fmt.Println("base:", rows[0][0], rows[0][1], rows[1][0], rows[1][1])

	// Store into the appended element too.
	rows[1][1] = 88
	fmt.Println("appidx:", rows[1][0], rows[1][1])

	// Multiple appends forcing several growslice rounds.
	var s [][3]int
	for i := 0; i < 10; i++ {
		s = append(s, [3]int{i, i + 1, i + 2})
	}
	s[0][0] = 100
	s[9][2] = 200
	fmt.Println("loop:", s[0][0], s[5][1], s[9][2], len(s))

	// Deeper nesting.
	deep := [][2][2]int{{{1, 2}, {3, 4}}}
	deep = append(deep, [2][2]int{{5, 6}, {7, 8}})
	deep[0][1][0] = 99
	fmt.Println("nest:", deep[0][0][0], deep[0][1][0], deep[1][0][0])
}
