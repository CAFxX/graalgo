// verifies: GG-0020
package main

import "fmt"

// P3-M4 remediation (P1-2): the nil-map write panics inside helper().
// The innermost traceback frame must be attributed to the panicking
// statement (line 12), not helper's definition line (line 8). The
// harness compares the FULL normalized stderr, so a regression to the
// definition line fails the corpus.

var m map[string]int

func helper() {
	m["x"] = 1 // line 15: the panicking statement
}

func main() {
	fmt.Println("about to write")
	helper()
}
