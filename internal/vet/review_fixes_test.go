package vet

// Falsifiable tests for the REVIEW-2026-10-30-15 vet-analyzer fixes
// (N-P1-11..N-P1-16, N-P2-22, N-P2-34). Each test fails on the pre-fix
// code and passes after the fix; runVetOnSrc (checks_test.go) runs all
// analyzers over a single temp file.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// N-P1-11: Scanf-family arguments are pointers to the scanned values;
// valid scans must not be flagged, mistyped targets must be.
func TestScanfPrintf(t *testing.T) {
	good := `package main

import (
	"fmt"
	"strings"
)

func f() {
	var n int
	var s string
	var f64 float64
	_, _ = fmt.Scanf("%d %s %f", &n, &s, &f64)
	_, _ = fmt.Fscanf(strings.NewReader("42"), "%d", &n)
	_, _ = fmt.Sscanf("42", "%d", &n)
	_, _ = fmt.Scanf("%v", &n)
}
`
	joined := runVetOnSrc(t, good)
	for _, w := range []string{"expects integer", "expects string", "expects floating"} {
		if strings.Contains(joined, w) {
			t.Errorf("valid Scanf flagged with %q:\n%s", w, joined)
		}
	}

	bad := `package main

import "fmt"

func f() {
	var n int
	var s string
	_, _ = fmt.Scanf("%d", &s)
	_, _ = fmt.Scanf("%s", &n)
}
`
	joined = runVetOnSrc(t, bad)
	if !strings.Contains(joined, "expects integer") {
		t.Errorf("Scanf %%d into *string should report expects integer, got:\n%s", joined)
	}
	if !strings.Contains(joined, "expects string") {
		t.Errorf("Scanf %%s into *int should report expects string, got:\n%s", joined)
	}
}

// N-P1-12: func variables, parameters, and fields CAN be nil; only
// declared function/method values are never nil.
func TestNilFuncVariables(t *testing.T) {
	good := `package main

type H struct{ cb func() }

func use(cb func()) {
	if cb != nil {
		cb()
	}
}

func (h H) run() {
	if h.cb != nil {
		h.cb()
	}
}

func main() {
	var f func()
	if f != nil {
		f()
	}
	use(nil)
}
`
	joined := runVetOnSrc(t, good)
	if strings.Contains(joined, "comparison of function to nil") {
		t.Errorf("func variable/parameter/field comparison must not be flagged, got:\n%s", joined)
	}

	bad := `package main

import "fmt"

type T struct{}

func (T) M() {}

func helper() {}

func main() {
	var t T
	if helper == nil {
		fmt.Println("never")
	}
	if t.M == nil {
		fmt.Println("never")
	}
	if T.M == nil {
		fmt.Println("never")
	}
	if fmt.Println == nil {
		fmt.Println("never")
	}
}
`
	joined = runVetOnSrc(t, bad)
	if n := strings.Count(joined, "comparison of function to nil"); n != 4 {
		t.Errorf("expected 4 nilfunc diagnostics (declared func, method value, method expression, package func), got %d:\n%s", n, joined)
	}
}

// N-P1-13: slog must not panic on malformed calls, and Log/LogContext
// take (ctx, level, msg, args...) so level is not a key.
func TestSlogNoPanicAndLog(t *testing.T) {
	// Run must return, not panic, on 0-arg / short calls.
	// (Note: slog has no LogContext function or method — Log already
	// takes ctx — so the LogContext start=3 branch is inert for real
	// code; it is kept for brief compliance. Only real APIs are
	// exercised here.)
	good := `package main

import (
	"context"
	"log/slog"
)

func f() {
	ctx := context.Background()
	slog.Info()
	slog.InfoContext(ctx)
	slog.Log(ctx, slog.LevelInfo, "hi", "k", "v")
	slog.Default().Log(ctx, slog.LevelInfo, "hi", "k", "v")
}
`
	joined := runVetOnSrc(t, good)
	if strings.Contains(joined, "slog") {
		t.Errorf("valid/malformed-but-tolerated slog calls must not report slog diagnostics, got:\n%s", joined)
	}

	bad := `package main

import (
	"context"
	"log/slog"
)

func f() {
	ctx := context.Background()
	slog.Log(ctx, slog.LevelInfo, "hi", 123, "v")
	slog.Log(ctx, slog.LevelInfo, "hi", "k")
}
`
	joined = runVetOnSrc(t, bad)
	if !strings.Contains(joined, "should be a string") {
		t.Errorf("slog.Log with non-string key should be flagged, got:\n%s", joined)
	}
	if !strings.Contains(joined, "odd number") {
		t.Errorf("slog.Log with odd args should be flagged, got:\n%s", joined)
	}
}

// N-P1-14: direct assignment to an atomic value is misuse; Store/Load
// are fine.
func TestAtomicDirectAssignment(t *testing.T) {
	good := `package main

import "sync/atomic"

func f() {
	var v atomic.Int64
	v.Store(1)
	_ = v.Load()
}
`
	joined := runVetOnSrc(t, good)
	if strings.Contains(joined, "direct assignment to atomic") {
		t.Errorf("Store/Load use must not be flagged, got:\n%s", joined)
	}

	bad := `package main

import "sync/atomic"

type S struct{ v atomic.Int64 }

func f() {
	var v atomic.Int64
	var w atomic.Int64
	v = atomic.Int64{}
	v = w
	var s S
	s.v = atomic.Int64{}
}
`
	joined = runVetOnSrc(t, bad)
	if n := strings.Count(joined, "direct assignment to atomic"); n != 3 {
		t.Errorf("expected 3 atomic diagnostics (plain, var-to-var, field), got %d:\n%s", n, joined)
	}
}

// N-P1-15: copying a POINTER to a lock is safe; copying a lock value
// (directly or embedded in a struct) is not.
func TestCopyLocksPointer(t *testing.T) {
	good := `package main

import "sync"

type H struct{ Mu *sync.Mutex }

func take(p *sync.Mutex) { _ = p }

func f() {
	var mu sync.Mutex
	take(&mu)
	h := H{Mu: &mu}
	_ = h
}
`
	joined := runVetOnSrc(t, good)
	if strings.Contains(joined, "copies lock") {
		t.Errorf("pointer-to-lock copies must not be flagged, got:\n%s", joined)
	}

	bad := `package main

import "sync"

type V struct{ Mu sync.Mutex }

func f() {
	var m sync.Mutex
	x := m
	_ = x
	v := V{Mu: sync.Mutex{}}
	_ = v
}
`
	joined = runVetOnSrc(t, bad)
	// The analyzer flags assignments of lock values; the struct literal
	// itself is a value copy only when assigned/passed, so at least the
	// plain `x := m` copy must fire.
	if !strings.Contains(joined, "copies lock") {
		t.Errorf("lock value copy must be flagged, got:\n%s", joined)
	}
}

// N-P1-16: %s accepts Stringer/error and named []byte; %q accepts
// integers (quoted chars).
func TestPrintfStringerAndQuotedInt(t *testing.T) {
	good := `package main

import "fmt"

type S struct{}

func (S) String() string { return "s" }

type B []byte

func f(err error) {
	_ = fmt.Sprintf("%s", S{})
	_ = fmt.Sprintf("%s", err)
	_ = fmt.Sprintf("%s", B("x"))
	_ = fmt.Sprintf("%s", []byte("x"))
	n := 65
	_ = fmt.Sprintf("%q", n)
	_ = fmt.Sprintf("%q", 65)
	_ = fmt.Sprintf("%q", "hi")
}
`
	joined := runVetOnSrc(t, good)
	if strings.Contains(joined, "expects string") {
		t.Errorf("valid %%s/%%q uses must not be flagged, got:\n%s", joined)
	}

	bad := `package main

import "fmt"

func f() {
	_ = fmt.Sprintf("%s", 42)
	_ = fmt.Sprintf("%q", 3.14)
}
`
	joined = runVetOnSrc(t, bad)
	if !strings.Contains(joined, "expects string") {
		t.Errorf("%%s on int should be flagged, got:\n%s", joined)
	}
}

// N-P2-22: indexed verbs are type-checked, '*' precision counts as a
// consumed argument (before the verb's argument), and surplus
// arguments are reported.
func TestPrintfIndexedStarsSurplus(t *testing.T) {
	good := `package main

import "fmt"

func f() {
	_ = fmt.Sprintf("%[1]d %[2]s", 1, "x")
	_ = fmt.Sprintf("%.*f", 2, 3.14)
	_ = fmt.Sprintf("%*d", 5, 42)
	_ = fmt.Sprintf("%*.*f", 3, 2, 3.14)
}
`
	joined := runVetOnSrc(t, good)
	for _, w := range []string{"expects", "missing argument", "too many arguments"} {
		if strings.Contains(joined, w) {
			t.Errorf("valid indexed/star format flagged with %q:\n%s", w, joined)
		}
	}

	bad := `package main

import "fmt"

func f() {
	_ = fmt.Sprintf("%[1]d", "hello")
	_ = fmt.Sprintf("%*d", "wide", 42)
	_ = fmt.Sprintf("%d", 1, 2)
	_ = fmt.Sprintf("hello", 1)
}
`
	joined = runVetOnSrc(t, bad)
	if !strings.Contains(joined, "expects integer") {
		t.Errorf("indexed %%[1]d with string / star width with string should report expects integer, got:\n%s", joined)
	}
	if n := strings.Count(joined, "too many arguments"); n != 2 {
		t.Errorf("expected 2 surplus-argument diagnostics, got %d:\n%s", n, joined)
	}
}

// N-P2-34: vet.Run groups files by package clause, so a directory with
// both package foo and package foo_test files still gets checked.
func TestVetRunTestPackageGrouping(t *testing.T) {
	dir := t.TempDir()
	a := `package foo

func Add(a, b int) int { return a + b }
`
	bt := `package foo_test

import (
	"fmt"
	"testing"
)

func TestX(t *testing.T) {
	fmt.Printf("%d", "hello")
}
`
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(a), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a_test.go"), []byte(bt), 0644); err != nil {
		t.Fatal(err)
	}
	ds, err := Run([]string{filepath.Join(dir, "a.go"), filepath.Join(dir, "a_test.go")})
	if err != nil {
		t.Fatal(err)
	}
	var msgs []string
	for _, d := range ds {
		msgs = append(msgs, d.Message)
	}
	joined := strings.Join(msgs, "\n")
	if !strings.Contains(joined, "expects integer") {
		t.Errorf("printf diagnostic from foo_test file lost under mixed packages, got:\n%s", joined)
	}
}
