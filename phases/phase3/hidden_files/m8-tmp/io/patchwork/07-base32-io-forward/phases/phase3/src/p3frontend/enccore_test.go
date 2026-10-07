package p3frontend

// P3-M7 (wave 1): p3frontend drift tests for the 8 encoding/* primitive
// synthetic packages (m7-wenc-core). The importer reimplements the frozen
// exporter's synthetic signatures (it cannot reuse them — unexported);
// these tests pin the mirrors against the REAL phase-1 synthetic sources
// so drift fails fast.
//
// The drift oracle type-checks phases/phase1/frontend/synth_*.go. pem's
// source imports synthetic encoding/base64, resolved here to the real
// phase-1 base64 source (the bytes->errors precedent).

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/token"
	"go/types"
	"strings"
	"testing"
)

// encCorePackage describes one of the 8 packages under test.
type encCorePackage struct {
	path      string // import path
	srcFile   string // phase-1 frontend file
	constName string // synthetic source const
	mirror    func() *types.Package
	// namedTypes with methods to check (exported or not).
	namedTypes []string
	// pairStructs: unexported Appendix M carriers; fields must match
	// positionally (the desugar extracts by field index).
	pairStructs map[string][]string
}

var encCorePackages = []encCorePackage{
	{
		path: "encoding/base32", srcFile: "synth_base32.go",
		constName:  "syntheticBase32Source",
		mirror:     syntheticBase32Package,
		namedTypes: []string{"Encoding", "CorruptInputError"},
		pairStructs: map[string][]string{
			"base32DecodePair":       {"n", "err"},
			"base32DecodeStringPair": {"b", "err"},
		},
	},
	{
		path: "encoding/base64", srcFile: "synth_base64.go",
		constName:  "syntheticBase64Source",
		mirror:     syntheticBase64Package,
		namedTypes: []string{"Encoding", "CorruptInputError"},
		pairStructs: map[string][]string{
			"base64DecodePair":       {"n", "err"},
			"base64DecodeStringPair": {"b", "err"},
		},
	},
	{
		path: "encoding/hex", srcFile: "synth_hex.go",
		constName:  "syntheticHexSource",
		mirror:     syntheticHexPackage,
		namedTypes: []string{"InvalidByteError"},
		pairStructs: map[string][]string{
			"hexDecodePair":       {"n", "err"},
			"hexDecodeStringPair": {"b", "err"},
		},
	},
	{
		path: "encoding/binary", srcFile: "synth_binary.go",
		constName:  "syntheticBinarySource",
		mirror:     syntheticBinaryPackage,
		namedTypes: []string{"ByteOrder", "AppendByteOrder", "littleEndian", "bigEndian", "nativeEndian"},
		pairStructs: map[string][]string{
			"binaryUvarintPair": {"x", "n"},
			"binaryVarintPair":  {"x", "n"},
		},
	},
	{
		path: "encoding/ascii85", srcFile: "synth_ascii85.go",
		constName:  "syntheticAscii85Source",
		mirror:     syntheticAscii85Package,
		namedTypes: []string{"CorruptInputError"},
		pairStructs: map[string][]string{
			"ascii85DecodeTriple": {"ndst", "nsrc", "err"},
		},
	},
	{
		path: "encoding/pem", srcFile: "synth_pem.go",
		constName:  "syntheticPemSource",
		mirror:     syntheticPemPackage,
		namedTypes: []string{"Block"},
		pairStructs: map[string][]string{
			"pemDecodePair": {"p", "rest"},
		},
	},
	{
		path: "encoding/asn1", srcFile: "synth_asn1.go",
		constName:  "syntheticAsn1Source",
		mirror:     syntheticAsn1Package,
		namedTypes: []string{"StructuralError", "SyntaxError", "BitString", "ObjectIdentifier"},
		pairStructs: map[string][]string{
			"asn1MarshalPair": {"b", "err"},
		},
	},
	{
		path: "encoding", srcFile: "synth_encoding.go",
		constName:  "syntheticEncodingSource",
		mirror:     syntheticEncodingPackage,
		namedTypes: []string{
			"BinaryMarshaler", "BinaryUnmarshaler", "BinaryAppender",
			"TextMarshaler", "TextUnmarshaler", "TextAppender",
		},
		pairStructs: map[string][]string{},
	},
}

// phase1EncCorePackage type-checks the REAL phase-1 synthetic source for
// path and returns its *types.Package. The "encoding/base64" import in
// pem's source resolves to the real phase-1 base64 source.
func phase1EncCorePackage(t *testing.T, path string) *types.Package {
	t.Helper()
	var ec *encCorePackage
	for i := range encCorePackages {
		if encCorePackages[i].path == path {
			ec = &encCorePackages[i]
		}
	}
	if ec == nil {
		t.Fatalf("unknown encoding package %q", path)
	}
	imp := importerFunc(func(p string) (*types.Package, error) {
		if p == "encoding/base64" {
			return phase1EncCorePackage(t, "encoding/base64"), nil
		}
		// P3-M8: the base32 synthetic source imports io (streaming API).
		if p == "io" {
			return phase1SyntheticPackageForTest(t,
				"../../../phase1/frontend/synth_io.go",
				"syntheticIoSource", "io", imp), nil
		}
		if p == "errors" {
			return phase1SyntheticPackageForTest(t,
				"../../../phase1/frontend/synth_errors.go",
				"syntheticErrorsSource", "errors", imp), nil
		}
		if p == "sync" {
			// The synthetic sync surface is unexported in the phase-1
			// frontend; real sync is a superset and the drift comparison
			// never observes it (every sync use is unexported).
			return importer.Default().Import("sync")
		}
		return nil, fmt.Errorf("unexpected synthetic import %q", p)
	})
	return phase1SyntheticPackageForTest(t,
		"../../../phase1/frontend/"+ec.srcFile, ec.constName, path, imp)
}

// encCoreNamedMethods returns the EXPORTED methods of a named type (the
// bytes_test precedent). Unexported methods (base32/base64's decode) are
// implementation details; unexported TYPES with exported methods
// (binary's littleEndian) are still checked — their methods are
// user-visible through the package vars.
func encCoreNamedMethods(named *types.Named) []*types.Func {
	var out []*types.Func
	for i := 0; i < named.NumMethods(); i++ {
		if m := named.Method(i); ast.IsExported(m.Name()) {
			out = append(out, m)
		}
	}
	return out
}

// TestEncCoreSyntheticSignatures is the drift test: for each of the 8
// packages, the mirror's exported scope, named-type method sets, and
// Appendix M pair/triple structs must match the phase-1 synthetic
// source.
func TestEncCoreSyntheticSignatures(t *testing.T) {
	for _, ec := range encCorePackages {
		t.Run(ec.path, func(t *testing.T) {
			mirror := ec.mirror()
			if mirror.Name() != ec.path[strings.LastIndex(ec.path, "/")+1:] ||
				mirror.Path() != ec.path {
				t.Fatalf("package name/path = %s/%s, want %s",
					mirror.Name(), mirror.Path(), ec.path)
			}
			phase1 := phase1EncCorePackage(t, ec.path)

			// Exported scope (names + rendered types).
			assertScopeMatchesPhase1(t, ec.path, mirror, phase1)

			// Package-level funcs, declaration-by-declaration.
			checkFuncSet(t, "func",
				bytesPackageFuncs(mirror), bytesPackageFuncs(phase1))

			// Named-type method sets.
			for _, typeName := range ec.namedTypes {
				mObj := mirror.Scope().Lookup(typeName)
				pObj := phase1.Scope().Lookup(typeName)
				if mObj == nil || pObj == nil {
					t.Fatalf("%s.%s missing (mirror=%v phase-1=%v)",
						ec.path, typeName, mObj != nil, pObj != nil)
				}
				mNamed, ok := mObj.Type().(*types.Named)
				if !ok {
					t.Fatalf("mirror %s.%s is %T, want *types.Named",
						ec.path, typeName, mObj.Type())
				}
				pNamed, ok := pObj.Type().(*types.Named)
				if !ok {
					t.Fatalf("phase-1 %s.%s is %T, want *types.Named",
						ec.path, typeName, pObj.Type())
				}
				checkFuncSet(t, typeName+" method",
					encCoreNamedMethods(mNamed), encCoreNamedMethods(pNamed))
			}

			// Appendix M pair/triple structs: field names and types must
			// match positionally (the desugar extracts by field index).
			for pairName, wantFields := range ec.pairStructs {
				mObj := mirror.Scope().Lookup(pairName)
				pObj := phase1.Scope().Lookup(pairName)
				if mObj == nil || pObj == nil {
					t.Fatalf("%s: pair struct %s missing (mirror=%v phase-1=%v)",
						ec.path, pairName, mObj != nil, pObj != nil)
				}
				mNamed, ok := mObj.Type().(*types.Named)
				if !ok {
					t.Fatalf("mirror %s.%s is %T, want *types.Named",
						ec.path, pairName, mObj.Type())
				}
				pNamed, ok := pObj.Type().(*types.Named)
				if !ok {
					t.Fatalf("phase-1 %s.%s is %T, want *types.Named",
						ec.path, pairName, pObj.Type())
				}
				mStruct, ok := mNamed.Underlying().(*types.Struct)
				if !ok {
					t.Fatalf("mirror %s.%s underlying is %T, want *types.Struct",
						ec.path, pairName, mNamed.Underlying())
				}
				pStruct, ok := pNamed.Underlying().(*types.Struct)
				if !ok {
					t.Fatalf("phase-1 %s.%s underlying is %T, want *types.Struct",
						ec.path, pairName, pNamed.Underlying())
				}
				if mStruct.NumFields() != len(wantFields) ||
					pStruct.NumFields() != len(wantFields) {
					t.Fatalf("%s.%s field count: mirror=%d phase-1=%d want=%d",
						ec.path, pairName,
						mStruct.NumFields(), pStruct.NumFields(), len(wantFields))
				}
				qual := func(p *types.Package) string {
					if p == nil {
						return ""
					}
					return p.Name()
				}
				for i, want := range wantFields {
					mf, pf := mStruct.Field(i), pStruct.Field(i)
					if mf.Name() != want || pf.Name() != want {
						t.Fatalf("%s.%s field %d: mirror=%q phase-1=%q want %q",
							ec.path, pairName, i, mf.Name(), pf.Name(), want)
					}
					mft, pft := types.TypeString(mf.Type(), qual), types.TypeString(pf.Type(), qual)
					if mft != pft {
						t.Fatalf("%s.%s field %s type drift:\n phase-1: %s\n mirror:  %s",
							ec.path, pairName, want, pft, mft)
					}
				}
			}
		})
	}
}

// TestEncCoreOmissionsPinned pins the deliberately omitted members on
// BOTH sides (mirror and phase-1 synthetic). Each omission carries its
// reason code from the exclusions.yaml vocabulary.
//
// Reason codes:
//   DEFERRED_WAVE — needs a package landing in a later wave (io).
//   UNSUPPORTED_FEATURE — needs a v1-unexpressible feature (WS D).
func TestEncCoreOmissionsPinned(t *testing.T) {
	omissions := []struct {
		pkg    string
		name   string
		reason string
		detail string
	}{
		// P3-M8: base32's NewEncoder/NewDecoder are DECLARED now (exact gc
		// signatures; streaming Read/Write/Close fail fast — WS D). The
		// remaining rows still wait on io-adjacent work in their waves.
		{"encoding/base64", "NewEncoder", "DEFERRED_WAVE", "streaming encoder needs io (P3-M8)"},
		{"encoding/base64", "NewDecoder", "DEFERRED_WAVE", "streaming decoder needs io (P3-M8)"},
		{"encoding/hex", "NewEncoder", "DEFERRED_WAVE", "streaming encoder needs io (P3-M8)"},
		{"encoding/hex", "NewDecoder", "DEFERRED_WAVE", "streaming decoder needs io (P3-M8)"},
		{"encoding/hex", "Dumper", "DEFERRED_WAVE", "hex dumper needs io (P3-M8)"},
		{"encoding/ascii85", "NewEncoder", "DEFERRED_WAVE", "streaming encoder needs io (P3-M8)"},
		{"encoding/ascii85", "NewDecoder", "DEFERRED_WAVE", "streaming decoder needs io (P3-M8)"},
		// NOTE: asn1.Unmarshal is NOT omitted from the API — both sides
		// keep its exact gc signature (fail-fast body). What is omitted
		// is its Appendix M desugar (pointer store needs WS D); that is
		// pinned phase-1-side by TestEnccoreFailFast (2-target Unmarshal
		// fails fast with the named M7 error).
	}
	validReasons := map[string]bool{
		"PLATFORM": true, "CGO": true, "ASM_UNSUPPORTED": true,
		"LINKNAME_MISSING": true, "UNSUPPORTED_FEATURE": true,
		"DEFERRED_WAVE": true, "NONPORTABLE_BEHAVIOR": true,
	}
	mirrors := map[string]*types.Package{}
	phase1s := map[string]*types.Package{}
	for _, ec := range encCorePackages {
		mirrors[ec.path] = ec.mirror()
		phase1s[ec.path] = phase1EncCorePackage(t, ec.path)
	}
	for _, o := range omissions {
		if !validReasons[o.reason] {
			t.Errorf("omission %s.%s: reason %q not in exclusions.yaml vocabulary",
				o.pkg, o.name, o.reason)
		}
		if obj := mirrors[o.pkg].Scope().Lookup(o.name); obj != nil {
			t.Errorf("mirror must omit %s.%s (%s: %s)", o.pkg, o.name, o.reason, o.detail)
		}
		if obj := phase1s[o.pkg].Scope().Lookup(o.name); obj != nil {
			t.Errorf("phase-1 synthetic must omit %s.%s (%s: %s)",
				o.pkg, o.name, o.reason, o.detail)
		}
	}
}

// TestEncCoreImportResolvesSynthetic verifies the loader resolves all 8
// as synthetic (never GOROOT).
func TestEncCoreImportResolvesSynthetic(t *testing.T) {
	l := NewLoader()
	for _, ec := range encCorePackages {
		ref, err := l.ResolveImport("/tmp", ec.path)
		if err != nil {
			t.Fatalf("ResolveImport(%q) failed: %v", ec.path, err)
		}
		if ref.Kind != kindSynthetic {
			t.Fatalf("ResolveImport(%q).Kind = %v, want kindSynthetic", ec.path, ref.Kind)
		}
	}
}

// TestEncCoreImporterReturnsMirror verifies the Importer serves each of
// the 8 mirrors (and that encoding/pem's base64 dependency resolves).
func TestEncCoreImporterReturnsMirror(t *testing.T) {
	l := NewLoader()
	imp := NewImporter(l, "/tmp", token.NewFileSet())
	for _, ec := range encCorePackages {
		pkg, err := imp.Import(ec.path)
		if err != nil {
			t.Fatalf("Import(%q) failed: %v", ec.path, err)
		}
		if pkg.Path() != ec.path {
			t.Fatalf("Import(%q).Path() = %q", ec.path, pkg.Path())
		}
	}
	// The base64 dependency of pem must be cached (imported, not rebuilt).
	if _, ok := imp.pkgs["encoding/base64"]; !ok {
		t.Fatalf("importing encoding/pem did not cache encoding/base64")
	}
}
