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
	"go/ast"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/parser"
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
		name: "IndexExpr_genericfunc",
		src: `package main
func id[T any](x T) T { return x }
func main() { println(id[int](42)) }
`,
		want: "unsupported expr",
	},
	{
		name: "IndexListExpr",
		src: `package main
func pair[A any, B any](a A, b B) (A, B) { return a, b }
func main() { x, y := pair[int, string](1, "s"); println(x, y) }
`,
		want: "unsupported expr",
	},
	{
		name: "GenDecl_const",
		src: `package main
const Answer = 42
func main() { println(Answer) }
`,
		want: "unsupported",
	},
	{
		name: "GenDecl_type",
		src: `package main
type T int
func main() { v := T(3); println(v) }
`,
		want: "unsupported",
	},
	{
		// A method declaration fails loudly. (Currently the top-level
		// type declaration it requires fails first; if type declarations
		// are ever implemented, this probe exercises the method path in
		// emitFuncDecl. Either way the program must not be silently
		// accepted.)
		name: "FuncDecl_method",
		src: `package main
type T struct{ n int }
func (t T) Get() int { return t.n }
func main() {}
`,
		want: "unsupported",
	},
}

func TestNegativeCasesFailLoudly(t *testing.T) {
	for _, tc := range negativeCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			fn := filepath.Join(dir, "neg.go")
			if err := os.WriteFile(fn, []byte(tc.src), 0644); err != nil {
				t.Fatal(err)
			}

			pres, err := parser.ParseFiles([]string{fn})
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			cres, err := checker.Check(pres.Fset, pres.Pkg, &checker.Config{GoVersion: "go1.27"})
			if err != nil {
				t.Fatalf("check: %v", err)
			}
			_, err = Emit(cres, pres.Pkg, pres.Files)
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

// TestEmitIndexRejectsGenericTypeInstantiation proves emitIndex detects a
// generic *type* instantiation via Info.Instances and rejects it loudly
// instead of mis-emitting it as an index expression.
//
// A direct unit test is used because end-to-end the program already fails
// earlier: the top-level generic type declaration is loudly rejected by
// sortedTopVarSpecs. This test pins the emitIndex path itself, so the
// detection keeps working if top-level type declarations are implemented
// later. It is demonstrably capable of failing: with the Instances check
// removed, emitIndex would proceed to emit x/index subexpressions and the
// assertion below would fail.
func TestEmitIndexRejectsGenericTypeInstantiation(t *testing.T) {
	src := `package main
type Box[T any] struct{ v T }
func conv(x int) Box[int] { return Box[int](Box[int]{v: x}) }
func main() { println(conv(1).v) }
`
	dir := t.TempDir()
	fn := filepath.Join(dir, "gen.go")
	if err := os.WriteFile(fn, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	pres, err := parser.ParseFiles([]string{fn})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cres, err := checker.Check(pres.Fset, pres.Pkg, &checker.Config{GoVersion: "go1.27"})
	if err != nil {
		t.Fatalf("check: %v", err)
	}

	// Find the Box[int] conversion's Fun: CallExpr -> IndexExpr.
	var ix *ast.IndexExpr
	for _, f := range pres.Pkg.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok {
				if idx, ok := c.Fun.(*ast.IndexExpr); ok {
					ix = idx
					return false
				}
			}
			return true
		})
	}
	if ix == nil {
		t.Fatal("test setup: no IndexExpr conversion found in probe program")
	}

	// The checker must have recorded the instantiation (this is the
	// mechanism the fix relies on).
	id := instantiationIdent(ix)
	if id == nil {
		t.Fatal("test setup: instantiationIdent returned nil for Box[int]")
	}
	if _, ok := cres.Info.Instances[id]; !ok {
		t.Fatal("test setup: checker did not record the Box[int] instantiation in Info.Instances")
	}

	e := &Emitter{info: cres}
	if _, err := e.emitIndex(ix); err == nil {
		t.Fatal("emitIndex accepted a generic type instantiation (silent miscompile)")
	} else if !strings.Contains(err.Error(), "generic instantiation") {
		t.Fatalf("emitIndex error %q does not name generic instantiation", err.Error())
	}
}
