package main

// PKG-082: & on *ast.SelectorExpr — address-of a field selected through a
// pointer-typed base that is itself not addressable (e.g. &mt.(*rtype).t
// in reflect/type.go, &f().N for a call returning a pointer). x.f where x
// has pointer-to-struct type is shorthand for (*x).f, and *p is always
// addressable — so isAddressable must consult the base's TYPE, not just
// recurse on the base expression. This was the red-master root cause:
// every corpus program failed buildOk because the real reflect package
// (compiled from GOROOT via the disk importer) contains &mt.(*rtype).t.
//
// Red-green: isAddressable returns false before the fix; true after.
// Negative controls pin the boundary: value-type assertion fields,
// map-index fields, and composite-literal fields stay non-addressable.

import (
	"go/ast"
	"go/token"
	"testing"
)

// findUnaryAddrOfSelector finds the first &x UnaryExpr whose operand is a
// *ast.SelectorExpr (skipping &T{...} composite literals and &ident).
func findUnaryAddrOfSelector082(f *ast.File) *ast.UnaryExpr {
	var found *ast.UnaryExpr
	ast.Inspect(f, func(n ast.Node) bool {
		if u, ok := n.(*ast.UnaryExpr); ok && u.Op == token.AND && found == nil {
			if _, ok := u.X.(*ast.SelectorExpr); ok {
				found = u
				return false
			}
		}
		return true
	})
	return found
}

// findSelector finds the first *ast.SelectorExpr in f (used for negative
// controls, where & would not type-check).
func findSelector082(f *ast.File) *ast.SelectorExpr {
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

// TestPkg082IsAddressableTypeAssertPtrField: &mt.(*rtype).t — the exact
// reflect/type.go pattern. The type assertion yields a *rtype (pointer),
// so .t selects through a pointer and is addressable even though the
// assertion itself is not an addressable expression.
func TestPkg082IsAddressableTypeAssertPtrField(t *testing.T) {
	src := `package p

type rtype struct{ t int }

func mk() any { return &rtype{t: 1} }

func get() int {
	mt := mk()
	p := &mt.(*rtype).t
	return *p
}
`
	f, info := typeCheckPkg(t, src)
	u := findUnaryAddrOfSelector082(f)
	if u == nil {
		t.Fatal("no &<selector> found in test program")
	}
	sel, ok := u.X.(*ast.SelectorExpr)
	if !ok {
		t.Fatalf("& operand is %T, want *ast.SelectorExpr", u.X)
	}
	if _, ok := sel.X.(*ast.TypeAssertExpr); !ok {
		t.Fatalf("selector base is %T, want *ast.TypeAssertExpr", sel.X)
	}
	if !isAddressable(u.X, info) {
		t.Fatalf("isAddressable(&mt.(*rtype).t operand) = false, want true (PKG-082)")
	}
}

// TestPkg082IsAddressableCallPtrField: &f().N where f returns *T — a call
// result is never addressable, but the field selected through the returned
// pointer is. gc accepts this; the frontend must too.
func TestPkg082IsAddressableCallPtrField(t *testing.T) {
	src := `package p

type T struct{ N int }

func f() *T { return &T{N: 1} }

func get() int {
	p := &f().N
	return *p
}
`
	f, info := typeCheckPkg(t, src)
	u := findUnaryAddrOfSelector082(f)
	if u == nil {
		t.Fatal("no &<selector> found in test program")
	}
	if !isAddressable(u.X, info) {
		t.Fatalf("isAddressable(&f().N operand) = false, want true (PKG-082)")
	}
}

// TestPkg082IsAddressablePtrVarField: &p.N where p is a *T variable — the
// pre-existing path (p is an addressable Ident). Guards the common case
// against regressions from the new type-based rule.
func TestPkg082IsAddressablePtrVarField(t *testing.T) {
	src := `package p

type T struct{ N int }

func get(p *T) int {
	q := &p.N
	return *q
}
`
	f, info := typeCheckPkg(t, src)
	u := findUnaryAddrOfSelector082(f)
	if u == nil {
		t.Fatal("no &<selector> found in test program")
	}
	if !isAddressable(u.X, info) {
		t.Fatalf("isAddressable(&p.N operand) = false, want true")
	}
}

// TestPkg082IsAddressableTypeAssertValueFieldFalse: i.(T).N where T is a
// struct VALUE type — gc rejects &i.(T).N ("cannot take address"), so the
// selector must stay non-addressable. The new rule must not fire for
// non-pointer assertion results.
func TestPkg082IsAddressableTypeAssertValueFieldFalse(t *testing.T) {
	src := `package p

type T struct{ N int }

func get(i any) int {
	return i.(T).N
}
`
	f, info := typeCheckPkg(t, src)
	sel := findSelector082(f)
	if sel == nil {
		t.Fatal("no selector found in test program")
	}
	if isAddressable(sel, info) {
		t.Fatalf("isAddressable(i.(T).N) = true, want false (value-type assertion field is not addressable)")
	}
}

// TestPkg082IsAddressableMapIndexFieldFalse: m[k].N — map index results
// are not addressable, and selecting a field through one does not change
// that (no pointer indirection involved).
func TestPkg082IsAddressableMapIndexFieldFalse(t *testing.T) {
	src := `package p

type T struct{ N int }

func get(m map[string]T) int {
	return m["k"].N
}
`
	f, info := typeCheckPkg(t, src)
	sel := findSelector082(f)
	if sel == nil {
		t.Fatal("no selector found in test program")
	}
	if isAddressable(sel, info) {
		t.Fatalf("isAddressable(m[k].N) = true, want false (map index is not addressable)")
	}
}

// TestPkg082IsAddressableComplitFieldFalse: (T{N:1}).N — composite
// literals are not addressable, and neither are their fields.
func TestPkg082IsAddressableComplitFieldFalse(t *testing.T) {
	src := `package p

type T struct{ N int }

func get() int {
	return (T{N: 1}).N
}
`
	f, info := typeCheckPkg(t, src)
	sel := findSelector082(f)
	if sel == nil {
		t.Fatal("no selector found in test program")
	}
	if isAddressable(sel, info) {
		t.Fatalf("isAddressable((T{...}).N) = true, want false (composite literal is not addressable)")
	}
}
