package main

// Regression tests for the rv18 (2026-10-18) independent-review findings
// in internal/frontend/mr_disk.go (multi-result desugar for disk-built
// packages). Each test failed before its fix and passes after; the
// per-test comment names the defect.

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"strings"
	"testing"
)

// 1. Switch-case hoist was a silent no-op: hoistNestedMRInStmt rewrote a
// throwaway &ast.BlockStmt{List: ccc.Body} and then rebuilt ccc.Body
// from the unchanged slice, so nested multi-result calls in case bodies
// were never hoisted.
func TestDiskMRSwitchCaseHoistNestedMR(t *testing.T) {
	checkDiskMRClean(t, `package p

func Two() (int, error) { return 1, nil }

func Must(v int, err error) int {
	if err != nil {
		return -1
	}
	return v
}

func f(x int) int {
	switch x {
	case 1:
		return Must(Two())
	}
	return 0
}
`)
}

// 2. hoistNestedMRInStmt had no *ast.TypeSwitchStmt case, so nested
// multi-result calls in type-switch case bodies were never hoisted.
// (Not observable via type-check: Must(Two()) is a valid multi-value
// spread. The defect is the missing hoist itself — the nested call
// would reach export as a call to the kept-for-resolution stub — so
// this test asserts the hoisted shape directly.)
func TestDiskMRTypeSwitchHoistNestedMR(t *testing.T) {
	rendered := transformAndRenderDiskMR(t, `package p

func Two() (int, error) { return 1, nil }

func Must(v int, err error) int {
	if err != nil {
		return -1
	}
	return v
}

func f(x any) int {
	switch x.(type) {
	case int:
		return Must(Two())
	}
	return 0
}
`)
	if !strings.Contains(rendered, "__mr_arg_") {
		t.Fatalf("nested MR call in type-switch case body was not hoisted:\n%s", rendered)
	}
}

// 3. rewriteReturn matched rs.Results[0].(*ast.CallExpr) directly, so
// return (Two()) (parenthesized) was never rewritten and left a
// multi-value call in the pair-returning helper's single-value return
// position.
func TestDiskMRParenthesizedReturnCall(t *testing.T) {
	checkDiskMRClean(t, `package p

func Two() (int, error) { return 1, nil }

func fwd() (int, error) {
	return (Two())
}
`)
}

// 4. The var-initializer hoist preserved the package qualifier,
// generating pkg.__Parse for var X = Must(pkg.Parse(a)) — the __-helper
// is unexported in pkg. Selector-fun inner calls must be skipped, the
// same policy hoistNestedMRExpr applies in argument position.
func TestDiskMRVarHoistSkipsSelectorFun(t *testing.T) {
	rendered := transformAndRenderDiskMR(t, `package p

func Parse(s string) (int, error) { return len(s), nil }

func Must(v int, err error) int {
	if err != nil {
		return -1
	}
	return v
}

var X = Must(other.Parse("a"))
`)
	if strings.Contains(rendered, "other.__Parse") {
		t.Fatalf("var hoist emitted unexported cross-package helper call other.__Parse:\n%s", rendered)
	}
	if strings.Contains(rendered, "__mr_hoist") {
		t.Fatalf("selector-fun inner call must not be hoisted:\n%s", rendered)
	}
}

// 5. rewriteStmt's LabeledStmt case dropped the label when a labeled
// return expanded to two statements — but the label may be a goto
// target.
func TestDiskMRLabeledReturnKeepsLabel(t *testing.T) {
	checkDiskMRClean(t, `package p

func Two() (int, error) { return 1, nil }

func fwd() (int, error) {
L:
	return Two()
	goto L
}
`)
}

// transformAndRenderDiskMR parses src, runs the disk multi-result
// transform, and renders the rewritten AST back to source.
func transformAndRenderDiskMR(t *testing.T, src string) string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	transformDiskMultiResult("example.com/p", []*ast.File{f})
	var sb strings.Builder
	if err := printer.Fprint(&sb, fset, f); err != nil {
		t.Fatalf("render error: %v", err)
	}
	return sb.String()
}
