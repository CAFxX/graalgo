package vet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Helper: run all analyzers over one source file and return joined messages.
func runVetSrc(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "x.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	ds, err := Run([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	var msgs []string
	for _, d := range ds {
		msgs = append(msgs, d.Message)
	}
	return strings.Join(msgs, "\n")
}

func TestPrintfPrecisionStar(t *testing.T) {
	// Positive: star args consumed before the value; no diagnostics expected
	// from the printf analyzer for these well-typed calls.
	good := `package main

import "fmt"

func f() {
	_ = fmt.Sprintf("%.*f", 2, 3.14)
	_ = fmt.Sprintf("%*.*f", 5, 2, 3.14)
	_ = fmt.Sprintf("%*s", 5, "hi")
	_ = fmt.Sprintf("%*d", 5, 42)
	_ = fmt.Sprintf("%d %.*f %s", 1, 2, 3.14, "tail")
}
`
	if got := runVetSrc(t, good); strings.Contains(got, "expects") ||
		strings.Contains(got, "missing argument") {
		t.Errorf("well-typed star calls flagged:\n%s", got)
	}

	// Negative: value after the star(s) has the wrong type.
	bad := `package main

import "fmt"

func f() {
	_ = fmt.Sprintf("%.*f", 2, "bad")
	_ = fmt.Sprintf("%*.*f", 5, 2, "bad")
	_ = fmt.Sprintf("%*s", 5, 42)
}
`
	got := runVetSrc(t, bad)
	for _, want := range []string{
		`%.*f: expects floating point`,
		`%*.*f: expects floating point`,
		`%*s: expects string or []byte`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected diagnostic %q, got:\n%s", want, got)
		}
	}

	// Negative: a non-integer star argument is itself wrong.
	badStar := `package main

import "fmt"

func f() {
	_ = fmt.Sprintf("%.*f", "bad", 3.14)
	_ = fmt.Sprintf("%*d", "bad", 42)
}
`
	if got := runVetSrc(t, badStar); !strings.Contains(got, "expects integer") {
		t.Errorf("expected non-integer star arg flagged, got:\n%s", got)
	}
}

func TestPrintfStringerAndError(t *testing.T) {
	// Positive: Stringer and error values are legal for %s and %q.
	good := `package main

import "fmt"

type S struct{}

func (S) String() string { return "s" }

func f(err error) {
	_ = fmt.Sprintf("%s", S{})
	_ = fmt.Sprintf("%q", S{})
	_ = fmt.Sprintf("%s", err)
	_ = fmt.Sprintf("%q", err)
	_ = fmt.Sprintf("%s", "plain")
	_ = fmt.Sprintf("%s", []byte("b"))
}
`
	if got := runVetSrc(t, good); strings.Contains(got, "expects string") {
		t.Errorf("Stringer/error for %%s/%%q flagged:\n%s", got)
	}

	// Negative: plain non-string, non-Stringer types are still flagged.
	bad := `package main

import "fmt"

func f() {
	_ = fmt.Sprintf("%s", 42)
	_ = fmt.Sprintf("%q", true)
}
`
	got := runVetSrc(t, bad)
	if strings.Count(got, "expects string or []byte") != 2 {
		t.Errorf("expected 2 string diagnostics, got:\n%s", got)
	}
}

func TestLoopClosureAssignedVars(t *testing.T) {
	// Negative: every assigned loop-variable form must be flagged when a
	// closure captures it.
	bad := `package main

func f(xs []int) {
	var i int
	for i = 0; i < 3; i++ {
		go func() { _ = i }()
	}
	var k, v int
	for k, v = range xs {
		go func() { _ = k }()
	}
	for k, v = range xs {
		go func() { _ = v }()
	}
	var j, m int
	for j, m = 0, 0; j < 3; j++ {
		go func() { _ = m }()
	}
}
`
	got := runVetSrc(t, bad)
	for _, want := range []string{
		"loop variable i captured by func literal",
		"loop variable k captured by func literal",
		"loop variable v captured by func literal",
		"loop variable m captured by func literal",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected diagnostic %q, got:\n%s", want, got)
		}
	}

	// Positive: per-iteration (:=) loop variables and closures that do not
	// reference the loop variable must not be flagged.
	good := `package main

func f(xs []int) {
	for k, v := range xs {
		go func() { _ = k; _ = v }()
	}
	for i := 0; i < 3; i++ {
		go func() { _ = i }()
	}
	var i int
	for i = 0; i < 3; i++ {
		go func() { _ = 1 }()
	}
}
`
	if got := runVetSrc(t, good); strings.Contains(got, "captured by func literal") {
		t.Errorf("per-iteration / non-capturing closures flagged:\n%s", got)
	}
}
