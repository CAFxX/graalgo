package main

// isAddressable with nil types.Object: when info.ObjectOf returns nil for
// an identifier (incomplete type info, e.g. for identifiers in dependencies
// where full type info was not recorded), isAddressable must not return
// false. The frontend runs types.Config.Check before export, so if
// addressability is being queried (for &x or the implicit & on a
// pointer-receiver method call), the code is valid Go and the identifier
// must denote an addressable variable. Returning false rejects valid code.
//
// This is the root cause of the 8 conformance divergences on master
// 74670a11: p.fmt.init(&p.buf) from fmt/print.go — p is a local variable,
// info.ObjectOf(p) is nil due to incomplete type info, and the old code
// returned false.
//
// Red-green: isAddressable returns false before the fix; true after.

import (
	"go/ast"
	"go/token"
	"testing"
)

// TestIsAddressableNilObjectLocalVar: &p.buf where p is a local variable
// but info.ObjectOf(p) is nil (simulating incomplete type info).
// Must return true — the code passed type checking, so p is a variable.
func TestIsAddressableNilObjectLocalVar(t *testing.T) {
	src := `package main

type T struct{ N int }

func main() {
	s := T{N: 1}
	p := &s.N
	_ = p
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
	// sel.X is the Ident 's'. Simulate incomplete type info by removing
	// its entries from info.Uses and info.Defs.
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		t.Fatalf("selector base is %T, want *ast.Ident", sel.X)
	}
	// Sanity: before removal, ObjectOf should find the var.
	if obj := info.ObjectOf(id); obj == nil {
		t.Fatal("precondition failed: info.ObjectOf(s) is nil before removal")
	}
	delete(info.Uses, id)
	delete(info.Defs, id)
	// Now ObjectOf returns nil — the bug scenario.
	if obj := info.ObjectOf(id); obj != nil {
		t.Fatalf("info.ObjectOf(s) = %v after removal, want nil", obj)
	}
	// The fix: isAddressable must return true (not false).
	if !isAddressable(u.X, info) {
		t.Fatalf("isAddressable(&s.N) with nil ObjectOf = false, want true")
	}
}

// TestIsAddressableNilObjectBareIdent: &x where x is a bare Ident with nil
// ObjectOf. Must return true for the same reason.
func TestIsAddressableNilObjectBareIdent(t *testing.T) {
	src := `package main

func main() {
	x := 42
	p := &x
	_ = p
}
`
	f, info := typeCheckPkg(t, src)
	u := findUnaryAddr(f)
	if u == nil {
		t.Fatal("no &x found in test program")
	}
	id, ok := u.X.(*ast.Ident)
	if !ok {
		t.Fatalf("& operand is %T, want *ast.Ident", u.X)
	}
	if obj := info.ObjectOf(id); obj == nil {
		t.Fatal("precondition failed: info.ObjectOf(x) is nil before removal")
	}
	delete(info.Uses, id)
	delete(info.Defs, id)
	if obj := info.ObjectOf(id); obj != nil {
		t.Fatalf("info.ObjectOf(x) = %v after removal, want nil", obj)
	}
	if !isAddressable(u.X, info) {
		t.Fatalf("isAddressable(&x) with nil ObjectOf = false, want true")
	}
}

// TestIsAddressableNilObjectDoesNotBreakPkgName: the nil-ObjectOf fallback
// must not interfere with the PKG-081 package-name path. When ObjectOf
// returns a *types.PkgName (not nil), the existing logic applies.
func TestIsAddressableNilObjectDoesNotBreakPkgName(t *testing.T) {
	// Reuse the PKG-081 shape: &os.Stdout with full type info.
	// info.ObjectOf(os) is a *types.PkgName (not nil), so the fallback
	// does not trigger; the PKG-081 path handles it.
	src := `package main

import "os"

func main() {
	p := &os.Stdout
	_ = p
}
`
	f, info := typeCheckPkg(t, src)
	u := findUnaryAddr(f)
	if u == nil {
		t.Fatal("no &x found in test program")
	}
	// Full type info: PkgName path must still work.
	if !isAddressable(u.X, info) {
		t.Fatalf("isAddressable(&os.Stdout) = false, want true (PKG-081 path)")
	}
}

// Ensure the token import is used (for go vet).
var _ = token.AND
