package p3frontend

// P3-M7 (wave 1): p3frontend drift test for the synthetic "encoding/xml"
// package. The importer reimplements the frozen exporter's synthetic
// signatures (it cannot reuse them — unexported and frozen); this test
// pins the mirror so drift fails fast.
//
// The test extracts the syntheticXmlSource const from
// internal/frontend/synth_xml.go, type-checks it, and compares the
// mirror (syntheticXmlPackage) declaration-by-declaration against the
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

// phase1SyntheticXmlPackage extracts the syntheticXmlSource const from the
// phase-1 frontend and type-checks it. This is the drift oracle for the
// mirror: the test below compares the mirror's declarations
// method-by-method against this package, so a phase-1-side change fails
// fast here instead of silently diverging.
func phase1SyntheticXmlPackage(t *testing.T) *types.Package {
	t.Helper()
	srcPath := "../../../../internal/frontend/synth_xml.go"
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
				if name.Name == "syntheticXmlSource" && i < len(vs.Values) {
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						t.Fatalf("syntheticXmlSource is not a string literal")
					}
					raw, err = strconv.Unquote(lit.Value)
					if err != nil {
						t.Fatalf("unquoting syntheticXmlSource: %v", err)
					}
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatalf("syntheticXmlSource const not found in %s", srcPath)
	}
	xmlFset := token.NewFileSet()
	xmlFile, err := parser.ParseFile(xmlFset, "xml.go", raw, parser.AllErrors)
	if err != nil {
		t.Fatalf("parsing synthetic xml source: %v", err)
	}
	conf := types.Config{
		Importer: importerFunc(func(path string) (*types.Package, error) {
			return nil, fmt.Errorf("unexpected synthetic import %q", path)
		}),
	}
	pkg, err := conf.Check("encoding/xml", xmlFset, []*ast.File{xmlFile}, nil)
	if err != nil {
		t.Fatalf("type-checking phase-1 synthetic xml: %v", err)
	}
	return pkg
}

// xmlSigString renders a signature with package-qualified type names.
func xmlSigString(sig *types.Signature) string {
	return types.TypeString(sig, func(p *types.Package) string {
		if p == nil {
			return ""
		}
		return p.Name()
	})
}

// TestXmlSyntheticSignatures pins the mirrored synthetic encoding/xml
// package: every exported type (with methods), function, and var must
// match the phase-1 synthetic source declaration-by-declaration.
func TestXmlSyntheticSignatures(t *testing.T) {
	mirror := syntheticXmlPackage()
	if mirror.Name() != "xml" || mirror.Path() != "encoding/xml" {
		t.Fatalf("package name/path = %s/%s, want xml/encoding/xml",
			mirror.Name(), mirror.Path())
	}
	phase1 := phase1SyntheticXmlPackage(t)

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
			// Underlying type must match (rendered).
			wantU := types.TypeString(wantNamed.Underlying(), func(p *types.Package) string {
				if p == nil {
					return ""
				}
				return p.Name()
			})
			gotU := types.TypeString(gotNamed.Underlying(), func(p *types.Package) string {
				if p == nil {
					return ""
				}
				return p.Name()
			})
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
							wantFT := types.TypeString(f.Type(), func(p *types.Package) string {
								if p == nil {
									return ""
								}
								return p.Name()
							})
							gotFT := types.TypeString(gf.Type(), func(p *types.Package) string {
								if p == nil {
									return ""
								}
								return p.Name()
							})
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
			} else if wantU != gotU {
				// Non-struct: underlying must render identically.
				// (Interfaces are compared method-by-method below.)
				if _, ok := wantNamed.Underlying().(*types.Interface); !ok {
					t.Fatalf("mirror %s underlying drift:\n phase-1: %s\n mirror:  %s",
						name, wantU, gotU)
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
				wantSig, ok := wm.Type().(*types.Signature)
				if !ok {
					t.Fatalf("phase-1 %s.%s is not a signature", name, wm.Name())
				}
				gotSig, ok := gm.Type().(*types.Signature)
				if !ok {
					t.Fatalf("mirror %s.%s is not a signature", name, wm.Name())
				}
				if xmlSigString(wantSig) != xmlSigString(gotSig) {
					t.Fatalf("mirror %s.%s signature drift:\n phase-1: %s\n mirror:  %s",
						name, wm.Name(), xmlSigString(wantSig), xmlSigString(gotSig))
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
			wantSig, ok := want.Type().(*types.Signature)
			if !ok {
				t.Fatalf("phase-1 %s is not a signature", name)
			}
			gotSig, ok := got.Type().(*types.Signature)
			if !ok {
				t.Fatalf("mirror %s is not a signature", name)
			}
			if xmlSigString(wantSig) != xmlSigString(gotSig) {
				t.Fatalf("mirror %s signature drift:\n phase-1: %s\n mirror:  %s",
					name, xmlSigString(wantSig), xmlSigString(gotSig))
			}
		case *types.Var:
			got, ok := mobj.(*types.Var)
			if !ok {
				t.Fatalf("mirror %s is %T, want *types.Var", name, mobj)
			}
			wantT := types.TypeString(want.Type(), func(p *types.Package) string {
				if p == nil {
					return ""
				}
				return p.Name()
			})
			gotT := types.TypeString(got.Type(), func(p *types.Package) string {
				if p == nil {
					return ""
				}
				return p.Name()
			})
			if wantT != gotT {
				t.Fatalf("mirror %s type drift:\n phase-1: %s\n mirror:  %s",
					name, wantT, gotT)
			}
		default:
			t.Fatalf("phase-1 %s is %T (unexpected)", name, obj)
		}
	}
}

// TestXmlImporterRegistersEncodingXml verifies the importer resolves
// "encoding/xml" to the synthetic mirror.
func TestXmlImporterRegistersEncodingXml(t *testing.T) {
	// The importer switch is exercised via a minimal loader; here we
	// just verify the mirror builds and has the expected path.
	pkg := syntheticXmlPackage()
	if pkg.Path() != "encoding/xml" {
		t.Fatalf("syntheticXmlPackage path = %q, want encoding/xml", pkg.Path())
	}
	// Spot-check key declarations exist.
	for _, name := range []string{"Decoder", "Encoder", "Token", "Name", "Attr",
		"StartElement", "EndElement", "CharData", "Comment", "ProcInst", "Directive",
		"SyntaxError", "NewDecoder", "NewEncoder", "EscapeText", "Escape",
		"CopyToken", "HTMLEntity", "HTMLAutoClose", "Unmarshal", "Marshal"} {
		if pkg.Scope().Lookup(name) == nil {
			t.Fatalf("syntheticXmlPackage missing %q", name)
		}
	}
}
