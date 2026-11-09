package p3frontend

// P3-M7 (wave 1): p3frontend drift test for the synthetic "encoding/json"
// package. The importer reimplements the frozen exporter's synthetic
// signatures (it cannot reuse them — unexported and frozen); this test
// pins the mirror so drift fails fast.
//
// The test extracts the syntheticJsonSource const from
// internal/frontend/synth_json.go, type-checks it, and compares the
// mirror (syntheticJsonPackage) declaration-by-declaration against the
// resulting *types.Package: every exported type (with methods), function,
// and var must match in name and signature.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strconv"
	"testing"
)

// phase1SyntheticJsonPackage extracts the syntheticJsonSource const from
// the phase-1 frontend and type-checks it. This is the drift oracle for
// the mirror: the test below compares the mirror's declarations
// method-by-method against this package, so a phase-1-side change fails
// fast here instead of silently diverging.
func phase1SyntheticJsonPackage(t *testing.T) *types.Package {
	t.Helper()
	srcPath := "../synth_json.go"
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
				if name.Name == "syntheticJsonSource" && i < len(vs.Values) {
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						t.Fatalf("syntheticJsonSource is not a string literal")
					}
					raw, err = strconv.Unquote(lit.Value)
					if err != nil {
						t.Fatalf("unquoting syntheticJsonSource: %v", err)
					}
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatalf("syntheticJsonSource const not found in %s", srcPath)
	}
	jsonFset := token.NewFileSet()
	jsonFile, err := parser.ParseFile(jsonFset, "json.go", raw, parser.AllErrors)
	if err != nil {
		t.Fatalf("parsing synthetic json source: %v", err)
	}
	conf := types.Config{
		Importer: importerFunc(func(path string) (*types.Package, error) {
			if path == "bytes" {
				// Minimal stub: the synthetic json source only needs
				// *bytes.Buffer as a parameter type for Indent.
				bytesPkg := types.NewPackage("bytes", "bytes")
				bufName := types.NewTypeName(token.NoPos, bytesPkg, "Buffer", nil)
				types.NewNamed(bufName, types.NewStruct(nil, nil), nil)
				bytesPkg.Scope().Insert(bufName)
				bytesPkg.MarkComplete()
				return bytesPkg, nil
			}
			if path == "reflect" {
				// Minimal stub (P3-M8): the synthetic json source uses
				// reflect.Type (error struct fields, jsonTypeString),
				// reflect.Value and reflect.StructField (error struct
				// fields). Shared with the mirror so both sides agree.
				return syntheticReflectStub(), nil
			}
			return nil, fmt.Errorf("unexpected synthetic import %q", path)
		}),
	}
	pkg, err := conf.Check("encoding/json", jsonFset, []*ast.File{jsonFile}, nil)
	if err != nil {
		t.Fatalf("type-checking phase-1 synthetic json: %v", err)
	}
	return pkg
}

// jsonSigString renders a signature with package-qualified type names.
func jsonSigString(sig *types.Signature) string {
	return types.TypeString(sig, func(p *types.Package) string {
		if p == nil {
			return ""
		}
		return p.Name()
	})
}

// TestJsonSyntheticSignatures pins the mirrored synthetic encoding/json
// package: every exported type (with methods), function, and var must
// match the phase-1 synthetic source declaration-by-declaration.
func TestJsonSyntheticSignatures(t *testing.T) {
	mirror := syntheticJsonPackage()
	if mirror.Name() != "json" || mirror.Path() != "encoding/json" {
		t.Fatalf("package name/path = %s/%s, want json/encoding/json",
			mirror.Name(), mirror.Path())
	}
	phase1 := phase1SyntheticJsonPackage(t)

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
				if jsonSigString(wantSig) != jsonSigString(gotSig) {
					t.Fatalf("mirror %s.%s signature drift:\n phase-1: %s\n mirror:  %s",
						name, wm.Name(), jsonSigString(wantSig), jsonSigString(gotSig))
				}
			}
			// Mirror must not have extra exported methods.
			for j := 0; j < gotNamed.NumMethods(); j++ {
				gm := gotNamed.Method(j)
				if !ast.IsExported(gm.Name()) {
					continue
				}
				found := false
				for i := 0; i < wantNamed.NumMethods(); i++ {
					if wantNamed.Method(i).Name() == gm.Name() {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("mirror %s has extra method %q (phase-1 lacks it)", name, gm.Name())
				}
			}
		case *types.Func:
			got, ok := mobj.(*types.Func)
			if !ok {
				t.Fatalf("mirror %s is %T, want *types.Func", name, mobj)
			}
			wantSig := want.Type().(*types.Signature)
			gotSig := got.Type().(*types.Signature)
			if jsonSigString(wantSig) != jsonSigString(gotSig) {
				t.Fatalf("mirror %s signature drift:\n phase-1: %s\n mirror:  %s",
					name, jsonSigString(wantSig), jsonSigString(gotSig))
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
		default:
			t.Fatalf("phase-1 %s is %T (unexpected)", name, obj)
		}
	}
}

// TestJsonImporterRegistersEncodingJson verifies the importer resolves
// "encoding/json" to the synthetic mirror.
func TestJsonImporterRegistersEncodingJson(t *testing.T) {
	pkg := syntheticJsonPackage()
	if pkg.Path() != "encoding/json" {
		t.Fatalf("syntheticJsonPackage path = %q, want encoding/json", pkg.Path())
	}
	// Spot-check key declarations exist.
	for _, name := range []string{"Marshal", "MarshalIndent", "Unmarshal",
		"Valid", "Indent", "NewDecoder", "NewEncoder", "Decoder", "Encoder",
		"Delim", "Number", "RawMessage", "Token", "SyntaxError",
		"Marshaler", "Unmarshaler"} {
		if pkg.Scope().Lookup(name) == nil {
			t.Fatalf("syntheticJsonPackage missing %q", name)
		}
	}
}
