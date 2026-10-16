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
		name: "IndexListExpr_generics",
		src: `package main
func id[T any](x T) T { return x }
func main() { println(id[int](42)) }
`,
		want: "unsupported expr",
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
