package vet

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// unreachableLines runs the full vet suite over src and returns the
// line numbers carrying an "unreachable code" diagnostic, in order.
func unreachableLines(t *testing.T, src string) []int {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "p.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	ds, err := Run([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	var lines []int
	for _, d := range ds {
		if d.Message == "unreachable code" {
			lines = append(lines, d.Pos.Line)
		}
	}
	return lines
}

// TestUnreachableLoopExits pins the loop-exit analysis of the
// unreachable analyzer. A labeled break or a goto that leaves an
// infinite for loop makes the following code reachable, so it must not
// be reported; genuinely unreachable code must still be reported.
// Lines carrying "// want-unreachable" are the only lines expected to
// be flagged.
func TestUnreachableLoopExits(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "labeled break exits loop",
			src: `package main

func labeledBreak(cond bool) {
L:
	for {
		if cond {
			break L
		}
	}
	println("after")
}
`,
		},
		{
			name: "goto exits loop",
			src: `package main

func gotoExits(cond bool) {
	for {
		if cond {
			goto done
		}
	}
done:
	println("after")
}
`,
		},
		{
			name: "labeled break from nested loop exits outer loop",
			src: `package main

func nestedLabeledBreak(cond bool) {
L:
	for {
		for {
			if cond {
				break L
			}
		}
	}
	println("after")
}
`,
		},
		{
			name: "goto from nested switch exits loop",
			src: `package main

func switchGoto(x int) {
	for {
		switch x {
		case 1:
			goto done
		}
	}
done:
	println("after")
}
`,
		},
		{
			name: "break in else branch exits loop",
			src: `package main

func elseBreak(cond bool) {
	for {
		if cond {
		} else {
			break
		}
	}
	println("after")
}
`,
		},
		{
			name: "unlabeled break exits loop",
			src: `package main

func unlabeledBreak(cond bool) {
	for {
		if cond {
			break
		}
	}
	println("after")
}
`,
		},
		{
			name: "infinite loop with no exit still flags",
			src: `package main

func infiniteLoop() {
	for {
		println("x")
	}
	println("after") // want-unreachable
}
`,
		},
		{
			name: "unreachable after return still flags",
			src: `package main

func afterReturn() {
	println("before")
	return
	println("after") // want-unreachable
}
`,
		},
		{
			// Precision guard: a goto whose target stays inside the
			// loop body is not an exit; the loop still never ends.
			name: "goto inside body is not an exit",
			src: `package main

func innerGoto(cond bool) {
	for {
		if cond {
			goto again
		}
	again:
		println("x")
	}
	println("after") // want-unreachable
}
`,
		},
		{
			// Precision guard: a labeled break to a label defined
			// inside the loop body targets a nested construct, not
			// an exit from this loop.
			name: "labeled break to inner label is not an exit",
			src: `package main

func innerLabelBreak(cond bool) {
	for {
	inner:
		for {
			if cond {
				break inner
			}
		}
	}
	println("after") // want-unreachable
}
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var want []int
			for i, line := range strings.Split(tc.src, "\n") {
				if strings.Contains(line, "// want-unreachable") {
					want = append(want, i+1)
				}
			}
			if got := unreachableLines(t, tc.src); !slices.Equal(got, want) {
				t.Errorf("unreachable lines = %v, want %v", got, want)
			}
		})
	}
}
