package vet

import (
	"strings"
	"testing"
)

// nilFuncDiags runs all analyzers on src and returns the messages emitted
// by the nilfunc analyzer (the only analyzer whose messages start with
// "comparison of function").
func nilFuncDiags(t *testing.T, src string) []string {
	t.Helper()
	var out []string
	for _, m := range strings.Split(runVetOnSrc(t, src), "\n") {
		if strings.Contains(m, "comparison of function") {
			out = append(out, m)
		}
	}
	return out
}

// A declared function can never be nil, so comparing one to nil is
// always false (==) or always true (!=). Method values and package-level
// functions are declarations too.
func TestNilFuncDeclaredFunction(t *testing.T) {
	src := `package main

func g() {}

type S struct{}

func (s S) M() {}

func main() {
	_ = g == nil
	_ = g != nil
	var s S
	_ = s.M == nil
}
`
	got := nilFuncDiags(t, src)
	want := []string{
		"comparison of function g == nil is always false",
		"comparison of function g != nil is always true",
		"comparison of function M == nil is always false",
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d nilfunc diagnostics, got %d: %q", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("diagnostic %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

// A func-typed variable, struct field, or map element may hold nil, so
// comparing one to nil is meaningful and must not be flagged.
func TestNilFuncFuncVariableNotFlagged(t *testing.T) {
	src := `package main

type S struct {
	f func()
}

func main() {
	var f func()
	if f == nil {
		println("nil func")
	}
	_ = f != nil
	var s S
	_ = s.f == nil
	m := map[string]func(){}
	_ = m["k"] == nil
}
`
	if got := nilFuncDiags(t, src); len(got) != 0 {
		t.Fatalf("func-typed variables/fields/elements compared to nil must not be flagged, got: %q", got)
	}
}

// suspiciousDiags runs all analyzers on src and returns the messages
// emitted by the bools analyzer's precedence check.
func suspiciousDiags(t *testing.T, src string) []string {
	t.Helper()
	var out []string
	for _, m := range strings.Split(runVetOnSrc(t, src), "\n") {
		if strings.Contains(m, "suspicious") {
			out = append(out, m)
		}
	}
	return out
}

// x == !y parses unambiguously as x == (!y): there is no precedence
// trap, so it must not be flagged. !x == y is a genuine trap
// ((!x) == y reads like !(x == y)) and stays flagged.
func TestBoolsBangYNotFlagged(t *testing.T) {
	src := `package main

func main() {
	x, y := true, false
	_ = x == !y
	_ = x != !y
	_ = !x == y
}
`
	got := suspiciousDiags(t, src)
	if len(got) != 1 {
		t.Fatalf("expected exactly one suspicious diagnostic (for !x == y), got %d: %q", len(got), got)
	}
	if want := "suspicious !x == y, use !(x == y) instead"; got[0] != want {
		t.Errorf("got %q, want %q", got[0], want)
	}
}
