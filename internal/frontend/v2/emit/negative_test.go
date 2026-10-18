// Negative tests: unhandled AST node kinds must fail loudly.
//
// Each test feeds a minimal Go program containing one unhandled construct
// through the full pipeline (parse → check → emit) and asserts the emitter
// returns an "unsupported" error — never a silent drop, never a panic,
// never a wrong result.
//
// If a construct starts being handled, its test moves to the positive
// suite. The coverage matrix (coverage_test.go) tracks the classification.
package emit

import (
	"strings"
	"testing"
)

// negativeCase is one unhandled construct.
type negativeCase struct {
	name string // AST node kind under test
	src  string // minimal program containing it
	want string // substring of the expected error
}

var negativeCases = []negativeCase{
	{
		name: "DeferStmt",
		src: `package main
func main() { defer println("x") }
`,
		want: "unsupported stmt",
	},
	{
		name: "GoStmt",
		src: `package main
func main() { go println("x") }
`,
		want: "unsupported stmt",
	},
	{
		name: "SendStmt",
		src: `package main
func send(c chan int) { c <- 1 }
func main() {}
`,
		want: "unsupported stmt",
	},
	{
		name: "SelectStmt",
		src: `package main
func sel(c chan int) {
	select {
	case x := <-c:
		println(x)
	default:
	}
}
func main() {}
`,
		want: "unsupported stmt",
	},
	{
		name: "SwitchStmt",
		src: `package main
func main() {
	x := 1
	switch x {
	case 1:
		println("one")
	default:
		println("other")
	}
}
`,
		want: "unsupported stmt",
	},
	{
		name: "TypeSwitchStmt",
		src: `package main
func main() {
	var x any = 1
	switch v := x.(type) {
	case int:
		println(v)
	default:
	}
}
`,
		want: "unsupported stmt",
	},
	{
		name: "LabeledStmt",
		src: `package main
func main() {
outer:
	for i := 0; i < 3; i++ {
		break outer
	}
}
`,
		want: "unsupported stmt",
	},
	{
		name: "EmptyStmt",
		src: `package main
func main() {
	x := 1
	;
	println(x)
}
`,
		want: "unsupported stmt",
	},
	{
		name: "StarExpr",
		src: `package main
func main() {
	x := 42
	p := &x
	println(*p)
}
`,
		want: "unsupported expr",
	},
	{
		name: "TypeAssertExpr",
		src: `package main
func main() {
	var x any = 42
	y := x.(int)
	println(y)
}
`,
		want: "unsupported expr",
	},
	{
		name: "FuncLit",
		src: `package main
func main() {
	f := func(x int) int { return x * 2 }
	println(f(21))
}
`,
		want: "unsupported expr",
	},
	{
		name: "Ellipsis",
		src: `package main
func sum(xs ...int) int {
	s := 0
	for _, x := range xs {
		s += x
	}
	return s
}
func main() {
	s := []int{1, 2, 3}
	println(sum(s...))
}
`,
		want: "unsupported expr",
	},
	{
		name: "IndexExpr_generics",
		src: `package main
func id[T any](x T) T { return x }
func main() { println(id[int](42)) }
`,
		want: "unsupported expr",
	},
	{
		// A single type argument parses as IndexExpr (see the case above,
		// which exercises emitIndex's generics branch); two or more parse
		// as IndexListExpr and hit emitExpr's default.
		name: "IndexListExpr",
		src: `package main
func pair[T any, U any](a T, b U) (T, U) { return a, b }
func main() {
	a, b := pair[int, string](1, "x")
	println(a)
	println(b)
}
`,
		want: "unsupported expr",
	},
	{
		name: "GenDecl",
		src: `package main
func main() {
	const k = 1
	println(k)
}
`,
		want: "unsupported decl",
	},
	{
		name: "TypeSpec",
		src: `package main
func main() {
	type T int
	var t T = 3
	println(t)
}
`,
		want: "unsupported decl",
	},
}

func TestNegativeCasesFailLoudly(t *testing.T) {
	for _, tc := range negativeCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := emitProgram(t, tc.src)
			if err == nil {
				t.Fatalf("%s: expected loud failure, got success (silent acceptance)", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("%s: error %q does not contain %q (must fail with 'unsupported')", tc.name, err.Error(), tc.want)
			}
			// Must not be a panic, must not be a nil dereference.
			if strings.Contains(err.Error(), "panic") || strings.Contains(err.Error(), "nil pointer") {
				t.Fatalf("%s: error %q looks like a crash, not a clean rejection", tc.name, err.Error())
			}
		})
	}
}
