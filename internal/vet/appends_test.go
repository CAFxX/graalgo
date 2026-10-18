package vet

import (
	"strings"
	"testing"
)

// RV18-P1-21(3): runAppends indexed call.Args[0] without a length check.
// Probing showed go/types does not record a type for an arg-count-mismatched
// builtin call, so the tv.Type != nil check incidentally prevents the panic
// today — but the analyzer's safety must not depend on that incidental
// property. The explicit length guard is defense-in-depth: a static analyzer
// must never crash on its input. This test pins the no-crash contract.
func TestAppendsEmptyCallNoCrash(t *testing.T) {
	src := `package main

func f() {
	append()
}
`
	// Must return without panicking; no diagnostic is expected (the type
	// error is not this analyzer's business).
	_ = runVetOnSrc(t, src)
}

func TestAppendsDiscardedResult(t *testing.T) {
	src := `package main

func f() {
	s := []int{1}
	append(s, 2)
	_ = append(s, 3)
}
`
	joined := runVetOnSrc(t, src)
	if n := strings.Count(joined, "is not used"); n != 1 {
		t.Errorf("expected exactly 1 append diagnostic, got %d:\n%s", n, joined)
	}
}
