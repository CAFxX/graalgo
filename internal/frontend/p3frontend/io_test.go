package p3frontend

// P1-2 lane (2026-11-03): p3frontend drift tests for the synthetic "io"
// package. The io mirror is deliberately MINIMAL (synth_io.go: Reader,
// Writer, ReadAll only — the types the bytes/strings/csv mirrors
// reference); the full v1 synthetic io API is not mirrored. These tests
// pin both directions of that contract:
//   - the three mirrored members match the v1 synthetic source exactly, and
//   - the mirror scope is exactly those three members (no silent growth),
//   - every other v1 exported member is pinned as an omission, so a v1
//     addition fails loudly instead of silently staying unmirrored.
// The drift oracle is the REAL phase-1 synthetic source
// (internal/frontend/synth_io.go, syntheticIoSource const), type-checked
// with the shared synthetic importer (io imports errors + real sync).

import (
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"testing"
)

// phase1SyntheticIoPackage type-checks the REAL phase-1 synthetic io
// source and returns its *types.Package.
func phase1SyntheticIoPackage(t *testing.T) *types.Package {
	t.Helper()
	return phase1SyntheticPackageForTest(t,
		"../synth_io.go",
		"syntheticIoSource", "io", syntheticStdImporter(t))
}

// ioSigString renders a signature with package-qualified type names
// (both the mirror and the phase-1 check use packages named "io", so
// equal signatures render equal strings).
func ioSigString(sig *types.Signature) string {
	return types.TypeString(sig, func(p *types.Package) string {
		if p == nil {
			return ""
		}
		return p.Name()
	})
}

func ioNamedMethod(t *testing.T, pkg *types.Package, typeName, method string) *types.Func {
	t.Helper()
	obj := pkg.Scope().Lookup(typeName)
	if obj == nil {
		t.Fatalf("io.%s missing", typeName)
	}
	named, ok := obj.Type().(*types.Named)
	if !ok {
		t.Fatalf("io.%s is %T, want *types.Named", typeName, obj.Type())
	}
	for i := 0; i < named.NumMethods(); i++ {
		if m := named.Method(i); m.Name() == method {
			return m
		}
	}
	t.Fatalf("io.%s.%s missing", typeName, method)
	return nil
}

// TestIoSyntheticSignatures is the drift test: the mirror's three members
// must match the phase-1 synthetic source declaration-by-declaration,
// and the mirror scope must be exactly those three.
func TestIoSyntheticSignatures(t *testing.T) {
	mirror := syntheticIoPackage()
	phase1 := phase1SyntheticIoPackage(t)
	if mirror.Name() != "io" || mirror.Path() != "io" {
		t.Fatalf("package name/path = %s/%s, want io/io", mirror.Name(), mirror.Path())
	}
	// Mirror scope is exactly the three mirrored members.
	var got []string
	for _, name := range mirror.Scope().Names() {
		got = append(got, name)
	}
	sort.Strings(got)
	want := []string{"ReadAll", "Reader", "Writer"}
	if len(got) != len(want) {
		t.Fatalf("mirror scope = %v, want exactly %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("mirror scope = %v, want exactly %v", got, want)
		}
	}
	// Interface method signatures.
	for _, tc := range []struct{ typ, method string }{
		{"Reader", "Read"},
		{"Writer", "Write"},
	} {
		mm := ioNamedMethod(t, mirror, tc.typ, tc.method)
		pm := ioNamedMethod(t, phase1, tc.typ, tc.method)
		if ioSigString(mm.Type().(*types.Signature)) != ioSigString(pm.Type().(*types.Signature)) {
			t.Errorf("mirror io.%s.%s signature drift:\n phase-1: %s\n mirror:  %s",
				tc.typ, tc.method,
				ioSigString(pm.Type().(*types.Signature)),
				ioSigString(mm.Type().(*types.Signature)))
		}
	}
	// Package-level func.
	mf, ok := mirror.Scope().Lookup("ReadAll").(*types.Func)
	if !ok {
		t.Fatalf("mirror io.ReadAll missing or not a func")
	}
	pf, ok := phase1.Scope().Lookup("ReadAll").(*types.Func)
	if !ok {
		t.Fatalf("phase-1 io.ReadAll missing or not a func")
	}
	if ioSigString(mf.Type().(*types.Signature)) != ioSigString(pf.Type().(*types.Signature)) {
		t.Errorf("mirror io.ReadAll signature drift:\n phase-1: %s\n mirror:  %s",
			ioSigString(pf.Type().(*types.Signature)),
			ioSigString(mf.Type().(*types.Signature)))
	}
}

// TestIoOmissionsPinned pins every v1 exported member the mirror
// deliberately omits (minimal-by-design mirror). A v1 addition fails here
// until it is either mirrored (and removed from this list) or explicitly
// added as a pinned omission — never silently unmirrored.
func TestIoOmissionsPinned(t *testing.T) {
	// Exported v1 scope names minus the three mirrored members, sorted.
	// Derived from the v1 synthetic source scope (funcs, types, vars,
	// consts); see the test failure output if this list drifts.
	omissions := []string{
		"ByteReader", "ByteScanner", "ByteWriter",
		"Closer", "Copy", "CopyBuffer", "CopyN",
		"Discard",
		"EOF", "ErrClosedPipe", "ErrNoProgress", "ErrShortBuffer",
		"ErrShortWrite", "ErrUnexpectedEOF",
		"LimitedReader", "LimitReader",
		"MultiReader", "MultiWriter",
		"NewOffsetWriter", "NewSectionReader", "NopCloser",
		"OffsetWriter",
		"Pipe", "PipeReader", "PipeWriter",
		"ReadAtLeast", "ReadCloser", "ReadFull", "ReadSeekCloser",
		"ReadSeeker", "ReadWriteCloser", "ReadWriteSeeker", "ReadWriter",
		"ReaderAt", "ReaderFrom", "RuneReader", "RuneScanner",
		"SectionReader", "SeekCurrent", "SeekEnd", "SeekStart", "Seeker",
		"StringWriter",
		"TeeReader",
		"WriteCloser", "WriteSeeker", "WriteString", "WriterAt", "WriterTo",
	}
	mirror := syntheticIoPackage()
	phase1 := phase1SyntheticIoPackage(t)
	for _, name := range omissions {
		if !ast.IsExported(name) {
			t.Errorf("omission %q is not exported (stale list entry)", name)
		}
		if obj := mirror.Scope().Lookup(name); obj != nil {
			t.Errorf("mirror must omit io.%s (minimal-by-design mirror)", name)
		}
		if obj := phase1.Scope().Lookup(name); obj == nil {
			t.Errorf("phase-1 synthetic must export io.%s (stale omission list?)", name)
		}
	}
	// Completeness: v1 exported scope minus mirrored members must equal
	// the pinned list exactly — no silent additions, no silent removals.
	var actual []string
	for _, name := range phase1.Scope().Names() {
		if !ast.IsExported(name) {
			continue
		}
		if name == "Reader" || name == "Writer" || name == "ReadAll" {
			continue
		}
		actual = append(actual, name)
	}
	sort.Strings(actual)
	if len(actual) != len(omissions) {
		t.Fatalf("v1 exported-minus-mirrored = %v (%d), want pinned list (%d):\n%v",
			actual, len(actual), len(omissions), omissions)
	}
	for i := range omissions {
		if actual[i] != omissions[i] {
			t.Fatalf("v1 exported-minus-mirrored = %v, want pinned list:\n%v",
				actual, omissions)
		}
	}
}

// TestIoImporterReturnsMirror verifies the Importer serves the io mirror.
func TestIoImporterReturnsMirror(t *testing.T) {
	l := NewLoader()
	imp := NewImporter(l, "/tmp", token.NewFileSet())
	pkg, err := imp.Import("io")
	if err != nil {
		t.Fatalf("Import(io) failed: %v", err)
	}
	if pkg.Name() != "io" {
		t.Fatalf("Import(io).Name() = %q, want io", pkg.Name())
	}
	for _, name := range []string{"Reader", "Writer", "ReadAll"} {
		if pkg.Scope().Lookup(name) == nil {
			t.Errorf("imported io package missing %q", name)
		}
	}
}
