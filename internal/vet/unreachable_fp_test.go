package vet

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// QN-3: labeled breaks targeting outer labels, return, and goto-out must
// count as loop exits — code after such loops is reachable and must not
// be flagged unreachable.
func TestUnreachableLoopExitsNoFP(t *testing.T) {
	src := `package main

func done() bool { return true }

func loopBreakOuter() {
outer:
	for {
		if done() {
			break outer
		}
	}
	println("after break-outer")
}

func loopBreakOuterSwitch() {
outer:
	for {
		switch {
		case done():
			break outer
		}
	}
	println("after switch break-outer")
}

func loopReturn() {
	for {
		if done() {
			return
		}
	}
	println("after return")
}

func loopGotoOut() {
	for {
		if done() {
			goto finished
		}
	}
finished:
	println("after goto-out")
}
`
	pass := testPass(t, src)
	if ds := runUnreachable(pass); len(ds) != 0 {
		for _, d := range ds {
			t.Errorf("false positive: %s: %s", d.Pos, d.Message)
		}
	}
}

// QN-4: a backward goto is a loop-back edge, not a terminator — code
// after it may be reached via other jumps and must not be flagged.
func TestUnreachableBackwardGotoNoFP(t *testing.T) {
	src := `package main

func done() bool { return true }

func backwardGoto() {
loop:
	println("x")
	if done() {
		goto out
	}
	goto loop
out:
	println("done")
}
`
	pass := testPass(t, src)
	if ds := runUnreachable(pass); len(ds) != 0 {
		for _, d := range ds {
			t.Errorf("false positive: %s: %s", d.Pos, d.Message)
		}
	}
}

// Negative controls: genuinely unreachable code must still be flagged.
func TestUnreachableTruePositives(t *testing.T) {
	src := `package main

func infiniteLoop() {
	for {
		println("spin")
	}
	println("never")
}

func forwardGoto() {
	goto skip
	println("skipped")
skip:
	println("done")
}

func returnThenMore() {
	println("x")
	return
	println("dead")
}

func innerLabelBreak() {
	for {
	inner:
		for {
			break inner
		}
	}
	println("never2")
}

func funclitReturn() {
	for {
		f := func() {
			return
		}
		_ = f
	}
	println("never3")
}
`
	pass := testPass(t, src)
	ds := runUnreachable(pass)
	if len(ds) != 5 {
		t.Fatalf("expected 5 diagnostics, got %d: %v", len(ds), ds)
	}
	for _, d := range ds {
		if d.Message != "unreachable code" {
			t.Errorf("unexpected diagnostic: %s: %s", d.Pos, d.Message)
		}
	}
}

// loopBody parses "package main; func f() { <src> }" and returns the body
// of the first for statement found.
func loopBody(t *testing.T, src string) *ast.BlockStmt {
	t.Helper()
	full := "package main\nfunc f() {\n" + src + "\n}\n"
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", full, 0)
	if err != nil {
		t.Fatal(err)
	}
	var body *ast.BlockStmt
	ast.Inspect(f, func(n ast.Node) bool {
		if fs, ok := n.(*ast.ForStmt); ok && body == nil {
			body = fs.Body
		}
		return true
	})
	if body == nil {
		t.Fatal("no for statement found")
	}
	return body
}

// QN-3 unit tests for hasLoopExit: a return, or a break/goto targeting a
// label outside the loop body, exits the loop; an unlabeled break inside
// a nested switch/select, or a break/goto targeting an inner label, does
// not. A return inside a nested func literal belongs to the literal.
//
// These are unit-tested directly because the labeled-break cases cannot
// produce an end-to-end false positive on the old code: terminates never
// unwrapped LabeledStmt, so a labeled infinite loop never triggered the
// rule at all (a pre-existing false negative, out of scope).
func TestHasLoopExit(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want bool
	}{
		{
			"unlabeled break",
			`for { break }`,
			true,
		},
		{
			"labeled break to outer label",
			`outer:
			for {
				switch true {
				case true:
					break outer
				}
			}`,
			true,
		},
		{
			"labeled break to inner label stays",
			`for {
			inner:
				switch true {
				case true:
					break inner
				}
			}`,
			false,
		},
		{
			"unlabeled break in nested switch targets switch",
			`for {
				switch true {
				case true:
					break
				}
			}`,
			false,
		},
		{
			"unlabeled break in nested select targets select",
			`for {
				select {
				case <-ch:
					break
				default:
				}
			}`,
			false,
		},
		{
			"return exits",
			`for {
				if true {
					return
				}
			}`,
			true,
		},
		{
			"return in nested func literal does not exit",
			`for {
				f := func() { return }
				_ = f
			}`,
			false,
		},
		{
			"goto outside loop exits",
			`for {
				if true {
					goto done
				}
			}
			done:
				_ = 0`,
			true,
		},
		{
			"backward goto to inner label stays",
			`for {
			again:
				_ = 0
				goto again
			}`,
			false,
		},
		{
			"forward goto to inner label stays",
			`for {
				if true {
					goto skip
				}
				_ = 0
			skip:
				_ = 1
			}`,
			false,
		},
		{
			"no exit",
			`for { _ = 0 }`,
			false,
		},
	}
	for _, c := range cases {
		if got := hasLoopExit(loopBody(t, c.src)); got != c.want {
			t.Errorf("%s: hasLoopExit = %v, want %v", c.name, got, c.want)
		}
	}
}
