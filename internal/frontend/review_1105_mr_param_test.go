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
