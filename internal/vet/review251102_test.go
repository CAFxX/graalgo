package vet

import (
	"strings"
	"testing"
)

// Regression tests for the 2026-11-02 independent codebase review
// (stream rev251102-vet-checkers). Each test failed before its fix and
// passes after; they pin the corrected analyzer behavior.

// Issue 1: printf precision-star. "%.*f" consumes the precision argument
// before the value; a following verb must align with the next argument.
func TestPrintfPrecisionStar(t *testing.T) {
	src := `package main

import "fmt"

func f() {
	_ = fmt.Sprintf("%.*f %d", 2, 3.14, 42)
	_ = fmt.Sprintf("%*.*f", 6, 2, 3.14)
	_ = fmt.Sprintf("%*d", 5, 42)
}
`
	joined := runVetOnSrc(t, src)
	if joined != "" {
		t.Errorf("expected no diagnostics for correct star formats, got:\n%s", joined)
	}

	// The value after a precision star must still be checked, and the
	// following verb must align with its own argument.
	bad := `package main

import "fmt"

func f() {
	_ = fmt.Sprintf("%.*f %d", 2, "notfloat", 42)
	_ = fmt.Sprintf("%.*f %d", 2, 3.14, "notint")
}
`
	joined = runVetOnSrc(t, bad)
	if !strings.Contains(joined, "%.*f: expects floating point") {
		t.Errorf("expected float diagnostic for %%.*f value, got:\n%s", joined)
	}
	if !strings.Contains(joined, "%d: expects integer") {
		t.Errorf("expected integer diagnostic for %%d after %%.*f, got:\n%s", joined)
	}
	if strings.Count(joined, "expects") != 2 {
		t.Errorf("expected exactly 2 diagnostics, got:\n%s", joined)
	}
}

// Issue 2: nilfunc must not flag func variables, which can be nil; only
// declared functions (and function literals) are never nil.
func TestNilFuncVariable(t *testing.T) {
	src := `package main

func declared() {}

func f() {
	var v func()
	if v == nil {
		println("nil var")
	}
	if declared == nil {
		println("never")
	}
	if func() {} == nil {
		println("never")
	}
}
`
	joined := runVetOnSrc(t, src)
	if strings.Count(joined, "always false") != 2 {
		t.Errorf("expected exactly 2 nilfunc diagnostics (declared func + func literal), got:\n%s", joined)
	}
}

// Issue 3: loopclosure must catch "=" loops (ForStmt with an assignment
// init) and range values (not just keys) captured by closures.
func TestLoopClosureAssignAndRangeValue(t *testing.T) {
	src := `package main

var funcs []func() int

func f() {
	var i int
	for i = 0; i < 3; i++ {
		funcs = append(funcs, func() int { return i })
	}
}

func g(xs []int) {
	var k, v int
	for k, v = range xs {
		funcs = append(funcs, func() int { return v })
	}
	_, _ = k, v
}
`
	joined := runVetOnSrc(t, src)
	if !strings.Contains(joined, "loop variable i captured by func literal") {
		t.Errorf("expected diagnostic for = loop variable i, got:\n%s", joined)
	}
	if !strings.Contains(joined, "loop variable v captured by func literal") {
		t.Errorf("expected diagnostic for range value v, got:\n%s", joined)
	}
}

// Issue 4: slog Log takes (ctx, level, msg, args...); the level and msg
// must not be parsed as a key-value pair.
func TestSlogLogOffset(t *testing.T) {
	src := `package main

import (
	"context"
	"log/slog"
)

func f(l *slog.Logger, ctx context.Context) {
	l.Log(ctx, slog.LevelInfo, "msg", "k", "v")
	slog.Log(ctx, slog.LevelInfo, "msg", "k", "v")
	l.Log(ctx, slog.LevelInfo, "msg", 123, "v")
}
`
	joined := runVetOnSrc(t, src)
	if strings.Count(joined, "key should be a string") != 1 {
		t.Errorf("expected exactly 1 slog key diagnostic (the 123 key), got:\n%s", joined)
	}
}
