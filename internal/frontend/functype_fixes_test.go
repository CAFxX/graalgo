package main

import (
	"go/ast"
	"go/types"
	"testing"
)

// TestFuncTypeConversionIsType: (func())(nil) is a type conversion, not a
// call to a computed func value. The Fun denotes a TYPE (IsType==true),
// so exportCall must route it to the conversion handling, not the
// "computed func value" branch (which would pass the FuncType to
// exportExpr and fail).
//
// This pattern appears in reflect/type.go:1921:
//
//	var ifunc any = (func())(nil)
func TestFuncTypeConversionIsType(t *testing.T) {
	src := `package p
func f() any {
	var ifunc any = (func())(nil)
	return ifunc
}
`
	f, info := typeCheckPkg(t, src)
	var found *ast.CallExpr
	ast.Inspect(f, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if _, ok := call.Fun.(*ast.ParenExpr); ok {
				if tv, ok := info.Types[call.Fun]; ok && tv.IsType() {
					found = call
					return false
				}
			}
		}
		return true
	})
	if found == nil {
		t.Fatal("did not find (func())(nil) conversion")
	}
	tv := info.Types[found.Fun]
	if !tv.IsType() {
		t.Errorf("expected IsType()==true for func type conversion target")
	}
	if tv.IsValue() {
		t.Errorf("expected IsValue()==false for func type conversion target")
	}
	// The type must be a signature (this is what confused the old code)
	if _, ok := tv.Type.Underlying().(*types.Signature); !ok {
		t.Errorf("expected func type to have signature underlying")
	}
}

// TestIsAddressableParenExpr: (x) has the same addressability as x.
// Parentheses are transparent for addressability purposes.
//
// This pattern appears in reflect/type.go:2707:
//
//	array.Slice = &(SliceOf(elem).(*rtype).t)
//
// The & is applied to a parenthesized selector expression.
func TestIsAddressableParenExpr(t *testing.T) {
	src := `package p
type S struct{ F int }
func f(s *S) *int {
	return &(s.F)
}
`
	f, info := typeCheckPkg(t, src)
	// Find the & unary expr
	unary := findUnaryAddr(f)
	if unary == nil {
		t.Fatal("did not find & expression")
	}
	// The operand should be a ParenExpr
	if _, ok := unary.X.(*ast.ParenExpr); !ok {
		t.Fatalf("expected ParenExpr operand, got %T", unary.X)
	}
	// It should be addressable (parens are transparent)
	if !isAddressable(unary.X, info) {
		t.Errorf("expected (s.F) to be addressable")
	}
}

// TestIsAddressableParenExprNegative: ((x)) where x is not addressable
// should not be addressable. Parens don't make non-addressable things
// addressable.
func TestIsAddressableParenExprNegative(t *testing.T) {
	src := `package p
func f() int {
	return (42)
}
`
	f, info := typeCheckPkg(t, src)
	var paren *ast.ParenExpr
	ast.Inspect(f, func(n ast.Node) bool {
		if p, ok := n.(*ast.ParenExpr); ok && paren == nil {
			paren = p
			return false
		}
		return true
	})
	if paren == nil {
		t.Fatal("did not find parenthesized expression")
	}
	// (42) is not addressable — 42 is a constant
	if isAddressable(paren, info) {
		t.Errorf("expected (42) to NOT be addressable")
	}
}
