package main

// Tests for the F-4 remediation (2026-10-18, review-name-keyed lane):
// the spelling-keyed, pre-type-check `time.Now` eta-expansion in
// transformDiskMultiResult was deleted. Time intrinsics in value position
// are lowered by the general export-time mechanism
// (exportTimeIntrinsicValue, gated on the resolved *types.Func object),
// which is spelling-independent.
//
// Pinned behaviors:
//  1. TestDiskTransformLeavesTimeValueSpecsAlone: the transform no longer
//     rewrites package-level `var x = time.Now` ValueSpecs at all.
//  2. TestDiskTimeNowValueExport: a disk package's `var NowFn = time.Now`
//     (and the aliased-import form `import t "time"; var x = t.Now`)
//     exports through the general mechanism — the wrapper closure carries
//     the "timenow" wire node.
//  3. TestDiskLocalTimeVarNotRewritten: the false positive — a disk
//     package with a LOCAL variable named `time` having a `Now` member —
//     type-checks cleanly (the old rewrite produced an unresolvable
//     `time.Time` result type and a confusing package-wide type error).

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// findValueSpec returns the first *ast.ValueSpec in f, or nil.
func findValueSpec(f *ast.File) *ast.ValueSpec {
	var found *ast.ValueSpec
	ast.Inspect(f, func(n ast.Node) bool {
		if vs, ok := n.(*ast.ValueSpec); ok && found == nil {
			found = vs
			return false
		}
		return true
	})
	return found
}

func TestDiskTransformLeavesTimeValueSpecsAlone(t *testing.T) {
	src := `package p

import "time"

var x = time.Now
var y = 42
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	transformDiskMultiResult("example.com/p", []*ast.File{f})
	vs := findValueSpec(f)
	if vs == nil {
		t.Fatal("ValueSpec vanished from the AST")
	}
	if _, ok := vs.Values[0].(*ast.FuncLit); ok {
		t.Errorf("transformDiskMultiResult rewrote `var x = time.Now` to a FuncLit: the spelling-keyed rewrite is back")
	}
	if _, ok := vs.Values[0].(*ast.SelectorExpr); !ok {
		t.Errorf("var x initializer is %T, want *ast.SelectorExpr (untouched)", vs.Values[0])
	}
}

// writeDiskFixture writes a GOPATH-style fixture package and points GOPATH
// at it. The import path is unique per test to dodge the disk caches.
func writeDiskFixture(t *testing.T, importPath, src string) {
	t.Helper()
	gp := t.TempDir()
	dir := filepath.Join(gp, "src", filepath.FromSlash(importPath))
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pkg.go"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPATH", gp)
}

func TestDiskTimeNowValueExport(t *testing.T) {
	writeDiskFixture(t, "example.com/graalgotimevalue", `package timevalue

import (
	"time"
	t "time"
)

var NowFn = time.Now
var NowFnAlias = t.Now

func GetNow() time.Time { return NowFn() }
func GetNowAlias() time.Time { return NowFnAlias() }
`)
	src := `package main

import tu "example.com/graalgotimevalue"

func main() {
	// NOTE: calling the func-valued vars directly (tu.NowFn()) is a
	// pre-existing frontend/v1 WS D limitation for package-qualified
	// vars, orthogonal to this fix; the vars are exercised through the
	// package's own functions, which is the shape the old rewrite
	// targeted.
	_ = tu.GetNow()
	_ = tu.GetNowAlias()
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with disk-package time.Now values: %v", err)
	}
	// One wrapper closure per package-level var (the plain and the
	// aliased-import form), each carrying the timenow wire node.
	if n := strings.Count(out, `"kind": "funclit"`); n < 2 {
		t.Errorf("exported JSON has %d funclit nodes, want >= 2 (NowFn + NowFnAlias wrappers)", n)
	}
	if n := strings.Count(out, `"kind": "timenow"`); n < 2 {
		t.Errorf("exported JSON has %d timenow nodes, want >= 2 (one per wrapper)", n)
	}
}

func TestDiskLocalTimeVarNotRewritten(t *testing.T) {
	// False positive pin: a local variable named `time` with a `Now`
	// member must not be mistaken for the time package. The old
	// spelling-keyed rewrite turned this into
	// `func() time.Time { return time.Now() }`, whose result type could
	// not resolve, failing the whole package build.
	writeDiskFixture(t, "example.com/graalgotimelocal", `package timelocal

type clock struct{}

func (clock) Now() int { return 1 }

var time = clock{}

var x = time.Now

func Get() int { return x() }
`)
	sp, err := buildDiskPackage("example.com/graalgotimelocal")
	if err != nil {
		t.Fatalf("buildDiskPackage with local `time` variable: %v", err)
	}
	// The ValueSpec must be untouched: still a selector, not a funclit.
	var sawSelector bool
	for _, f := range sp.files {
		ast.Inspect(f, func(n ast.Node) bool {
			vs, ok := n.(*ast.ValueSpec)
			if !ok || len(vs.Names) != 1 || vs.Names[0].Name != "x" {
				return true
			}
			if _, ok := vs.Values[0].(*ast.FuncLit); ok {
				t.Errorf("local `time`.Now ValueSpec was rewritten to a FuncLit")
			}
			if _, ok := vs.Values[0].(*ast.SelectorExpr); ok {
				sawSelector = true
			}
			return true
		})
	}
	if !sawSelector {
		t.Errorf("did not find the untouched `var x = time.Now` ValueSpec")
	}
}
