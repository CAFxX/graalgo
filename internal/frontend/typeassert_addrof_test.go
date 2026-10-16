package main

// TypeAssertExpr isAddressable: & on a selector through a type assertion.
//
// &(x.(T).f) is valid Go only when T is a pointer type (e.g. reflect's
// &mt.(*rtype).t): the field is accessed through the pointer, addressing
// real memory. When T is not a pointer, x.(T) is a copy and not
// addressable.
//
// The fix adds a *ast.TypeAssertExpr case to isAddressable that checks
// structurally via go/types whether the asserted type is a *types.Pointer.
// This is general, not keyed on any type or package name.
//
// Red-green: isAddressable returns false for both cases before the fix;
// after the fix, the pointer case returns true and the non-pointer case
// still returns false.

import (
	"go/ast"
	"testing"
)

// findSelector finds the first *ast.SelectorExpr in the AST.
func findSelector(f *ast.File) *ast.SelectorExpr {
	var found *ast.SelectorExpr
	ast.Inspect(f, func(n ast.Node) bool {
		if s, ok := n.(*ast.SelectorExpr); ok && found == nil {
			found = s
			return false
		}
		return true
	})
	return found
}

// TestTypeAssertPointerAddressable: &x.(*S).F — the type assertion is to
// a pointer type (*S), so the field access addresses real memory through
// the pointer. isAddressable must return true.
func TestTypeAssertPointerAddressable(t *testing.T) {
	src := `package main

type S struct{ F int }

func f(x interface{}) *int {
	return &x.(*S).F
}
`
	f, info := typeCheckPkg(t, src)
	u := findUnaryAddr(f)
	if u == nil {
		t.Fatal("no &x found in test program")
	}
	sel, ok := u.X.(*ast.SelectorExpr)
	if !ok {
		t.Fatalf("& operand is %T, want *ast.SelectorExpr", u.X)
	}
	// Verify the shape: the selector base is a type assertion.
	if _, ok := sel.X.(*ast.TypeAssertExpr); !ok {
		t.Fatalf("selector base is %T, want *ast.TypeAssertExpr", sel.X)
	}
	// The fix: isAddressable must return true for pointer type assertions.
	if !isAddressable(u.X, info) {
		t.Fatalf("isAddressable(&x.(*S).F operand) = false, want true")
	}
}

// TestTypeAssertNonPointerNotAddressable: x.(S).F where S is not a
// pointer — the type assertion result is a copy, not addressable.
// isAddressable must return false.
//
// Note: &x.(S).F is invalid Go (cannot take address of non-addressable
// value), so we test isAddressable directly on the selector without
// the & in the source.
func TestTypeAssertNonPointerNotAddressable(t *testing.T) {
	src := `package main

type S struct{ F int }

func f(x interface{}) int {
	// x.(S) is a copy; .F selects from the copy.
	return x.(S).F
}
`
	f, info := typeCheckPkg(t, src)
	sel := findSelector(f)
	if sel == nil {
		t.Fatal("no selector found in test program")
	}
	// Verify the shape: the selector base is a type assertion.
	if _, ok := sel.X.(*ast.TypeAssertExpr); !ok {
		t.Fatalf("selector base is %T, want *ast.TypeAssertExpr", sel.X)
	}
	// The fix must not over-approximate: non-pointer assertion results
	// are not addressable.
	if isAddressable(sel, info) {
		t.Fatalf("isAddressable(x.(S).F) = true, want false (non-pointer assertion result is a copy)")
	}
}
