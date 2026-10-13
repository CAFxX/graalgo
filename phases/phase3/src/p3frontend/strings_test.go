package p3frontend

// P3-M7 (wave 1): p3frontend drift tests for the synthetic "strings"
// package. The importer reimplements the frozen exporter's synthetic
// signatures (it cannot reuse them — unexported); these tests pin the
// mirror against the REAL phase-1 synthetic source so drift fails fast.

import (
	"go/ast"
	"go/types"
	"testing"
)

// TestStringsSyntheticSignatures pins the mirrored synthetic strings
// package against the phase-1 source: ~50 functions plus Builder,
// Replacer, Reader with all methods.
func TestStringsSyntheticSignatures(t *testing.T) {
	mirror := syntheticStringsPackage()
	if mirror.Name() != "strings" || mirror.Path() != "strings" {
		t.Fatalf("package name/path = %s/%s, want strings/strings",
			mirror.Name(), mirror.Path())
	}
	phase1 := phase1SyntheticPackageForTest(t,
		"../../../../internal/frontend/synth_strings.go", "syntheticStringsSource",
		"strings", syntheticStdImporter(t))

	// Full exported-scope drift check (names + rendered types).
	assertScopeMatchesPhase1(t, "strings", mirror, phase1)

	// Cut is multi-result (before, after string, found bool).
	cutObj := mirror.Scope().Lookup("Cut").(*types.Func)
	if cutObj.Type().(*types.Signature).Results().Len() != 3 {
		t.Fatal("strings.Cut mirror does not have 3 results")
	}

	// NewReplacer is variadic (...string).
	nrObj := mirror.Scope().Lookup("NewReplacer").(*types.Func)
	if !nrObj.Type().(*types.Signature).Variadic() {
		t.Fatal("strings.NewReplacer mirror is not variadic")
	}

	// Builder.Write is multi-result (int, error).
	builderObj := mirror.Scope().Lookup("Builder").(*types.TypeName)
	builderNamed := builderObj.Type().(*types.Named)
	var writeSig *types.Signature
	for i := 0; i < builderNamed.NumMethods(); i++ {
		if builderNamed.Method(i).Name() == "Write" {
			writeSig = builderNamed.Method(i).Type().(*types.Signature)
		}
	}
	if writeSig == nil || writeSig.Results().Len() != 2 {
		t.Fatal("strings.Builder.Write mirror is not (int, error)")
	}

	// Reader.ReadRune is triple-result (ch rune, size int, err error).
	readerObj := mirror.Scope().Lookup("Reader").(*types.TypeName)
	readerNamed := readerObj.Type().(*types.Named)
	var readRuneSig *types.Signature
	for i := 0; i < readerNamed.NumMethods(); i++ {
		if readerNamed.Method(i).Name() == "ReadRune" {
			readRuneSig = readerNamed.Method(i).Type().(*types.Signature)
		}
	}
	if readRuneSig == nil || readRuneSig.Results().Len() != 3 {
		t.Fatal("strings.Reader.ReadRune mirror does not have 3 results")
	}

	// Unexported implementation types are not mirrored.
	for _, name := range []string{"stringsCaseRange", "stringsTrieNode", "stringsGenericReplacer"} {
		if mirror.Scope().Lookup(name) != nil {
			t.Fatalf("mirror must not contain unexported %q", name)
		}
	}
}

// TestStringsOmissionSet pins the deliberate omission set: these gc 1.27.1
// members are absent from BOTH the phase-1 synthetic source and the
// mirror, with the documented reason. If a future wave makes one
// expressible (unicode.SpecialCase, io.Writer, iter.Seq land), this test
// forces an explicit update of the set — the omission must not silently
// shrink or grow.
func TestStringsOmissionSet(t *testing.T) {
	omitted := map[string]string{
		// Need unicode.SpecialCase (unicode wave).
		"ToUpperSpecial": "unicode.SpecialCase parameter",
		"ToLowerSpecial": "unicode.SpecialCase parameter",
		"ToTitleSpecial": "unicode.SpecialCase parameter",
		// Need io.Writer (io wave).
		"WriteString": "io.Writer parameter (Replacer.WriteString)",
		"WriteTo":     "io.Writer parameter (Reader.WriteTo)",
		// Need iter.Seq (iter wave).
		"Lines":         "iter.Seq result",
		"SplitSeq":      "iter.Seq result",
		"SplitAfterSeq": "iter.Seq result",
		"FieldsSeq":     "iter.Seq result",
		"FieldsFuncSeq": "iter.Seq result",
	}
	mirror := syntheticStringsPackage()
	phase1 := phase1SyntheticPackageForTest(t,
		"../../../../internal/frontend/synth_strings.go", "syntheticStringsSource",
		"strings", syntheticStdImporter(t))

	for name := range omitted {
		if mirror.Scope().Lookup(name) != nil {
			t.Fatalf("omitted %q unexpectedly present in mirror", name)
		}
		if phase1.Scope().Lookup(name) != nil {
			t.Fatalf("omitted %q unexpectedly present in phase-1 source", name)
		}
	}

	// The mirror must not omit anything else: every other gc 1.27.1
	// exported member is covered (checked by TestStringsSyntheticSignatures
	// via assertScopeMatchesPhase1). Spot-check the fail-fast member:
	// Title is present (exact signature, panic body).
	if mirror.Scope().Lookup("Title") == nil {
		t.Fatal("strings.Title must be present (fail-fast), not omitted")
	}
	if phase1.Scope().Lookup("Title") == nil {
		t.Fatal("strings.Title must be present in phase-1 (fail-fast), not omitted")
	}

	// WriteString/WriteTo exist as METHODS (not package-level), so check
	// the method sets explicitly.
	replacerObj := mirror.Scope().Lookup("Replacer").(*types.TypeName)
	replacerNamed := replacerObj.Type().(*types.Named)
	for i := 0; i < replacerNamed.NumMethods(); i++ {
		if replacerNamed.Method(i).Name() == "WriteString" {
			t.Fatal("Replacer.WriteString must be omitted (io.Writer)")
		}
	}
	readerObj := mirror.Scope().Lookup("Reader").(*types.TypeName)
	readerNamed := readerObj.Type().(*types.Named)
	for i := 0; i < readerNamed.NumMethods(); i++ {
		if readerNamed.Method(i).Name() == "WriteTo" {
			t.Fatal("Reader.WriteTo must be omitted (io.Writer)")
		}
	}

	_ = ast.IsExported // keep ast import if unused in future edits
}

// TestStringsImportResolvesSynthetic verifies the loader resolves
// "strings" as synthetic (never GOROOT).
func TestStringsImportResolvesSynthetic(t *testing.T) {
	l := NewLoader()
	ref, err := l.ResolveImport("/tmp", "strings")
	if err != nil {
		t.Fatalf("ResolveImport(%q) failed: %v", "strings", err)
	}
	if ref.Kind != kindSynthetic {
		t.Fatalf("ResolveImport(%q).Kind = %v, want kindSynthetic", "strings", ref.Kind)
	}
}
