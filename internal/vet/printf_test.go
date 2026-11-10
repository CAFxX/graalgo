package vet

import (
	"strings"
	"testing"
)

// V-P1-5: a precision star consumes an argument of its own. Width `*`
// sets starWidth, but precision `.*` was consumed without setting any
// flag, so "%.*f" was treated as taking one arg and the valid precision
// argument was type-checked as the float verb argument.
func TestPrintfPrecisionStar(t *testing.T) {
	src := `package main

import "fmt"

func f() {
	_ = fmt.Sprintf("%.*f", 2, 3.14)     // good: precision star takes an int arg
	_ = fmt.Sprintf("%*.*f", 5, 2, 3.14) // good: width + precision stars
	_ = fmt.Sprintf("%.*f", 3.14)        // bad: missing the precision arg
	_ = fmt.Sprintf("%*.*f", 5, 3.14)    // bad: missing the precision arg
}
`
	joined := runVetOnSrc(t, src)
	if count := strings.Count(joined, "missing argument"); count != 2 {
		t.Errorf("expected exactly 2 missing-argument diagnostics, got %d:\n%s", count, joined)
	}
	if strings.Contains(joined, "expects floating point") {
		t.Errorf("precision-star arg falsely checked as float (V-P1-5):\n%s", joined)
	}
}

// V-P1-6: %s/%q accept Stringer and error arguments, as upstream does.
func TestPrintfStringer(t *testing.T) {
	src := `package main

import "fmt"

type S struct{}

func (S) String() string { return "s" }

type E struct{}

func (E) Error() string { return "e" }

type P struct{}

func (*P) String() string { return "p" }

func f() {
	_ = fmt.Sprintf("%s", S{}) // good: Stringer
	_ = fmt.Sprintf("%q", S{}) // good: Stringer
	_ = fmt.Sprintf("%s", E{}) // good: error
	_ = fmt.Sprintf("%q", E{}) // good: error
	p := &P{}
	_ = fmt.Sprintf("%s", p)  // good: *P implements Stringer
	_ = fmt.Sprintf("%s", 42) // bad: int is not a string
}
`
	joined := runVetOnSrc(t, src)
	if count := strings.Count(joined, "expects string or []byte"); count != 1 {
		t.Errorf("expected exactly 1 string-verb diagnostic, got %d:\n%s", count, joined)
	}
}

// V-P1-7: extra arguments are reported when non-indexed verbs consume
// fewer args than supplied. Formats using explicit indexes are exempt:
// an indexed verb does not advance the sequential counter, so the
// extra-arg check cannot tell which args were consumed.
func TestPrintfExtraArgs(t *testing.T) {
	src := `package main

import "fmt"

func f() {
	_ = fmt.Sprintf("%d", 1, 2)     // bad: extra arg
	_ = fmt.Sprintf("hello", 1)     // bad: arg but no verbs
	_ = fmt.Sprintf("%d", 1)        // good
	_ = fmt.Sprintf("%d %s", 1, "x") // good
	_ = fmt.Sprintf("%[1]d", 1, 2)  // indexed: extra-arg check skipped by design
	_ = fmt.Sprintf("%%", 1)        // bad: %% takes no arg
}
`
	joined := runVetOnSrc(t, src)
	if count := strings.Count(joined, "too many arguments"); count != 3 {
		t.Errorf("expected exactly 3 too-many-arguments diagnostics, got %d:\n%s", count, joined)
	}
}
