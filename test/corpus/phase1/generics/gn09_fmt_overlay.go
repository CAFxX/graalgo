package main

import "fmt"

// The D4 fmt overlay (seven pure functions) via the Phase-1 import overlay.

func main() {
	fmt.Println("hello", 42)
	s := fmt.Sprintf("%d+%d=%d", 1, 2, 3)
	a := 0
	if s == "1+2=3" {
		a = 1
	}
	t := fmt.Sprint("x", 7, true)
	b := 0
	if t == "x7 true" {
		b = 1
	}
	u := fmt.Sprintln("a", "b")
	c := 0
	if u == "a b\n" {
		c = 1
	}
	a = a
	b = b
	c = c
}

// verifies: GG-0014
// observe: a b c
