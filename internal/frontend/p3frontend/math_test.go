package p3frontend

// P3-M7 (WS G, ADR-0007 D5): p3frontend tests for the synthetic "math"
// package. The importer reimplements the frozen exporter's synthetic
// signatures (it cannot reuse them — unexported and frozen); these tests
// pin the mirror so drift fails fast.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strconv"
	"testing"
)

// phase1SyntheticMathPackage type-checks the REAL phase-1 synthetic math
// source (internal/frontend/synth_math.go, syntheticMathSource const)
// and returns its *types.Package. This is the drift oracle for the
// mirror: the test below compares the mirror's package-level functions
// (names + signatures) and constants against this package, so a
// phase-1-side change fails fast here instead of silently diverging.
func phase1SyntheticMathPackage(t *testing.T) *types.Package {
	t.Helper()
	srcPath := "../synth_math.go"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, srcPath, nil, 0)
	if err != nil {
		t.Fatalf("parsing phase-1 synth source: %v", err)
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
				if name.Name == "syntheticMathSource" && i < len(vs.Values) {
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						t.Fatalf("syntheticMathSource is not a string literal")
					}
					raw, err = strconv.Unquote(lit.Value)
					if err != nil {
						t.Fatalf("unquoting syntheticMathSource: %v", err)
					}
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatalf("syntheticMathSource const not found in %s", srcPath)
	}
	mathFset := token.NewFileSet()
	mathFile, err := parser.ParseFile(mathFset, "math.go", raw, parser.AllErrors)
	if err != nil {
		t.Fatalf("parsing synthetic math source: %v", err)
	}
	conf := types.Config{
		Importer: importerFunc(func(path string) (*types.Package, error) {
			return nil, fmt.Errorf("unexpected synthetic import %q", path)
		}),
	}
	pkg, err := conf.Check("math", mathFset, []*ast.File{mathFile}, nil)
	if err != nil {
		t.Fatalf("type-checking phase-1 synthetic math: %v", err)
	}
	return pkg
}

// TestMathSyntheticSignatures pins the mirrored synthetic math package.
// It is a REAL drift test: the mirror's package-level functions and
// constants are compared (name + signature / name + value) against the
// phase-1 synthetic package's actual *types.Package — not against the
// mirror itself.
func TestMathSyntheticSignatures(t *testing.T) {
	pkg := syntheticMathPackage()
	if pkg.Name() != "math" || pkg.Path() != "math" {
		t.Fatalf("package name/path = %s/%s, want math/math", pkg.Name(), pkg.Path())
	}
	phase1Pkg := phase1SyntheticMathPackage(t)

	// Every EXPORTED package-level function in the phase-1 source must
	// exist in the mirror with an identical signature, and vice versa.
	// Unexported members are implementation details (not visible to the
	// importer) and are not mirrored.
	isExported := func(name string) bool {
		if name == "" {
			return false
		}
		c := name[0]
		return c >= 'A' && c <= 'Z'
	}
	phase1Funcs := map[string]*types.Func{}
	mirrorFuncs := map[string]*types.Func{}
	for _, scope := range []struct {
		pkg *types.Package
		m   map[string]*types.Func
	}{ {phase1Pkg, phase1Funcs}, {pkg, mirrorFuncs} } {
		for _, name := range scope.pkg.Scope().Names() {
			if !isExported(name) {
				continue
			}
			if fn, ok := scope.pkg.Scope().Lookup(name).(*types.Func); ok {
				scope.m[name] = fn
			}
		}
	}
	for name, want := range phase1Funcs {
		got, ok := mirrorFuncs[name]
		if !ok {
			t.Errorf("mirror missing function math.%s (phase-1 has it)", name)
			continue
		}
		wantSig, gotSig := want.Type().(*types.Signature), got.Type().(*types.Signature)
		if wantSig == nil || gotSig == nil {
			t.Errorf("math.%s is not a signature", name)
			continue
		}
		if sigString(wantSig) != sigString(gotSig) {
			t.Errorf("mirror math.%s signature drift:\n phase-1: %s\n mirror:  %s",
				name, sigString(wantSig), sigString(gotSig))
		}
	}
	for name := range mirrorFuncs {
		if _, ok := phase1Funcs[name]; !ok {
			t.Errorf("mirror has extra function math.%s (phase-1 lacks it)", name)
		}
	}

	// Constants: every EXPORTED phase-1 constant must exist in the mirror
	// with an identical value, and vice versa.
	collectConsts := func(p *types.Package) map[string]string {
		m := map[string]string{}
		for _, name := range p.Scope().Names() {
			if !isExported(name) {
				continue
			}
			if c, ok := p.Scope().Lookup(name).(*types.Const); ok {
				m[name] = c.Val().ExactString()
			}
		}
		return m
	}
	pc, mc := collectConsts(phase1Pkg), collectConsts(pkg)
	for name, want := range pc {
		got, ok := mc[name]
		if !ok {
			t.Errorf("mirror missing constant math.%s (phase-1 has it)", name)
			continue
		}
		if want != got {
			t.Errorf("mirror math.%s value drift: phase-1=%s mirror=%s", name, want, got)
		}
	}
	for name := range mc {
		if _, ok := pc[name]; !ok {
			t.Errorf("mirror has extra constant math.%s (phase-1 lacks it)", name)
		}
	}
}

// TestMathImportResolvesSynthetic verifies the loader resolves "math" as
// synthetic (never GOROOT).
func TestMathImportResolvesSynthetic(t *testing.T) {
	l := NewLoader()
	ref, err := l.ResolveImport("/tmp", "math")
	if err != nil {
		t.Fatalf("ResolveImport(%q) failed: %v", "math", err)
	}
	if ref.Kind != kindSynthetic {
		t.Fatalf("ResolveImport(%q).Kind = %v, want kindSynthetic", "math", ref.Kind)
	}
}
