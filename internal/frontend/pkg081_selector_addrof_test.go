package main

// PKG-081: & on *ast.SelectorExpr — address-of a package-qualified
// selector (e.g. &pkg.Var.field) was rejected with a WS D scope error.
// isAddressable did not handle the case where the selector base is a
// package name (a *types.PkgName, not a value): the recursive check on
// x.X fails because a PkgName is never a *types.Var. Package-level
// variables are always addressable in Go, so &pkg.Var and &pkg.Var.field
// are valid and must lower to the "addrof" wire node.
// Red-green: isAddressable returns false before the fix; true after.

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"
)

// typeCheckPkg parses and type-checks src as a single package, returning
// the AST file and types.Info.
func typeCheckPkg(t *testing.T, src string) (*ast.File, *types.Info) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
	}
	conf := types.Config{Importer: importer.Default()}
	if _, err := conf.Check("p", fset, []*ast.File{f}, info); err != nil {
		t.Fatal(err)
	}
	return f, info
}

// findUnaryAddr finds the first &x UnaryExpr in the AST.
func findUnaryAddr(f *ast.File) *ast.UnaryExpr {
	var found *ast.UnaryExpr
	ast.Inspect(f, func(n ast.Node) bool {
		if u, ok := n.(*ast.UnaryExpr); ok && u.Op == token.AND && found == nil {
			found = u
			return false
		}
		return true
	})
	return found
}

// TestPkg081IsAddressablePackageVarField: &pkg.Var.field — the selector
// pkg.Var denotes a package-level variable (a *types.Var), so the
// address-of operand is addressable.
func TestPkg081IsAddressablePackageVarField(t *testing.T) {
	// Simulate: package lib defines var Global struct{N int}; main does
	// &lib.Global.N. We type-check a single file that references the
	// pattern via a dot-import-like structure. Simpler: construct the
	// AST shape directly with a fake PkgName.
	//
	// Instead of full two-package type-checking (which needs GOPATH),
	// we test the core logic: a SelectorExpr whose X is an Ident denoting
	// a *types.PkgName, and whose Sel denotes a *types.Var.
	src := `package main

import "os"

var _ = os.Stdout

func main() {
	// os.Stdout is a package-level var; &os.Stdout is valid Go.
	p := &os.Stdout
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
	// Verify the shape: X is Ident denoting a PkgName.
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		t.Fatalf("selector base is %T, want *ast.Ident", sel.X)
	}
	if _, isPkg := info.ObjectOf(id).(*types.PkgName); !isPkg {
		t.Fatalf("selector base denotes %T, want *types.PkgName", info.ObjectOf(id))
	}
	// The fix: isAddressable must return true.
	if !isAddressable(u.X, info) {
		t.Fatalf("isAddressable(&os.Stdout operand) = false, want true (PKG-081)")
	}
}

// TestPkg081IsAddressablePackageFuncFalse: &pkg.Func must still be false —
// functions are not addressable. The fix must not over-approximate.
func TestPkg081IsAddressablePackageFuncFalse(t *testing.T) {
	src := `package main

import "os"

func main() {
	// os.Getenv is a func; &os.Getenv is invalid Go (go/types rejects it
	// before we run, so we test isAddressable directly on the selector).
	_ = os.Getenv
}
`
	f, info := typeCheckPkg(t, src)
	var sel *ast.SelectorExpr
	ast.Inspect(f, func(n ast.Node) bool {
		if s, ok := n.(*ast.SelectorExpr); ok && sel == nil {
			if id, ok := s.X.(*ast.Ident); ok && id.Name == "os" {
				sel = s
				return false
			}
		}
		return true
	})
	if sel == nil {
		t.Fatal("no os.X selector found")
	}
	if isAddressable(sel, info) {
		t.Fatalf("isAddressable(os.Getenv) = true, want false (functions are not addressable)")
	}
}

// TestPkg081IsAddressableLocalVarField: &s.field where s is a local var —
// the pre-existing path (recursive isAddressable on x.X). Guards against
// regressions in the common case.
func TestPkg081IsAddressableLocalVarField(t *testing.T) {
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
	if !isAddressable(u.X, info) {
		t.Fatalf("isAddressable(&s.N operand) = false, want true")
	}
}
