package p3frontend

// P3-M7 (wave 1): p3frontend drift test for the synthetic "strconv"
// package. The importer reimplements the frozen exporter's synthetic
// signatures (it cannot reuse them — unexported and frozen); this test
// pins the mirror so drift fails fast.
//
// The test extracts the syntheticStrconvSource const from
// phases/phase1/frontend/synth_strconv.go, type-checks it, and compares the
// mirror (syntheticStrconvPackage) declaration-by-declaration against the
// resulting *types.Package: every exported type (with methods), function,
// var, and const must match in name and signature.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strconv"
	"testing"
)

// phase1SyntheticErrorsPackage extracts the syntheticErrorsSource const
// from the phase-1 frontend and type-checks it, for the strconv drift
// test's importer.
func phase1SyntheticErrorsPackage(t *testing.T) *types.Package {
	t.Helper()
	srcPath := "../../../phase1/frontend/synth_errors.go"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, srcPath, nil, 0)
	if err != nil {
		t.Fatalf("parsing phase-1 errors synth source: %v", err)
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
				if name.Name == "syntheticErrorsSource" && i < len(vs.Values) {
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						t.Fatalf("syntheticErrorsSource is not a string literal")
					}
					raw, err = strconv.Unquote(lit.Value)
					if err != nil {
						t.Fatalf("unquoting syntheticErrorsSource: %v", err)
					}
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatalf("syntheticErrorsSource const not found in %s", srcPath)
	}
	errFset := token.NewFileSet()
	errFile, err := parser.ParseFile(errFset, "errors.go", raw, parser.AllErrors)
	if err != nil {
		t.Fatalf("parsing synthetic errors source: %v", err)
	}
	conf := types.Config{
		Importer: importerFunc(func(path string) (*types.Package, error) {
			return nil, fmt.Errorf("unexpected synthetic import %q", path)
		}),
	}
	pkg, err := conf.Check("errors", errFset, []*ast.File{errFile}, nil)
	if err != nil {
		t.Fatalf("type-checking phase-1 synthetic errors: %v", err)
	}
	pkg.MarkComplete()
	return pkg
}

// phase1SyntheticStrconvPackage extracts the syntheticStrconvSource const
// from the phase-1 frontend and type-checks it. This is the drift oracle
// for the mirror: the test below compares the mirror's declarations
// method-by-method against this package, so a phase-1-side change fails
// fast here instead of silently diverging.
func phase1SyntheticStrconvPackage(t *testing.T) *types.Package {
	t.Helper()
	srcPath := "../../../phase1/frontend/synth_strconv.go"
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
				if name.Name == "syntheticStrconvSource" && i < len(vs.Values) {
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						t.Fatalf("syntheticStrconvSource is not a string literal")
					}
					raw, err = strconv.Unquote(lit.Value)
					if err != nil {
						t.Fatalf("unquoting syntheticStrconvSource: %v", err)
					}
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatalf("syntheticStrconvSource const not found in %s", srcPath)
	}
	convFset := token.NewFileSet()
	convFile, err := parser.ParseFile(convFset, "strconv.go", raw, parser.AllErrors)
	if err != nil {
		t.Fatalf("parsing synthetic strconv source: %v", err)
	}
	conf := types.Config{
		Importer: importerFunc(func(path string) (*types.Package, error) {
			if path == "errors" {
				// Type-check the real synthetic errors source so the
				// strconv source resolves errors.New faithfully.
				return phase1SyntheticErrorsPackage(t), nil
			}
			return nil, fmt.Errorf("unexpected synthetic import %q", path)
		}),
	}
	pkg, err := conf.Check("strconv", convFset, []*ast.File{convFile}, nil)
	if err != nil {
		t.Fatalf("type-checking phase-1 synthetic strconv: %v", err)
	}
	return pkg
}

// strconvSigString renders a signature with package-qualified type names.
func strconvSigString(sig *types.Signature) string {
	return types.TypeString(sig, func(p *types.Package) string {
		if p == nil {
			return ""
		}
		return p.Name()
	})
}

// TestStrconvSyntheticSignatures pins the mirrored synthetic strconv
// package: every exported type (with methods), function, var, and const
// must match the phase-1 synthetic source declaration-by-declaration.
func TestStrconvSyntheticSignatures(t *testing.T) {
	mirror := syntheticStrconvPackage()
	if mirror.Name() != "strconv" || mirror.Path() != "strconv" {
		t.Fatalf("package name/path = %s/%s, want strconv/strconv",
			mirror.Name(), mirror.Path())
	}
	phase1 := phase1SyntheticStrconvPackage(t)

	qual := func(p *types.Package) string {
		if p == nil {
			return ""
		}
		return p.Name()
	}

	// Every exported declaration in phase-1 must exist in the mirror
	// with an identical signature/type string.
	for _, name := range phase1.Scope().Names() {
		obj := phase1.Scope().Lookup(name)
		if !ast.IsExported(name) {
			continue
		}
		mobj := mirror.Scope().Lookup(name)
		if mobj == nil {
			t.Fatalf("mirror missing %q (phase-1 has it)", name)
		}
		switch want := obj.(type) {
		case *types.TypeName:
			got, ok := mobj.(*types.TypeName)
			if !ok {
				t.Fatalf("mirror %s is %T, want *types.TypeName", name, mobj)
			}
			wantNamed, ok := want.Type().(*types.Named)
			if !ok {
				t.Fatalf("phase-1 %s is %T, want *types.Named", name, want.Type())
			}
			gotNamed, ok := got.Type().(*types.Named)
			if !ok {
				t.Fatalf("mirror %s is %T, want *types.Named", name, got.Type())
			}
			// For structs, compare field-by-field (unexported fields are
			// omitted in the mirror; compare only exported fields).
			if wantSt, ok := wantNamed.Underlying().(*types.Struct); ok {
				gotSt, ok := gotNamed.Underlying().(*types.Struct)
				if !ok {
					t.Fatalf("mirror %s underlying is %T, want *types.Struct", name, gotNamed.Underlying())
				}
				for i := 0; i < wantSt.NumFields(); i++ {
					f := wantSt.Field(i)
					if !ast.IsExported(f.Name()) {
						continue
					}
					found := false
					for j := 0; j < gotSt.NumFields(); j++ {
						gf := gotSt.Field(j)
						if gf.Name() == f.Name() {
							found = true
							wantFT := types.TypeString(f.Type(), qual)
							gotFT := types.TypeString(gf.Type(), qual)
							if wantFT != gotFT {
								t.Fatalf("mirror %s field %s type drift:\n phase-1: %s\n mirror:  %s",
									name, f.Name(), wantFT, gotFT)
							}
							break
						}
					}
					if !found {
						t.Fatalf("mirror %s missing exported field %q", name, f.Name())
					}
				}
			}
			// Methods must match (name + signature).
			for i := 0; i < wantNamed.NumMethods(); i++ {
				wm := wantNamed.Method(i)
				if !ast.IsExported(wm.Name()) {
					continue
				}
				var gm *types.Func
				for j := 0; j < gotNamed.NumMethods(); j++ {
					if gotNamed.Method(j).Name() == wm.Name() {
						gm = gotNamed.Method(j)
						break
					}
				}
				if gm == nil {
					t.Fatalf("mirror %s missing method %q", name, wm.Name())
				}
				wantSig := wm.Type().(*types.Signature)
				gotSig := gm.Type().(*types.Signature)
				if strconvSigString(wantSig) != strconvSigString(gotSig) {
					t.Fatalf("mirror %s.%s signature drift:\n phase-1: %s\n mirror:  %s",
						name, wm.Name(), strconvSigString(wantSig), strconvSigString(gotSig))
				}
			}
		case *types.Func:
			got, ok := mobj.(*types.Func)
			if !ok {
				t.Fatalf("mirror %s is %T, want *types.Func", name, mobj)
			}
			wantSig := want.Type().(*types.Signature)
			gotSig := got.Type().(*types.Signature)
			if strconvSigString(wantSig) != strconvSigString(gotSig) {
				t.Fatalf("mirror %s signature drift:\n phase-1: %s\n mirror:  %s",
					name, strconvSigString(wantSig), strconvSigString(gotSig))
			}
		case *types.Var:
			got, ok := mobj.(*types.Var)
			if !ok {
				t.Fatalf("mirror %s is %T, want *types.Var", name, mobj)
			}
			wantT := types.TypeString(want.Type(), qual)
			gotT := types.TypeString(got.Type(), qual)
			if wantT != gotT {
				t.Fatalf("mirror %s type drift:\n phase-1: %s\n mirror:  %s",
					name, wantT, gotT)
			}
		case *types.Const:
			got, ok := mobj.(*types.Const)
			if !ok {
				t.Fatalf("mirror %s is %T, want *types.Const", name, mobj)
			}
			wantT := types.TypeString(want.Type(), qual)
			gotT := types.TypeString(got.Type(), qual)
			if wantT != gotT {
				t.Fatalf("mirror %s const type drift:\n phase-1: %s\n mirror:  %s",
					name, wantT, gotT)
			}
			if want.Val().String() != got.Val().String() {
				t.Fatalf("mirror %s const value drift:\n phase-1: %s\n mirror:  %s",
					name, want.Val(), got.Val())
			}
		default:
			t.Fatalf("phase-1 %s is %T (unexpected)", name, obj)
		}
	}
}

// TestStrconvImporterRegistersStrconv verifies the importer resolves
// "strconv" to the synthetic mirror.
func TestStrconvImporterRegistersStrconv(t *testing.T) {
	pkg := syntheticStrconvPackage()
	if pkg.Path() != "strconv" {
		t.Fatalf("syntheticStrconvPackage path = %q, want strconv", pkg.Path())
	}
	// Spot-check key declarations exist.
	for _, name := range []string{"Atoi", "Itoa", "ParseInt", "ParseUint",
		"ParseFloat", "ParseBool", "ParseComplex", "FormatInt", "FormatUint",
		"FormatFloat", "FormatBool", "FormatComplex", "Quote", "Unquote",
		"UnquoteChar", "CanBackquote", "IsPrint", "IsGraphic", "NumError",
		"ErrSyntax", "ErrRange", "IntSize"} {
		if pkg.Scope().Lookup(name) == nil {
			t.Fatalf("syntheticStrconvPackage missing %q", name)
		}
	}
}
