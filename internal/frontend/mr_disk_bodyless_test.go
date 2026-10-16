package main

// Tests for body-less multi-result function handling in the disk
// multi-result transform (transformDiskMultiResult in mr_disk.go).
//
// Body-less declarations (e.g. //go:noescape assembly stubs like
// reflect's chanrecv) have no Go body to rewrite, but their call sites
// still need desugaring. The transform must generate:
//   1. The pair type (so the __-helper return type exists)
//   2. The stub (for type-checking the original signature)
//   3. A __-helper with a fail-fast panic body (the real implementation
//      is a backend intrinsic)
//
// Without this, the call site `selected, ok := chanrecv(...)` is not
// desugared, and the v1 frontend (which cannot emit multi-result calls)
// fails.
//
// Red-green: before the fix, transformDiskMultiResult skipped body-less
// functions (fd.Body == nil), so no spec was generated and the call site
// was not desugared.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// TestDiskMRBodylessMultiResult: a body-less multi-result function
// declaration gets a spec (pair type + helper + stub).
func TestDiskMRBodylessMultiResult(t *testing.T) {
	src := `package p

import "unsafe"

//go:noescape
func chanrecv(ch unsafe.Pointer, nb bool, val unsafe.Pointer) (selected, received bool)
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	specs := transformDiskMultiResult("example.com/p", []*ast.File{f})
	if len(specs) != 1 {
		t.Fatalf("got %d specs, want 1", len(specs))
	}
	s := specs[0]
	if s.name != "chanrecv" {
		t.Errorf("spec name = %q, want %q", s.name, "chanrecv")
	}
	if s.pairType != "__chanrecvPair" {
		t.Errorf("pairType = %q, want %q", s.pairType, "__chanrecvPair")
	}
	if s.helper != "__chanrecv" {
		t.Errorf("helper = %q, want %q", s.helper, "__chanrecv")
	}
	if len(s.pairFields) != 2 {
		t.Errorf("pairFields len = %d, want 2", len(s.pairFields))
	}
}

// TestDiskMRBodylessTypeChecks: the rewritten AST (pair + panic helper +
// stub) type-checks cleanly.
func TestDiskMRBodylessTypeChecks(t *testing.T) {
	checkDiskMRClean(t, `package p

import "unsafe"

//go:noescape
func chanrecv(ch unsafe.Pointer, nb bool, val unsafe.Pointer) (selected, received bool)
`)
}

// TestDiskMRBodylessCallSiteDesugared: a call to a body-less multi-result
// function is registered for desugaring (lookupDiskMR finds it).
func TestDiskMRBodylessCallSiteDesugared(t *testing.T) {
	src := `package p

import "unsafe"

//go:noescape
func chanrecv(ch unsafe.Pointer, nb bool, val unsafe.Pointer) (selected, received bool)
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	// Clear any existing specs for this package to avoid interference.
	diskMRMu.Lock()
	oldSpecs := diskMRSpecs
	diskMRSpecs = nil
	diskMRMu.Unlock()
	defer func() {
		diskMRMu.Lock()
		diskMRSpecs = oldSpecs
		diskMRMu.Unlock()
	}()

	specs := transformDiskMultiResult("example.com/p", []*ast.File{f})
	diskMRMu.Lock()
	diskMRSpecs = append(diskMRSpecs, specs...)
	diskMRMu.Unlock()

	spec := lookupDiskMR("example.com/p", "chanrecv", "")
	if spec == nil {
		t.Fatal("lookupDiskMR(example.com/p, chanrecv, \"\") = nil, want spec")
	}
	if spec.helper != "__chanrecv" {
		t.Errorf("spec helper = %q, want __chanrecv", spec.helper)
	}
}

// TestDiskMRBodylessNegative: single-result body-less functions are NOT
// transformed (no spec generated).
func TestDiskMRBodylessNegative(t *testing.T) {
	src := `package p

import "unsafe"

//go:noescape
func chansend0(ch unsafe.Pointer, val unsafe.Pointer, nb bool) bool
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	specs := transformDiskMultiResult("example.com/p", []*ast.File{f})
	if len(specs) != 0 {
		t.Fatalf("got %d specs for single-result body-less func, want 0", len(specs))
	}
}
