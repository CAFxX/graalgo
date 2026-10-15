package main

// Regression test for the spurious "declared and not used" on named
// results assigned in defer closures. The transform reuses *ast.Ident
// nodes for the helper's var declarations; sharing them with the stub's
// named results confused go/types when the use was inside a closure.

import (
	"testing"
)

// TestMRTransformDeferClosureAssign: a named result assigned only in a
// deferred closure (testify's getLen shape) must not trip "declared and
// not used".
func TestMRTransformDeferClosureAssign(t *testing.T) {
	src := `package p

func getLen(x interface{}) (length int, ok bool) {
	defer func() {
		ok = recover() == nil
	}()
	return 0, true
}
`
	if err := checkMRTransform(t, src); err != nil {
		t.Fatalf("type-check after transform: %v", err)
	}
}

// TestMRTransformGoClosureAssign: same shape via go statement.
func TestMRTransformGoClosureAssign(t *testing.T) {
	src := `package p

func f(ch chan int) (n int, ok bool) {
	go func() {
		ch <- 1
		ok = true
	}()
	return 0, false
}
`
	if err := checkMRTransform(t, src); err != nil {
		t.Fatalf("type-check after transform: %v", err)
	}
}

// TestMRTransformNestedClosureAssign: named result assigned in a nested
// (non-defer/go) closure literal.
func TestMRTransformNestedClosureAssign(t *testing.T) {
	src := `package p

func f() (x int, y int) {
	g := func() {
		y = 42
	}
	g()
	return 1, 2
}
`
	if err := checkMRTransform(t, src); err != nil {
		t.Fatalf("type-check after transform: %v", err)
	}
}
