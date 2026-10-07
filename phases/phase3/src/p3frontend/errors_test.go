package p3frontend

// P3-M7 (wave 1): p3frontend drift tests for the synthetic "errors"
// package. The importer reimplements the frozen exporter's synthetic
// signatures (it cannot reuse them — unexported); these tests pin the
// mirror against the REAL phase-1 synthetic source so drift fails fast.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strconv"
	"testing"
)

// phase1SyntheticPackageForTest parses the named string const out of a
// phase-1 frontend source file, type-checks the embedded Go source with
// imp, and returns its *types.Package. This is the drift oracle for the
// mirrors: tests compare the mirror against this package — not against
// itself — so a phase-1-side change fails fast instead of silently
// diverging. (Generalizes phase1SyntheticContextPackage in
// timecontext_test.go; errors/sort import nothing.)
func phase1SyntheticPackageForTest(t *testing.T, srcFile, constName, path string, imp types.Importer) *types.Package {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, srcFile, nil, 0)
	if err != nil {
		t.Fatalf("parsing phase-1 synth file %s: %v", srcFile, err)
	}
	var raw string
	found := false
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if name.Name == constName && i < len(vs.Values) {
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						t.Fatalf("%s is not a string literal", constName)
					}
					raw, err = strconv.Unquote(lit.Value)
					if err != nil {
						t.Fatalf("unquoting %s: %v", constName, err)
					}
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatalf("%s const not found in %s", constName, srcFile)
	}
	synFset := token.NewFileSet()
	synFile, err := parser.ParseFile(synFset, constName+".go", raw, parser.AllErrors)
	if err != nil {
		t.Fatalf("parsing synthetic %s source: %v", constName, err)
	}
	conf := types.Config{Importer: imp}
	pkg, err := conf.Check(path, synFset, []*ast.File{synFile}, nil)
	if err != nil {
		t.Fatalf("type-checking phase-1 synthetic %s: %v", constName, err)
	}
	return pkg
}

// rejectImports is a types.Importer for self-contained synthetic sources
// (errors, sort): any import is a test failure.
type rejectImports struct{}

func (rejectImports) Import(path string) (*types.Package, error) {
	return nil, fmt.Errorf("unexpected synthetic import %q", path)
}

// assertScopeMatchesPhase1 is the core drift check: every name in the
// phase-1 synthetic package's scope must exist in the mirror with an
// identical rendered type, and the mirror must not export anything the
// phase-1 source lacks. Unexported phase-1 names (errorString, joinError,
// pdqsort internals, ...) are skipped — they are not visible to user code
// and are deliberately not mirrored.
func assertScopeMatchesPhase1(t *testing.T, pkgName string, mirror, phase1 *types.Package) {
	t.Helper()
	qual := func(p *types.Package) string {
		if p == nil {
			return ""
		}
		return p.Name()
	}
	for _, name := range phase1.Scope().Names() {
		obj := phase1.Scope().Lookup(name)
		if !ast.IsExported(name) {
			continue
		}
		got := mirror.Scope().Lookup(name)
		if got == nil {
			t.Fatalf("%s: mirror missing exported %q (phase-1 has it)", pkgName, name)
		}
		wantS := types.TypeString(obj.Type(), qual)
		gotS := types.TypeString(got.Type(), qual)
		if wantS != gotS {
			t.Fatalf("%s: mirror %s type drift:\n phase-1: %s\n mirror:  %s",
				pkgName, name, wantS, gotS)
		}
	}
	for _, name := range mirror.Scope().Names() {
		if !ast.IsExported(name) {
			continue
		}
		if phase1.Scope().Lookup(name) == nil {
			t.Fatalf("%s: mirror exports %q which phase-1 lacks", pkgName, name)
		}
	}
}

// TestErrorsSyntheticSignatures pins the mirrored synthetic errors
// package against the phase-1 source: New, Is, As, AsType (generic),
// Unwrap, Join (variadic), ErrUnsupported.
func TestErrorsSyntheticSignatures(t *testing.T) {
	mirror := syntheticErrorsPackage()
	if mirror.Name() != "errors" || mirror.Path() != "errors" {
		t.Fatalf("package name/path = %s/%s, want errors/errors",
			mirror.Name(), mirror.Path())
	}
	phase1 := phase1SyntheticPackageForTest(t,
		"../../../phase1/frontend/synth_errors.go", "syntheticErrorsSource",
		"errors", rejectImports{})

	// Full exported-scope drift check (names + rendered types).
	assertScopeMatchesPhase1(t, "errors", mirror, phase1)

	// Join must be variadic (...error), not []error.
	joinObj := mirror.Scope().Lookup("Join").(*types.Func)
	joinSig := joinObj.Type().(*types.Signature)
	if !joinSig.Variadic() {
		t.Fatal("errors.Join mirror is not variadic")
	}

	// AsType must be generic: one type param E constrained by error.
	asTypeObj := mirror.Scope().Lookup("AsType").(*types.Func)
	asTypeSig := asTypeObj.Type().(*types.Signature)
	if asTypeSig.TypeParams().Len() != 1 {
		t.Fatalf("errors.AsType mirror has %d type params, want 1",
			asTypeSig.TypeParams().Len())
	}
	if asTypeSig.Results().Len() != 2 {
		t.Fatalf("errors.AsType mirror has %d results, want 2",
			asTypeSig.Results().Len())
	}

	// ErrUnsupported is a package-level error variable.
	varObj := mirror.Scope().Lookup("ErrUnsupported")
	if _, ok := varObj.(*types.Var); !ok {
		t.Fatalf("errors.ErrUnsupported is %T, want *types.Var", varObj)
	}
	if types.TypeString(varObj.Type(), nil) != "error" {
		t.Fatalf("errors.ErrUnsupported type = %s, want error",
			types.TypeString(varObj.Type(), nil))
	}

	// Unexported implementation types are not mirrored.
	for _, name := range []string{"errorString", "joinError", "unwrapper"} {
		if mirror.Scope().Lookup(name) != nil {
			t.Fatalf("mirror must not contain unexported %q", name)
		}
	}
}

// TestErrorsImportResolvesSynthetic verifies the loader resolves
// "errors" as synthetic (never GOROOT).
func TestErrorsImportResolvesSynthetic(t *testing.T) {
	l := NewLoader()
	ref, err := l.ResolveImport("/tmp", "errors")
	if err != nil {
		t.Fatalf("ResolveImport(%q) failed: %v", "errors", err)
	}
	if ref.Kind != kindSynthetic {
		t.Fatalf("ResolveImport(%q).Kind = %v, want kindSynthetic", "errors", ref.Kind)
	}
}
