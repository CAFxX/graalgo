package main

// Regression tests for review-1105-mr-param (independent review
// 2026-11-05-11): multi-result call through a function PARAMETER.
//
// A parameter has no initializer, so the MR-closure-var linking
// (mrInfoForFuncValueInit, which links vars from their initializer)
// never covers it, and calledFunc returns nil for a parameter ident
// (a *types.Var, not a *types.Func). Calls through such a parameter
// failed with the unnamed WS D error
// "multi-valued call result is not supported in frontend/v1 scope
// (WS D)". Controls that already passed: a var initialized with a
// func literal, a var initialized with a named func, a field of a
// locally-constructed struct, single-result func values.
//
// The open question from the lane brief — a generic struct-field call
// h.f() where h is a parameter of a generic named struct — is also
// covered (TestReview1105MRParamGenericStructField): the field
// classifier resolves the field's type through the instantiation
// substitution, so no separate fix was needed.
//
// The tests export whole programs through ExportFile (the same entry
// point the gofrontend binary uses) and assert success.

import (
	"os"
	"path/filepath"
	"testing"
)

func exportReview1105MRParamProg(t *testing.T, name, src string) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ExportFile(p); err != nil {
		t.Fatalf("ExportFile(%s) failed: %v", name, err)
	}
}

func TestReview1105MRParamReturn(t *testing.T) {
	// Non-generic parameter, return form: return f().
	exportReview1105MRParamProg(t, "paramreturn.go", `package main

func two() (int, int) { return 1, 2 }

func callPlain(f func() (int, int)) (int, int) { return f() }

func main() {
	a, b := callPlain(two)
	_, _ = a, b
}
`)
}

func TestReview1105MRParamAssign(t *testing.T) {
	// Non-generic parameter, assignment form: a, b := f().
	exportReview1105MRParamProg(t, "paramassign.go", `package main

func two() (int, int) { return 1, 2 }

func callAssign(f func() (int, int)) {
	a, b := f()
	_, _ = a, b
}

func main() { callAssign(two) }
`)
}

func TestReview1105MRParamGeneric(t *testing.T) {
	// Generic function with a multi-result func parameter, return form,
	// instantiated with a func literal argument.
	exportReview1105MRParamProg(t, "paramgeneric.go", `package main

func callTwice[T any](f func() (T, T)) (T, T) { return f() }

func main() {
	a, b := callTwice(func() (int, int) { return 1, 2 })
	_, _ = a, b
}
`)
}

func TestReview1105MRParamGenericNamedFunc(t *testing.T) {
	// Generic parameter form driven by a named function value.
	exportReview1105MRParamProg(t, "paramgenericnamed.go", `package main

func two() (int, int) { return 1, 2 }

func callTwice[T any](f func() (T, T)) (T, T) { return f() }

func main() {
	a, b := callTwice[int](two)
	_, _ = a, b
}
`)
}

func TestReview1105MRParamNamedFuncType(t *testing.T) {
	// Parameter declared with a named func type.
	exportReview1105MRParamProg(t, "paramnamedtype.go", `package main

type F func() (int, int)

func two() (int, int) { return 1, 2 }

func callF(f F) (int, int) { return f() }

func main() {
	a, b := callF(two)
	_, _ = a, b
}
`)
}

func TestReview1105MRParamMultiple(t *testing.T) {
	// Two multi-result func parameters in one signature.
	exportReview1105MRParamProg(t, "parammultiple.go", `package main

func two() (int, int) { return 1, 2 }
func three() (int, int) { return 3, 4 }

func call2(f, g func() (int, int)) (int, int) {
	a, _ := f()
	_, b := g()
	return a, b
}

func main() {
	a, b := call2(two, three)
	_, _ = a, b
}
`)
}

func TestReview1105MRParamWithArgs(t *testing.T) {
	// Multi-result func parameter that takes arguments.
	exportReview1105MRParamProg(t, "paramwithargs.go", `package main

func inc(x int) (int, int) { return x + 1, x + 2 }

func callArg(f func(int) (int, int)) (int, int) { return f(10) }

func main() {
	a, b := callArg(inc)
	_, _ = a, b
}
`)
}

func TestReview1105MRParamMethod(t *testing.T) {
	// Multi-result func parameter of a method.
	exportReview1105MRParamProg(t, "parammethod.go", `package main

type S struct{}

func two() (int, int) { return 1, 2 }

func (s S) call(f func() (int, int)) (int, int) { return f() }

func main() {
	var s S
	a, b := s.call(two)
	_, _ = a, b
}
`)
}

func TestReview1105MRParamFuncLit(t *testing.T) {
	// Multi-result func parameter of a function literal.
	exportReview1105MRParamProg(t, "paramfunclit.go", `package main

func two() (int, int) { return 1, 2 }

func main() {
	call := func(f func() (int, int)) (int, int) { return f() }
	a, b := call(two)
	_, _ = a, b
}
`)
}

func TestReview1105MRParamCallArg(t *testing.T) {
	// Call through a parameter in call-argument position: use(f()).
	exportReview1105MRParamProg(t, "paramcallarg.go", `package main

func two() (int, int) { return 1, 2 }

func use(a, b int) {}

func callArgPos(f func() (int, int)) { use(f()) }

func main() { callArgPos(two) }
`)
}

func TestReview1105MRParamDefer(t *testing.T) {
	// Call through a parameter in direct defer position (results
	// discarded, legal Go): defer f().
	exportReview1105MRParamProg(t, "paramdefer.go", `package main

func two() (int, int) { return 1, 2 }

func callDefer(f func() (int, int)) {
	defer f()
}

func main() { callDefer(two) }
`)
}

func TestReview1105MRParamClosureCapture(t *testing.T) {
	// Call through a parameter captured by a function literal.
	exportReview1105MRParamProg(t, "paramcapture.go", `package main

func two() (int, int) { return 1, 2 }

func callCapture(f func() (int, int)) {
	defer func() {
		a, b := f()
		_, _ = a, b
	}()
}

func main() { callCapture(two) }
`)
}

func TestReview1105MRParamGenericTwoInstantiations(t *testing.T) {
	// Same generic template instantiated with two different type
	// arguments: the per-instantiation pair type must not leak from
	// the first instantiation into the second (linkMRFuncParams
	// rewrites the link on every instantiation body export).
	exportReview1105MRParamProg(t, "paramgenerictwo.go", `package main

func twoInt() (int, int) { return 1, 2 }
func twoStr() (string, string) { return "a", "b" }

func callTwice[T any](f func() (T, T)) (T, T) { return f() }

func main() {
	a, b := callTwice[int](twoInt)
	_, _ = a, b
	c, d := callTwice[string](twoStr)
	_, _ = c, d
}
`)
}

func TestReview1105MRParamThreeResults(t *testing.T) {
	// Three-result func parameter (arity-generality of the pair).
	exportReview1105MRParamProg(t, "paramthree.go", `package main

func three() (int, string, error) { return 1, "x", nil }

func call3(f func() (int, string, error)) (int, string, error) { return f() }

func main() {
	a, b, c := call3(three)
	_, _, _ = a, b, c
}
`)
}

func TestReview1105MRParamGenericStructField(t *testing.T) {
	// OPEN QUESTION: call h.f() where h is a parameter of a generic
	// named struct type. This goes through the func-field classifier,
	// not the parameter-ident path.
	exportReview1105MRParamProg(t, "paramgenericfield.go", `package main

type Holder[T any] struct {
	f func() (T, T)
}

func callField[T any](h Holder[T]) (T, T) { return h.f() }

func main() {
	h := Holder[int]{f: func() (int, int) { return 1, 2 }}
	a, b := callField[int](h)
	_, _ = a, b
}
`)
}
