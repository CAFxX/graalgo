package p3frontend

// P3-M7 (wave 1): p3frontend drift tests for the synthetic "encoding/csv"
// and "encoding/gob" packages. The importer reimplements the frozen
// exporter's synthetic signatures (it cannot reuse them — unexported);
// these tests pin the mirror against the REAL phase-1 synthetic source
// so drift fails fast.
//
// Each test extracts the syntheticCsvSource / syntheticGobSource const
// from internal/frontend/synth_enc_csv_gob.go, type-checks it, and
// compares the mirror (syntheticCsvPackage / syntheticGobPackage)
// declaration-by-declaration against the resulting *types.Package: every
// exported type (with exported struct fields and methods), function, and
// var must match in name and signature. The synthetic sources import the
// synthetic errors package, resolved here to the drift-pinned errors
// mirror (errors_test.go pins that mirror against the phase-1 errors
// source, so this stays honest).

import (
	"fmt"
	"go/ast"
	"go/types"
	"regexp"
	"testing"
)

// csvGobTestImporter resolves the synthetic csv/gob sources' single
// import ("errors") to the drift-pinned synthetic errors mirror.
func csvGobTestImporter() types.Importer {
	return importerFunc(func(path string) (*types.Package, error) {
		if path == "errors" {
			pkg := syntheticErrorsPackage()
			// The drift type-check needs a complete package or the
			// import resolves to an empty scope ("undefined: errors")
			// — cf. bytes_test.go. Fresh per call, so side-effect free.
			pkg.MarkComplete()
			return pkg, nil
		}
		return nil, fmt.Errorf("unexpected synthetic import %q", path)
	})
}

// phase1SyntheticCsvPackage type-checks the real phase-1 synthetic csv
// source. This is the drift oracle for the csv mirror: the test below
// compares the mirror's declarations against this package — not against
// itself — so a phase-1-side change fails fast instead of silently
// diverging.
func phase1SyntheticCsvPackage(t *testing.T) *types.Package {
	t.Helper()
	return phase1SyntheticPackageForTest(t,
		"../../../../internal/frontend/synth_enc_csv_gob.go", "syntheticCsvSource",
		"encoding/csv", csvGobTestImporter())
}

// phase1SyntheticGobPackage type-checks the real phase-1 synthetic gob
// source (drift oracle for the gob mirror).
func phase1SyntheticGobPackage(t *testing.T) *types.Package {
	t.Helper()
	return phase1SyntheticPackageForTest(t,
		"../../../../internal/frontend/synth_enc_csv_gob.go", "syntheticGobSource",
		"encoding/gob", csvGobTestImporter())
}

// csvGobSigString renders a signature with package-qualified type names.
func csvGobSigString(sig *types.Signature) string {
	return canonCsvGobSpelling(types.TypeString(sig, func(p *types.Package) string {
		if p == nil {
			return ""
		}
		return p.Name()
	}))
}

// csvGobTypeString renders a type like csvGobSigString does for signatures.
func csvGobTypeString(typ types.Type, qual types.Qualifier) string {
	return canonCsvGobSpelling(types.TypeString(typ, qual))
}

var (
	csvGobUint8Re = regexp.MustCompile(`\buint8\b`)
	csvGobInt32Re = regexp.MustCompile(`\bint32\b`)
)

// canonCsvGobSpelling canonicalizes the two alias spellings go/types can
// produce for the same type: hand-built mirrors may use
// types.Typ[types.Byte]/types.Typ[types.Rune] (rendering "uint8"/"int32")
// while the type-checked phase-1 source renders the universe "byte"/"rune"
// objects (all mirrors now use the universe objects, but the test stays
// robust to either spelling). Comparison is therefore up to Go alias
// equivalence: byte == uint8 and rune == int32 are the same type, so no
// real drift is masked -- a change to a genuinely different type still
// fails.
func canonCsvGobSpelling(s string) string {
	s = csvGobUint8Re.ReplaceAllString(s, "byte")
	s = csvGobInt32Re.ReplaceAllString(s, "rune")
	return s
}

// assertCsvGobMirrorMatches is the drift check: every exported declaration
// in the phase-1 synthetic package must exist in the mirror with an
// identical rendered type/signature (types: exported struct fields and
// exported methods compared individually; unexported members are skipped —
// they are deliberately not mirrored), and the mirror must not export
// anything the phase-1 source lacks.
func assertCsvGobMirrorMatches(t *testing.T, pkgName string, mirror, phase1 *types.Package) {
	t.Helper()
	qual := func(p *types.Package) string {
		if p == nil {
			return ""
		}
		return p.Name()
	}
	for _, name := range phase1.Scope().Names() {
		if !ast.IsExported(name) {
			continue
		}
		obj := phase1.Scope().Lookup(name)
		mobj := mirror.Scope().Lookup(name)
		if mobj == nil {
			t.Fatalf("%s: mirror missing %q (phase-1 has it)", pkgName, name)
		}
		switch want := obj.(type) {
		case *types.TypeName:
			got, ok := mobj.(*types.TypeName)
			if !ok {
				t.Fatalf("%s: mirror %s is %T, want *types.TypeName", pkgName, name, mobj)
			}
			wantNamed, ok := want.Type().(*types.Named)
			if !ok {
				t.Fatalf("%s: phase-1 %s is %T, want *types.Named", pkgName, name, want.Type())
			}
			gotNamed, ok := got.Type().(*types.Named)
			if !ok {
				t.Fatalf("%s: mirror %s is %T, want *types.Named", pkgName, name, got.Type())
			}
			// For structs, compare exported fields (unexported fields
			// are omitted in the mirror by design).
			if wantSt, ok := wantNamed.Underlying().(*types.Struct); ok {
				gotSt, ok := gotNamed.Underlying().(*types.Struct)
				if !ok {
					t.Fatalf("%s: mirror %s underlying is %T, want *types.Struct",
						pkgName, name, gotNamed.Underlying())
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
							wantFT := csvGobTypeString(f.Type(), qual)
							gotFT := csvGobTypeString(gf.Type(), qual)
							if wantFT != gotFT {
								t.Fatalf("%s: mirror %s field %s type drift:\n phase-1: %s\n mirror:  %s",
									pkgName, name, f.Name(), wantFT, gotFT)
							}
							break
						}
					}
					if !found {
						t.Fatalf("%s: mirror %s missing exported field %q", pkgName, name, f.Name())
					}
				}
			} else if csvGobTypeString(wantNamed.Underlying(), qual) !=
				csvGobTypeString(gotNamed.Underlying(), qual) {
				t.Fatalf("%s: mirror %s underlying drift:\n phase-1: %s\n mirror:  %s",
					pkgName, name,
					csvGobTypeString(wantNamed.Underlying(), qual),
					csvGobTypeString(gotNamed.Underlying(), qual))
			}
			// Exported methods must match (name + signature).
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
					t.Fatalf("%s: mirror %s missing method %q", pkgName, name, wm.Name())
				}
				wantSig, ok := wm.Type().(*types.Signature)
				if !ok {
					t.Fatalf("%s: phase-1 %s.%s is not a signature", pkgName, name, wm.Name())
				}
				gotSig, ok := gm.Type().(*types.Signature)
				if !ok {
					t.Fatalf("%s: mirror %s.%s is not a signature", pkgName, name, wm.Name())
				}
				if csvGobSigString(wantSig) != csvGobSigString(gotSig) {
					t.Fatalf("%s: mirror %s.%s signature drift:\n phase-1: %s\n mirror:  %s",
						pkgName, name, wm.Name(), csvGobSigString(wantSig), csvGobSigString(gotSig))
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
					t.Fatalf("%s: mirror %s has extra method %q (phase-1 lacks it)",
						pkgName, name, gm.Name())
				}
			}
		case *types.Func:
			got, ok := mobj.(*types.Func)
			if !ok {
				t.Fatalf("%s: mirror %s is %T, want *types.Func", pkgName, name, mobj)
			}
			wantSig, ok := want.Type().(*types.Signature)
			if !ok {
				t.Fatalf("%s: phase-1 %s is not a signature", pkgName, name)
			}
			gotSig, ok := got.Type().(*types.Signature)
			if !ok {
				t.Fatalf("%s: mirror %s is not a signature", pkgName, name)
			}
			if csvGobSigString(wantSig) != csvGobSigString(gotSig) {
				t.Fatalf("%s: mirror %s signature drift:\n phase-1: %s\n mirror:  %s",
					pkgName, name, csvGobSigString(wantSig), csvGobSigString(gotSig))
			}
		case *types.Var:
			got, ok := mobj.(*types.Var)
			if !ok {
				t.Fatalf("%s: mirror %s is %T, want *types.Var", pkgName, name, mobj)
			}
			wantT := csvGobTypeString(want.Type(), qual)
			gotT := csvGobTypeString(got.Type(), qual)
			if wantT != gotT {
				t.Fatalf("%s: mirror %s type drift:\n phase-1: %s\n mirror:  %s",
					pkgName, name, wantT, gotT)
			}
		default:
			t.Fatalf("%s: phase-1 %s is %T (unexpected)", pkgName, name, obj)
		}
	}
	// Mirror must not export anything the phase-1 source lacks.
	for _, name := range mirror.Scope().Names() {
		if !ast.IsExported(name) {
			continue
		}
		if phase1.Scope().Lookup(name) == nil {
			t.Fatalf("%s: mirror exports %q which phase-1 lacks", pkgName, name)
		}
	}
}

// TestCsvSyntheticSignatures pins the mirrored synthetic csv package
// against the real phase-1 synthetic source: every exported type (with
// methods), function, and var must match declaration-by-declaration.
func TestCsvSyntheticSignatures(t *testing.T) {
	mirror := syntheticCsvPackage()
	if mirror.Name() != "csv" || mirror.Path() != "encoding/csv" {
		t.Fatalf("package name/path = %s/%s, want csv/encoding/csv",
			mirror.Name(), mirror.Path())
	}
	phase1 := phase1SyntheticCsvPackage(t)
	assertCsvGobMirrorMatches(t, "csv", mirror, phase1)
}

// TestGobSyntheticSignatures pins the mirrored synthetic gob package
// against the real phase-1 synthetic source: every exported type (with
// methods) and function must match declaration-by-declaration.
func TestGobSyntheticSignatures(t *testing.T) {
	mirror := syntheticGobPackage()
	if mirror.Name() != "gob" || mirror.Path() != "encoding/gob" {
		t.Fatalf("package name/path = %s/%s, want gob/encoding/gob",
			mirror.Name(), mirror.Path())
	}
	phase1 := phase1SyntheticGobPackage(t)
	assertCsvGobMirrorMatches(t, "gob", mirror, phase1)
}
