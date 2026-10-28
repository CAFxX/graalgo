package main

// Regression tests for the disk multi-result transform dropping
// CallExpr.Ellipsis when it rebuilds a call to a multi-result function
// as a call to its __-helper (fix-redmaster-r40).
//
// The reported instance was fmt.Println:
//
//	func Println(a ...any) (n int, err error) {
//		return Fprintln(os.Stdout, a...)
//	}
//
// rewriteReturn rebuilt the return's call as __Fprintln(os.Stdout, a)
// with Ellipsis lost. The exporter then saw a non-spread variadic call
// whose single trailing argument is the []any slice itself and packed
// it into []any{a} — the callee received one element (the whole slice)
// instead of the slice's elements. Every fmt.Print* entry point that
// forwards a spread through a multi-result return was miscompiled the
// same way.
//
// The wrapped form still type-checks ([]any is a valid `any` element),
// so these tests assert on the rewritten AST: the __-helper call must
// carry a valid Ellipsis whenever the original call was a spread.
// All three arg-forwarding rebuild sites are covered:
//  1. rewriteReturn, single-call return (the Println shape).
//  2. The package-level value-spec hoist (var v, e = F(xs...)).
//  3. hoistNestedMRExpr, an MR call nested as an argument.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// findHelperCall returns the first call to the named __-helper in the
// file's declarations, or nil.
func findHelperCall(f *ast.File, helper string) *ast.CallExpr {
	var found *ast.CallExpr
	for _, d := range f.Decls {
		ast.Inspect(d, func(n ast.Node) bool {
			if found != nil {
				return false
			}
			if call, ok := n.(*ast.CallExpr); ok {
				if id, ok := call.Fun.(*ast.Ident); ok && id.Name == helper {
					found = call
					return false
				}
			}
			return true
		})
		if found != nil {
			break
		}
	}
	return found
}

func transformParse(t *testing.T, src string) *ast.File {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	transformDiskMultiResult("example.com/p", []*ast.File{f})
	return f
}

func TestDiskMRSpreadReturnPreservesEllipsis(t *testing.T) {
	// The fmt.Println shape: a multi-result function whose body is a
	// single return of a variadic-spread call to another multi-result
	// function.
	f := transformParse(t, `package p

func Inner(w int, a ...any) (int, error) { return len(a), nil }

func Outer(w int, a ...any) (int, error) { return Inner(w, a...) }
`)
	call := findHelperCall(f, "__Inner")
	if call == nil {
		t.Fatalf("no __Inner helper call in rewritten AST")
	}
	if !call.Ellipsis.IsValid() {
		t.Fatalf("helper call lost the variadic spread: Ellipsis invalid, " +
			"exporter will pack the slice as a single element")
	}
	if len(call.Args) != 2 {
		t.Fatalf("helper call has %d args, want 2 (w, a)", len(call.Args))
	}
}

func TestDiskMRSpreadVarHoistPreservesEllipsis(t *testing.T) {
	// Package-level var spec whose initializer nests a spread call to
	// an MR function: the transform hoists it to
	// var __mr_hoist_0 = __Inner(xs...).
	f := transformParse(t, `package p

func Inner(a ...any) (int, error) { return len(a), nil }

func Sink(n int, err error) int { return n }

var xs = []any{1, 2}

var v = Sink(Inner(xs...))
`)
	call := findHelperCall(f, "__Inner")
	if call == nil {
		t.Fatalf("no __Inner helper call in rewritten AST")
	}
	if !call.Ellipsis.IsValid() {
		t.Fatalf("hoisted helper call lost the variadic spread")
	}
}

func TestDiskMRSpreadNestedArgPreservesEllipsis(t *testing.T) {
	// An MR call with a spread, nested as an argument to another call.
	f := transformParse(t, `package p

func Inner(a ...any) (int, error) { return len(a), nil }

func Sink(n int, err error) int { return n }

var xs = []any{1, 2}

func Use() int { return Sink(Inner(xs...)) }
`)
	call := findHelperCall(f, "__Inner")
	if call == nil {
		t.Fatalf("no __Inner helper call in rewritten AST")
	}
	if !call.Ellipsis.IsValid() {
		t.Fatalf("nested helper call lost the variadic spread")
	}
}

func TestDiskMRNonSpreadCallKeepsNoEllipsis(t *testing.T) {
	// Negative: a plain (packed) variadic call must NOT gain an
	// Ellipsis from the transform — the exporter packs its elements.
	f := transformParse(t, `package p

func Inner(w int, a ...any) (int, error) { return len(a), nil }

func Outer(w int) (int, error) { return Inner(w, 1, 2) }
`)
	call := findHelperCall(f, "__Inner")
	if call == nil {
		t.Fatalf("no __Inner helper call in rewritten AST")
	}
	if call.Ellipsis.IsValid() {
		t.Fatalf("non-spread call gained an Ellipsis in the transform")
	}
}
