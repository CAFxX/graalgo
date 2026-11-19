package main

// F2 (REVIEW-2026-11-19-big-scan): the disk-MR __-helper path had the
// same defect as the user pair-transform — the helper packs the pair at
// the `return` statement, before deferred functions run, so deferred
// writes to named results (fmt's `defer errorHandler(&err)` in doScanf)
// were silently lost and Sscanf returned err=<nil> for every failing
// scan.
//
// The fix, pinned here at the rewritten-AST shape (type-checking the
// rewritten AST alone is not discriminating: the pre-fix tree also
// type-checked and lost the values only at runtime):
//   1. In a helper whose body has an own-level defer and at least one
//      used named result, the pair result is named `__mrret` and a
//      synthetic deferred repack closure — registered first, so LIFO
//      runs it last — re-packs the named-result locals into the pair
//      after the user's defers run.
//   2. Explicit returns land in the named-result locals first (via
//      __mr_ret<seq>_<i> temps, so each value expression is evaluated
//      exactly once, in order).
//   3. A helper with no own-level defer, or with no used named result,
//      is unchanged: unnamed pair result, no repack, no temps.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// diskMRHelper returns the __-helper FuncDecl named helper for the
// transformed src, or fails the test.
func diskMRHelper(t *testing.T, src, helper string) *ast.FuncDecl {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	transformDiskMultiResult("example.com/p", []*ast.File{f})
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == helper {
			return fd
		}
	}
	t.Fatalf("no helper %s in transformed decls", helper)
	return nil
}

// diskMRHelperResultName returns the (single) result name of the helper,
// "" when the result is unnamed.
func diskMRHelperResultName(t *testing.T, fd *ast.FuncDecl) string {
	t.Helper()
	if fd.Type.Results == nil || len(fd.Type.Results.List) != 1 {
		t.Fatalf("helper %s has %d result fields, want 1", fd.Name.Name, len(fd.Type.Results.List))
	}
	names := fd.Type.Results.List[0].Names
	if len(names) == 0 {
		return ""
	}
	return names[0].Name
}

// diskMRHasRepack reports whether the helper body contains the synthetic
// repack defer: a defer of a funclit whose body assigns to __mrret.
func diskMRHasRepack(t *testing.T, fd *ast.FuncDecl) bool {
	t.Helper()
	found := false
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if found {
			return false
		}
		if ds, ok := n.(*ast.DeferStmt); ok {
			if call, ok := ds.Call.Fun.(*ast.FuncLit); ok {
				for _, s := range call.Body.List {
					if as, ok := s.(*ast.AssignStmt); ok {
						for _, lhs := range as.Lhs {
							if id, ok := lhs.(*ast.Ident); ok && id.Name == "__mrret" {
								found = true
								return false
							}
						}
					}
				}
			}
		}
		return true
	})
	return found
}

// diskMRHasTempPrefix reports whether the helper body contains a
// declaration (var or :=) of a temp with the given prefix.
func diskMRHasTempPrefix(t *testing.T, fd *ast.FuncDecl, prefix string) bool {
	t.Helper()
	found := false
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if found {
			return false
		}
		if as, ok := n.(*ast.AssignStmt); ok && as.Tok == token.DEFINE {
			for _, lhs := range as.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && strings.HasPrefix(id.Name, prefix) {
					found = true
					return false
				}
			}
		}
		if ds, ok := n.(*ast.DeclStmt); ok {
			if gd, ok := ds.Decl.(*ast.GenDecl); ok && gd.Tok == token.VAR {
				for _, sp := range gd.Specs {
					if vs, ok := sp.(*ast.ValueSpec); ok {
						for _, name := range vs.Names {
							if strings.HasPrefix(name.Name, prefix) {
								found = true
								return false
							}
						}
					}
				}
			}
		}
		return true
	})
	return found
}

const diskMRDeferExplicitSrc = `package p

import "errors"

func f(n int) (m int, err error) {
	defer func() { err = errors.New("defer") }()
	return n + 1, nil
}
`

// TestDiskMRDeferRepackExplicit: a deferring helper with an explicit
// return must carry the repack defer and the step-1 temps, and its pair
// result must be named __mrret. Fails on the pre-fix tree: unnamed
// result, no repack, and the return packs the pair directly.
func TestDiskMRDeferRepackExplicit(t *testing.T) {
	checkDiskMRClean(t, diskMRDeferExplicitSrc)
	fd := diskMRHelper(t, diskMRDeferExplicitSrc, "__f")
	if got := diskMRHelperResultName(t, fd); got != "__mrret" {
		t.Fatalf("helper result name = %q, want __mrret", got)
	}
	if !diskMRHasRepack(t, fd) {
		t.Fatal("no repack defer in deferring helper __f")
	}
	if !diskMRHasTempPrefix(t, fd, "__mr_ret") {
		t.Fatal("no __mr_ret temp in deferring helper __f (explicit return did not hoist values)")
	}
}

const diskMRDeferBareSrc = `package p

import "errors"

func f() (n int, err error) {
	defer func() { err = errors.New("defer") }()
	n = 7
	return
}
`

// TestDiskMRDeferRepackBare: a deferring helper with a bare return must
// carry the repack defer (the bare return packs the locals, but a later
// defer may still rewrite them). No __mr_ret temps are needed.
func TestDiskMRDeferRepackBare(t *testing.T) {
	checkDiskMRClean(t, diskMRDeferBareSrc)
	fd := diskMRHelper(t, diskMRDeferBareSrc, "__f")
	if got := diskMRHelperResultName(t, fd); got != "__mrret" {
		t.Fatalf("helper result name = %q, want __mrret", got)
	}
	if !diskMRHasRepack(t, fd) {
		t.Fatal("no repack defer in deferring helper __f (bare return)")
	}
	if diskMRHasTempPrefix(t, fd, "__mr_ret") {
		t.Fatal("unexpected __mr_ret temp in bare-return helper __f")
	}
}

const diskMRNoDeferSrc = `package p

func f(n int) (m int, err error) {
	return n + 1, nil
}
`

// TestDiskMRNoRepackWithoutDefer: a helper with no own-level defer is
// unchanged — unnamed pair result, no repack, no temps.
func TestDiskMRNoRepackWithoutDefer(t *testing.T) {
	checkDiskMRClean(t, diskMRNoDeferSrc)
	fd := diskMRHelper(t, diskMRNoDeferSrc, "__f")
	if got := diskMRHelperResultName(t, fd); got != "" {
		t.Fatalf("helper result name = %q, want unnamed", got)
	}
	if diskMRHasRepack(t, fd) {
		t.Fatal("unexpected repack defer in helper __f with no defer")
	}
	if diskMRHasTempPrefix(t, fd, "__mr_ret") {
		t.Fatal("unexpected __mr_ret temp in helper __f with no defer")
	}
}

const diskMRDeferUnnamedSrc = `package p

import "errors"

func f(n int) (int, error) {
	defer func() { _ = errors.New("defer") }()
	return n + 1, nil
}
`

// TestDiskMRNoRepackUnnamedResults: a helper with no named results has
// no locals to re-pack, so no repack even with a defer.
func TestDiskMRNoRepackUnnamedResults(t *testing.T) {
	checkDiskMRClean(t, diskMRDeferUnnamedSrc)
	fd := diskMRHelper(t, diskMRDeferUnnamedSrc, "__f")
	if got := diskMRHelperResultName(t, fd); got != "" {
		t.Fatalf("helper result name = %q, want unnamed", got)
	}
	if diskMRHasRepack(t, fd) {
		t.Fatal("unexpected repack defer in helper __f with unnamed results")
	}
}

const diskMRDeferLiteralSrc = `package p

import "errors"

func f() (n int, err error) {
	h := func() {
		defer func() { err = errors.New("inner") }()
	}
	h()
	n = 15
	return
}
`

// TestDiskMRNoRepackLiteralDefer: a defer inside a nested func literal
// belongs to the literal, not the helper — no repack for the helper.
func TestDiskMRNoRepackLiteralDefer(t *testing.T) {
	checkDiskMRClean(t, diskMRDeferLiteralSrc)
	fd := diskMRHelper(t, diskMRDeferLiteralSrc, "__f")
	if got := diskMRHelperResultName(t, fd); got != "" {
		t.Fatalf("helper result name = %q, want unnamed", got)
	}
	if diskMRHasRepack(t, fd) {
		t.Fatal("unexpected repack defer in helper __f (its only defer is inside a nested func literal)")
	}
}
