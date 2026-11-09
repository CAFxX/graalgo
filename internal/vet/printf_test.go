package vet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runVetSrc runs all analyzers over a single temporary source file and
// returns the diagnostic messages.
func runVetSrc(t *testing.T, src string) []Diagnostic {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "src.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	ds, err := Run([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	return ds
}

func diagnosticsContaining(ds []Diagnostic, substr string) []Diagnostic {
	var out []Diagnostic
	for _, d := range ds {
		if strings.Contains(d.Message, substr) {
			out = append(out, d)
		}
	}
	return out
}

func dumpDiagnostics(t *testing.T, ds []Diagnostic) {
	t.Helper()
	for _, d := range ds {
		t.Logf("%s: %s", d.Pos, d.Message)
	}
}

// R05-V-02: %s/%q must accept error values, fmt.Stringer values, and
// named []byte (and named string) types — all valid uses of the verbs.
func TestPrintfStringVerbAcceptsValidTypes(t *testing.T) {
	src := `package main

import "fmt"

type MyString string
type MyBytes []byte
type MyArr [3]byte

type Stringer struct{}

func (Stringer) String() string { return "s" }

type PtrStringer struct{}

func (*PtrStringer) String() string { return "s" }

func main() {
	var err error
	var s string
	var ms MyString
	var b []byte
	var mb MyBytes
	var arrB [3]byte
	var ma MyArr
	var st Stringer
	var pst PtrStringer
	_ = fmt.Sprintf("%s", err)
	_ = fmt.Sprintf("%s", s)
	_ = fmt.Sprintf("%s", ms)
	_ = fmt.Sprintf("%s", b)
	_ = fmt.Sprintf("%q", mb)
	_ = fmt.Sprintf("%s", st)
	_ = fmt.Sprintf("%s", pst)
	_ = fmt.Sprintf("%s", &st)
	_ = fmt.Sprintf("%s", &pst)
	_ = fmt.Sprintf("%q", err)
	_ = fmt.Sprintf("%q", st)
	_ = fmt.Sprintf("%s", arrB)
	_ = fmt.Sprintf("%q", ma)
	_ = fmt.Sprintf("%s", &b)
	_ = fmt.Sprintf("%q", &mb)
	_ = fmt.Sprintf("%s", &arrB)
}
`
	ds := runVetSrc(t, src)
	if bad := diagnosticsContaining(ds, "expects string"); len(bad) != 0 {
		dumpDiagnostics(t, ds)
		t.Errorf("expected no \"expects string\" diagnostics, got %d", len(bad))
	}
}

// R05-V-02 (negative control): %s/%q with genuinely wrong types must
// still be flagged.
func TestPrintfStringVerbStillRejectsInvalidTypes(t *testing.T) {
	src := `package main

import "fmt"

type Plain struct{ X int }

type MyString string

func main() {
	var n int
	var ok bool
	var p Plain
	var ints []int
	var s string
	var ms MyString
	var runes []rune
	var b []byte
	pb := &b
	_ = fmt.Sprintf("%s", n)
	_ = fmt.Sprintf("%s", ok)
	_ = fmt.Sprintf("%s", p)
	_ = fmt.Sprintf("%s", ints)
	_ = fmt.Sprintf("%q", n)
	_ = fmt.Sprintf("%s", &s)
	_ = fmt.Sprintf("%s", &ms)
	_ = fmt.Sprintf("%s", runes)
	_ = fmt.Sprintf("%s", &pb)
}
`
	ds := runVetSrc(t, src)
	if got := diagnosticsContaining(ds, "expects string or []byte"); len(got) != 9 {
		dumpDiagnostics(t, ds)
		t.Errorf("expected 9 \"expects string or []byte\" diagnostics, got %d", len(got))
	}
}

// R05-V-06: a width or precision '*' consumes an argument, so the verb
// must be checked against the value argument that follows the star
// arguments — not against the star argument itself.
func TestPrintfStarArgsConsumeArguments(t *testing.T) {
	src := `package main

import "fmt"

func main() {
	_ = fmt.Sprintf("%.*f", 2, 3.14)
	_ = fmt.Sprintf("%*.*f", 6, 2, 3.14)
	_ = fmt.Sprintf("%*d", 5, 42)
	_ = fmt.Sprintf("%.*f %d", 2, 3.14, 7)
	_ = fmt.Sprintf("%*.*s %d", 5, 3, "hi", 9)
	_ = fmt.Sprintf("%*s", 5, "hi")
}
`
	ds := runVetSrc(t, src)
	for _, substr := range []string{"expects floating point", "expects string", "expects integer", "missing argument"} {
		if bad := diagnosticsContaining(ds, substr); len(bad) != 0 {
			dumpDiagnostics(t, ds)
			t.Errorf("expected no %q diagnostics, got %d", substr, len(bad))
		}
	}
}

// R05-V-06 (alignment proof): with a precision '*', a wrong-typed value
// argument must be flagged at the *value* argument's position — not at
// the precision argument. The value is on its own line so the reported
// line discriminates red (precision-arg line) from green (value-arg line).
func TestPrintfPrecisionStarChecksValueArgument(t *testing.T) {
	src := `package main

import "fmt"

func main() {
	_ = fmt.Sprintf("%.*f",
		2,
		"x")
}
`
	ds := runVetSrc(t, src)
	got := diagnosticsContaining(ds, "expects floating point")
	if len(got) != 1 {
		dumpDiagnostics(t, ds)
		t.Fatalf("expected exactly 1 \"expects floating point\" diagnostic, got %d", len(got))
	}
	// "%.*f" is on line 6, the precision arg 2 on line 7, the value "x" on line 8.
	if got[0].Pos.Line != 8 {
		dumpDiagnostics(t, ds)
		t.Errorf("expected diagnostic at line 8 (the value argument), got line %d", got[0].Pos.Line)
	}
}

// R05-V-06 (missing-arg proof): star arguments count toward the total,
// so a verb after a star-consuming verb with too few arguments left is
// a missing argument.
func TestPrintfStarArgsCountTowardMissingArgument(t *testing.T) {
	src := `package main

import "fmt"

func main() {
	_ = fmt.Sprintf("%.*f %d", 2, 3.14)
	_ = fmt.Sprintf("%*.*f", 6, 2)
}
`
	ds := runVetSrc(t, src)
	if got := diagnosticsContaining(ds, "missing argument"); len(got) != 2 {
		dumpDiagnostics(t, ds)
		t.Errorf("expected 2 \"missing argument\" diagnostics, got %d", len(got))
	}
}
