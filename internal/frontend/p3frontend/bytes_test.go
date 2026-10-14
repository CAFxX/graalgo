package p3frontend

// P3-M7 (wave 1): p3frontend drift tests for the synthetic "bytes"
// package. The importer reimplements the frozen exporter's synthetic
// signatures (it cannot reuse them — unexported and frozen); these tests
// pin the mirror so drift fails fast.
//
// The drift oracle is the REAL phase-1 synthetic source
// (internal/frontend/synth_bytes.go, syntheticBytesSource const):
// the test type-checks it and compares the mirror declaration-by-
// declaration (name + signature). A phase-1-side change fails fast here
// instead of silently diverging.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strconv"
	"testing"
)

// phase1SyntheticBytesPackage type-checks the REAL phase-1 synthetic
// bytes source and returns its *types.Package. The synthetic source
// imports "errors", resolved to the synthetic errors mirror.
func phase1SyntheticBytesPackage(t *testing.T) *types.Package {
	t.Helper()
	srcPath := "../../../../internal/frontend/synth_bytes.go"
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
				if name.Name == "syntheticBytesSource" && i < len(vs.Values) {
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						t.Fatalf("syntheticBytesSource is not a string literal")
					}
					raw, err = strconv.Unquote(lit.Value)
					if err != nil {
						t.Fatalf("unquoting syntheticBytesSource: %v", err)
					}
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatalf("syntheticBytesSource const not found in %s", srcPath)
	}
	bytesFset := token.NewFileSet()
	bytesFile, err := parser.ParseFile(bytesFset, "bytes.go", raw, parser.AllErrors)
	if err != nil {		t.Fatalf("parsing synthetic bytes source: %v", err)
	}
	// P3-M10: use the shared synthetic importer (resolves "errors", "io",
	// and "sync" for the M8 import graph).
	conf := types.Config{
		Importer: syntheticStdImporter(t),
	}
	pkg, err := conf.Check("bytes", bytesFset, []*ast.File{bytesFile}, nil)
	if err != nil {
		t.Fatalf("type-checking phase-1 synthetic bytes: %v", err)
	}
	return pkg
}

// bytesSigString renders a signature with package-qualified type names
// (both the mirror and the phase-1 check use packages named "bytes" and
// "errors", so equal signatures render equal strings).
func bytesSigString(sig *types.Signature) string {
	return types.TypeString(sig, func(p *types.Package) string {
		if p == nil {
			return ""
		}
		return p.Name()
	})
}

// checkFuncSet compares two function sets (name + signature) and reports
// drift. kind labels the set for error messages ("func" or a method set
// like "Buffer method").
func checkFuncSet(t *testing.T, kind string, mirror, phase1 []*types.Func) {
	t.Helper()
	byName := func(fns []*types.Func) map[string]*types.Func {
		m := map[string]*types.Func{}
		for _, f := range fns {
			m[f.Name()] = f
		}
		return m
	}
	mm, pm := byName(mirror), byName(phase1)
	for name, want := range pm {
		got, ok := mm[name]
		if !ok {
			t.Errorf("mirror missing %s %q (phase-1 has it)", kind, name)
			continue
		}
		wantSig, gotSig := want.Type().(*types.Signature), got.Type().(*types.Signature)
		if wantSig == nil || gotSig == nil {
			t.Errorf("mirror %s %q is not a signature", kind, name)
			continue
		}
		if bytesSigString(wantSig) != bytesSigString(gotSig) {
			t.Errorf("mirror %s %q signature drift:\n phase-1: %s\n mirror:  %s",
				kind, name, bytesSigString(wantSig), bytesSigString(gotSig))
		}
	}
	for name := range mm {
		if _, ok := pm[name]; !ok {
			t.Errorf("mirror has extra %s %q (phase-1 lacks it)", kind, name)
		}
	}
}

// bytesPackageFuncs returns the EXPORTED package-level funcs. Unexported
// helpers (bytesDecodeRune, genSplit, ...) are phase-1-internal; the
// mirror only pins the exported API.
func bytesPackageFuncs(pkg *types.Package) []*types.Func {
	var out []*types.Func
	for _, name := range pkg.Scope().Names() {
		if fn, ok := pkg.Scope().Lookup(name).(*types.Func); ok && ast.IsExported(name) {
			out = append(out, fn)
		}
	}
	return out
}

// bytesNamedMethods returns the EXPORTED methods of a named type.
func bytesNamedMethods(named *types.Named) []*types.Func {
	var out []*types.Func
	for i := 0; i < named.NumMethods(); i++ {
		if m := named.Method(i); ast.IsExported(m.Name()) {
			out = append(out, m)
		}
	}
	return out
}

// TestBytesSyntheticSignatures is the drift test: the mirror's package
// funcs, Buffer methods and Reader methods must match the phase-1
// synthetic source declaration-by-declaration.
func TestBytesSyntheticSignatures(t *testing.T) {
	mirror := syntheticBytesPackage(syntheticIoPackage())
	if mirror.Name() != "bytes" || mirror.Path() != "bytes" {
		t.Fatalf("package name/path = %s/%s, want bytes/bytes", mirror.Name(), mirror.Path())
	}
	phase1 := phase1SyntheticBytesPackage(t)

	checkFuncSet(t, "func", bytesPackageFuncs(mirror), bytesPackageFuncs(phase1))

	for _, typeName := range []string{"Buffer", "Reader"} {
		mObj := mirror.Scope().Lookup(typeName)
		pObj := phase1.Scope().Lookup(typeName)
		if mObj == nil || pObj == nil {
			t.Fatalf("bytes.%s missing (mirror=%v phase-1=%v)", typeName, mObj != nil, pObj != nil)
		}
		mNamed, ok := mObj.Type().(*types.Named)
		if !ok {
			t.Fatalf("mirror bytes.%s is %T, want *types.Named", typeName, mObj.Type())
		}
		pNamed, ok := pObj.Type().(*types.Named)
		if !ok {
			t.Fatalf("phase-1 bytes.%s is %T, want *types.Named", typeName, pObj.Type())
		}
		checkFuncSet(t, typeName+" method",
			bytesNamedMethods(mNamed), bytesNamedMethods(pNamed))
	}

	// ErrTooLarge var and MinRead const.
	if obj := mirror.Scope().Lookup("ErrTooLarge"); obj == nil {
		t.Error("mirror bytes.ErrTooLarge missing")
	} else if _, ok := obj.(*types.Var); !ok {
		t.Errorf("mirror bytes.ErrTooLarge is %T, want *types.Var", obj)
	}
	if obj := mirror.Scope().Lookup("MinRead"); obj == nil {
		t.Error("mirror bytes.MinRead missing")
	} else if _, ok := obj.(*types.Const); !ok {
		t.Errorf("mirror bytes.MinRead is %T, want *types.Const", obj)
	}
}

// TestBytesOmissionsPinned pins the 8 deliberately omitted members on
// BOTH sides (mirror and phase-1 synthetic). Each omission carries its
// reason code; the list is the machine-readable record the exclusions
// validator cannot express (it is package-level only — a member entry is
// rejected as stale, verified 2026-10-07).
//
// Reason codes (same vocabulary as exclusions.yaml):
//   DEFERRED_WAVE — needs a package landing in a later wave.
func TestBytesOmissionsPinned(t *testing.T) {
	omissions := []struct {
		name   string
		reason string
		detail string
	}{
		{"Lines", "DEFERRED_WAVE", "needs iter.Seq (iter package + generics); unnameable without importing iter"},
		{"SplitSeq", "DEFERRED_WAVE", "needs iter.Seq[[]byte]; unnameable without importing iter"},
		{"SplitAfterSeq", "DEFERRED_WAVE", "needs iter.Seq[[]byte]; unnameable without importing iter"},
		{"FieldsSeq", "DEFERRED_WAVE", "needs iter.Seq[[]byte]; unnameable without importing iter"},
		{"FieldsFuncSeq", "DEFERRED_WAVE", "needs iter.Seq[[]byte]; unnameable without importing iter"},
		{"ToUpperSpecial", "DEFERRED_WAVE", "needs unicode.SpecialCase; unnameable without importing unicode"},
		{"ToLowerSpecial", "DEFERRED_WAVE", "needs unicode.SpecialCase; unnameable without importing unicode"},
		{"ToTitleSpecial", "DEFERRED_WAVE", "needs unicode.SpecialCase; unnameable without importing unicode"},
	}
	// The reason codes must be in the exclusions.yaml vocabulary.
	validReasons := map[string]bool{
		"PLATFORM": true, "CGO": true, "ASM_UNSUPPORTED": true,
		"LINKNAME_MISSING": true, "UNSUPPORTED_FEATURE": true,
		"DEFERRED_WAVE": true, "NONPORTABLE_BEHAVIOR": true,
	}
	mirror := syntheticBytesPackage(syntheticIoPackage())
	phase1 := phase1SyntheticBytesPackage(t)
	for _, o := range omissions {
		if !validReasons[o.reason] {
			t.Errorf("omission %s: reason %q not in exclusions.yaml vocabulary", o.name, o.reason)
		}
		if obj := mirror.Scope().Lookup(o.name); obj != nil {
			t.Errorf("mirror must omit bytes.%s (%s: %s)", o.name, o.reason, o.detail)
		}
		if obj := phase1.Scope().Lookup(o.name); obj != nil {
			t.Errorf("phase-1 synthetic must omit bytes.%s (%s: %s)", o.name, o.reason, o.detail)
		}
	}
	// Guard against silent list shrinkage: the test must pin exactly 8.
	if len(omissions) != 8 {
		t.Fatalf("omission list has %d entries, want 8", len(omissions))
	}
}

// TestBytesImportResolvesSynthetic verifies the loader resolves "bytes"
// as synthetic (never GOROOT).
func TestBytesImportResolvesSynthetic(t *testing.T) {
	l := NewLoader()
	ref, err := l.ResolveImport("/tmp", "bytes")
	if err != nil {
		t.Fatalf("ResolveImport(%q) failed: %v", "bytes", err)
	}
	if ref.Kind != kindSynthetic {
		t.Fatalf("ResolveImport(%q).Kind = %v, want kindSynthetic", "bytes", ref.Kind)
	}
}

// TestBytesImporterReturnsMirror verifies the Importer serves the bytes
// mirror (and that errors resolves for the synthetic dependency).
func TestBytesImporterReturnsMirror(t *testing.T) {
	l := NewLoader()
	imp := NewImporter(l, "/tmp", token.NewFileSet())
	pkg, err := imp.Import("bytes")
	if err != nil {
		t.Fatalf("Import(bytes) failed: %v", err)
	}
	if pkg.Name() != "bytes" {
		t.Fatalf("Import(bytes).Name() = %q, want bytes", pkg.Name())
	}
	for _, name := range []string{"Equal", "Index", "NewBuffer", "Buffer", "Reader", "ErrTooLarge", "MinRead"} {
		if pkg.Scope().Lookup(name) == nil {
			t.Errorf("imported bytes package missing %q", name)
		}
	}
}
