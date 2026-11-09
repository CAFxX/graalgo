package vet

import (
	"strings"
	"testing"
)

// R05-V-13: a labeled break that exits an infinite loop must not make the
// code after the loop look unreachable. Labeled breaks are the normal way
// out of a `for {}` from inside a nested switch, and the only way out of
// an inner `for {}` to an outer label.
func TestUnreachableLabeledBreak(t *testing.T) {
	// Code after these loops is reachable: no "unreachable code"
	// diagnostic may fire.
	quiet := []struct {
		name string
		src  string
	}{
		{
			"labeled break directly in loop body",
			`package main

func f(cond bool) {
Outer:
	for {
		if cond {
			break Outer
		}
	}
	println("after")
}
`,
		},
		{
			"labeled break from inside a nested switch",
			`package main

func f(cond bool, x int) {
Outer:
	for {
		switch x {
		case 1:
			if cond {
				break Outer
			}
		}
	}
	println("after")
}
`,
		},
		{
			"labeled break in else branch",
			`package main

func f(cond bool) {
Outer:
	for {
		if cond {
			println("x")
		} else {
			break Outer
		}
	}
	println("after")
}
`,
		},
		{
			"labeled break to an outer label exits an inner infinite loop",
			`package main

func f(cond bool) {
Outer:
	for {
		for {
			if cond {
				break Outer
			}
		}
		println("after inner")
	}
	println("after outer")
}
`,
		},
	}
	for _, tc := range quiet {
		if got := runVetOnSrc(t, tc.src); strings.Contains(got, "unreachable") {
			t.Errorf("%s: labeled-break loop wrongly flagged unreachable:\n%s", tc.name, got)
		}
	}

	// Genuinely terminating infinite loops must still fire.
	firing := []struct {
		name string
		src  string
	}{
		{
			"unlabeled infinite loop with no break",
			`package main

func f() {
	for {
		println("x")
	}
	println("after")
}
`,
		},
		{
			"labeled infinite loop with no break",
			`package main

func f() {
Outer:
	for {
		println("x")
	}
	println("after")
}
`,
		},
		{
			"break to a label defined inside the loop exits only the nested loop",
			`package main

func f(x int) {
	for {
		Inner:
		for i := 0; i < 3; i++ {
			if x == i {
				break Inner
			}
		}
		println("still looping")
	}
	println("after")
}
`,
		},
		{
			"unlabeled break inside a nested switch exits only the switch",
			`package main

func f(x int) {
	for {
		switch x {
		case 1:
			break
		}
	}
	println("after")
}
`,
		},
	}
	for _, tc := range firing {
		if got := runVetOnSrc(t, tc.src); !strings.Contains(got, "unreachable") {
			t.Errorf("%s: expected unreachable diagnostic, got:\n%s", tc.name, got)
		}
	}
}
