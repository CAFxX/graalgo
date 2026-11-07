package main

// Regression tests for the time.Now eta-expansion (R-1107-03).
//
// The disk-MR rewrite eta-expands `var x = time.Now` to
// `var x = func() time.Time { return time.Now() }` (M5 limitation: the v1
// frontend cannot emit an intrinsic in value position). The expansion must
// be keyed on the RESOLVED object, not the identifier spelling: matching
// `pkg.Name == "time"` wrongly rewrote an aliased import named `time`
// (`import time "example.com/x"`) or a package-level variable named `time`
// with a `Now` method, and then failed type-check on valid Go code.
//
// etaExpandTimeNowValues runs AFTER type-checking, so the tests below parse
// a snippet, run transformDiskMultiResult (pipeline fidelity: the real
// caller gates the expansion on the package having multi-result funcs),
// type-check, and only then run the expansion — asserting on the AST shape
// and the registered info entries. A rewrite is asserted by pointer
// identity where no rewrite must happen (the original selector survives)
// and by FuncLit shape + info.Types registration where it must happen.
// Each test fails pre-fix (the spelling match rewrites the first two
// cases, and never rewrites the aliased-stdlib case).

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"
)

// parseAndCheckDiskMRTime parses src, runs the disk multi-result transform,
// and type-checks with imp, returning the file, the populated info, the
// checked package, and any type errors. It does NOT run the eta-expansion;
// callers do that after capturing the original AST nodes.
func parseAndCheckDiskMRTime(t *testing.T, src string, imp types.Importer) (*ast.File, *types.Info, *types.Package, []error) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	files := []*ast.File{f}
	transformDiskMultiResult("example.com/p", files)
	info := &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
	}
	conf := types.Config{Importer: imp}
	var errs []error
	conf.Error = func(e error) { errs = append(errs, e) }
	pkg, _ := conf.Check("example.com/p", fset, files, info)
	return f, info, pkg, errs
}

// varSpecByName returns the package-level ValueSpec declaring name, or nil.
func varSpecByName(f *ast.File, name string) *ast.ValueSpec {
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, n := range vs.Names {
				if n.Name == name {
					return vs
				}
			}
		}
	}
	return nil
}

// checkTimeNowClean asserts the snippet type-checks with zero errors.
func checkTimeNowClean(t *testing.T, errs []error) {
	t.Helper()
	if len(errs) != 0 {
		t.Fatalf("got %d type errors, want 0: %v", len(errs), errs)
	}
}

// fakeImporter serves packages from a fixed map; anything else is an error.
type fakeImporter struct {
	pkgs map[string]*types.Package
}

func (f fakeImporter) Import(path string) (*types.Package, error) {
	if p, ok := f.pkgs[path]; ok {
		return p, nil
	}
	return nil, fmt.Errorf("package %q not found", path)
}

// fakeNowPackage builds a package "example.com/x" exposing func Now() int.
func fakeNowPackage() *types.Package {
	pkg := types.NewPackage("example.com/x", "x")
	sig := types.NewSignatureType(nil, nil, nil, types.NewTuple(),
		types.NewTuple(types.NewVar(token.NoPos, pkg, "", types.Typ[types.Int])), false)
	fn := types.NewFunc(token.NoPos, pkg, "Now", sig)
	pkg.Scope().Insert(fn)
	pkg.MarkComplete()
	return pkg
}

func TestDiskMRTimeNowAliasedImportNotRewritten(t *testing.T) {
	// `import time "example.com/x"` — the local name is "time" but the
	// package is not stdlib time. Must pass through unrewritten.
	src := `package p

import time "example.com/x"

func F() (int, error) { return 0, nil }

var x = time.Now
`
	f, info, pkg, errs := parseAndCheckDiskMRTime(t, src, fakeImporter{pkgs: map[string]*types.Package{
		"example.com/x": fakeNowPackage(),
	}})
	checkTimeNowClean(t, errs)
	vs := varSpecByName(f, "x")
	if vs == nil {
		t.Fatal("var x not found")
	}
	origSel, ok := vs.Values[0].(*ast.SelectorExpr)
	if !ok {
		t.Fatalf("var x value is %T, want *ast.SelectorExpr", vs.Values[0])
	}
	etaExpandTimeNowValues([]*ast.File{f}, info, pkg)
	// Re-fetch after expansion and assert pointer identity: no rewrite.
	if vs.Values[0] != origSel {
		t.Fatalf("aliased import time.Now was rewritten to %T, want it untouched", vs.Values[0])
	}
}

func TestDiskMRTimeNowPackageVarNotRewritten(t *testing.T) {
	// A package-level variable named `time` with a `Now` method. The
	// selector is a method value, not the stdlib func. Must pass through
	// unrewritten.
	src := `package p

type clock struct{}

func (clock) Now() int { return 0 }

var time = clock{}

func F() (int, error) { return 0, nil }

var x = time.Now
`
	f, info, pkg, errs := parseAndCheckDiskMRTime(t, src, importer.Default())
	checkTimeNowClean(t, errs)
	vs := varSpecByName(f, "x")
	if vs == nil {
		t.Fatal("var x not found")
	}
	origSel, ok := vs.Values[0].(*ast.SelectorExpr)
	if !ok {
		t.Fatalf("var x value is %T, want *ast.SelectorExpr", vs.Values[0])
	}
	etaExpandTimeNowValues([]*ast.File{f}, info, pkg)
	if vs.Values[0] != origSel {
		t.Fatalf("package-var time.Now was rewritten to %T, want it untouched", vs.Values[0])
	}
}

func TestDiskMRTimeNowParamShadowNotRewritten(t *testing.T) {
	// A function parameter named `time` shadows any import. The selector
	// resolves to the parameter (a *types.Var), not a package name.
	src := `package p

type clock struct{}

func (clock) Now() int { return 0 }

func F() (int, error) { return 0, nil }

func G(time clock) func() int {
	var x = time.Now
	return x
}
`
	f, info, pkg, errs := parseAndCheckDiskMRTime(t, src, importer.Default())
	checkTimeNowClean(t, errs)
	// Find the function-local `var x = time.Now` inside G.
	var vs *ast.ValueSpec
	var origSel *ast.SelectorExpr
	ast.Inspect(f, func(n ast.Node) bool {
		if s, ok := n.(*ast.ValueSpec); ok && len(s.Names) == 1 && s.Names[0].Name == "x" {
			if sel, ok := s.Values[0].(*ast.SelectorExpr); ok {
				vs, origSel = s, sel
			}
		}
		return true
	})
	if vs == nil {
		t.Fatal("var x inside G not found")
	}
	etaExpandTimeNowValues([]*ast.File{f}, info, pkg)
	if vs.Values[0] != origSel {
		t.Fatalf("param-shadowed time.Now was rewritten to %T, want it untouched", vs.Values[0])
	}
}

func TestDiskMRTimeNowStdlibRewritten(t *testing.T) {
	// The true positive: stdlib `time.Now` in value position is rewritten
	// to a wrapper closure, with the signature registered in info.Types
	// (exportFuncLit reads the literal's signature from there).
	src := `package p

import "time"

func F() (int, error) { return 0, nil }

var x = time.Now

var y = time.Now()
`
	f, info, pkg, errs := parseAndCheckDiskMRTime(t, src, importer.Default())
	checkTimeNowClean(t, errs)
	vs := varSpecByName(f, "x")
	if vs == nil {
		t.Fatal("var x not found")
	}
	origSel, ok := vs.Values[0].(*ast.SelectorExpr)
	if !ok {
		t.Fatalf("var x value is %T, want *ast.SelectorExpr", vs.Values[0])
	}
	etaExpandTimeNowValues([]*ast.File{f}, info, pkg)
	lit, ok := vs.Values[0].(*ast.FuncLit)
	if !ok {
		t.Fatalf("var x value is %T, want *ast.FuncLit (time.Now not rewritten)", vs.Values[0])
	}
	tv, ok := info.Types[lit.Type]
	if !ok || tv.Type == nil {
		t.Fatal("no info.Types entry for the rewritten func literal's type")
	}
	sig, ok := tv.Type.(*types.Signature)
	if !ok {
		t.Fatalf("rewritten literal type is %T, want *types.Signature", tv.Type)
	}
	if sig.Params().Len() != 0 || sig.Results().Len() != 1 {
		t.Fatalf("rewritten literal signature %s, want func() time.Time", sig)
	}
	if got := types.TypeString(sig.Results().At(0).Type(), nil); got != "time.Time" {
		t.Fatalf("rewritten literal result is %s, want time.Time", got)
	}
	// The inner call reuses the original selector, so its info entries
	// (PkgName, time.Now func) still classify the call.
	body := lit.Body.List
	if len(body) != 1 {
		t.Fatalf("rewritten literal body has %d statements, want 1", len(body))
	}
	ret, ok := body[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		t.Fatalf("rewritten literal body is not a single return, got %T", body[0])
	}
	call, ok := ret.Results[0].(*ast.CallExpr)
	if !ok {
		t.Fatalf("rewritten literal returns %T, want *ast.CallExpr", ret.Results[0])
	}
	if call.Fun != origSel {
		t.Fatal("rewritten literal's inner call does not reuse the original time.Now selector")
	}
	// A call in value position (`var y = time.Now()`) is not a bare func
	// value and must be left alone.
	vy := varSpecByName(f, "y")
	if vy == nil {
		t.Fatal("var y not found")
	}
	if _, ok := vy.Values[0].(*ast.CallExpr); !ok {
		t.Fatalf("var y value is %T, want *ast.CallExpr (untouched)", vy.Values[0])
	}
}

func TestDiskMRTimeNowAliasedStdlibRewritten(t *testing.T) {
	// `import t "time"` — the stdlib package under another local name.
	// Type-info resolution finds it; the spelling match never did.
	src := `package p

import t "time"

func F() (int, error) { return 0, nil }

var x = t.Now
`
	f, info, pkg, errs := parseAndCheckDiskMRTime(t, src, importer.Default())
	checkTimeNowClean(t, errs)
	vs := varSpecByName(f, "x")
	if vs == nil {
		t.Fatal("var x not found")
	}
	if _, ok := vs.Values[0].(*ast.SelectorExpr); !ok {
		t.Fatalf("var x value is %T, want *ast.SelectorExpr", vs.Values[0])
	}
	etaExpandTimeNowValues([]*ast.File{f}, info, pkg)
	lit, ok := vs.Values[0].(*ast.FuncLit)
	if !ok {
		t.Fatalf("var x value is %T, want *ast.FuncLit (aliased stdlib time.Now not rewritten)", vs.Values[0])
	}
	tv, ok := info.Types[lit.Type]
	if !ok || tv.Type == nil {
		t.Fatal("no info.Types entry for the rewritten func literal's type")
	}
	if sig, ok := tv.Type.(*types.Signature); !ok || sig.Results().Len() != 1 {
		t.Fatalf("rewritten literal signature %v, want func() time.Time", tv.Type)
	}
}
