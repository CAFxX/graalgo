package main

import (
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"
)

// P1-09: the frontend no longer repairs synthetic sources at load
// (the former restoreRawStrings/hoistSyntheticImports rewrites were
// deleted from buildSyntheticPackage). The tests in this file pin the
// replacement contract instead:
//   - every registered synthetic source parses exactly as registered
//     (the generators must emit parseable Go);
//   - damaged input (the two damage classes the old repairs masked)
//     fails loudly in buildSyntheticPackage, naming the offending
//     package, instead of compiling a silently rewritten source;
//   - an ordinary parse error keeps the generic diagnostic (the
//     damage classification is specific, not a catch-all).

// TestSyntheticSourcesParseUnmodified: every registered synthetic
// source must parse with no repair step. This is the regression gate
// for P1-09: if a generator regresses to emitting scattered imports
// or backtick-stripping damage, this test fails here, at the source,
// instead of the frontend silently fixing it at load.
func TestSyntheticSourcesParseUnmodified(t *testing.T) {
	for path, src := range syntheticSources {
		if _, err := parser.ParseFile(token.NewFileSet(), path+".go", src, parser.AllErrors); err != nil {
			t.Errorf("synthetic %s does not parse unmodified (generator must emit parseable Go, P1-09): %v", path, err)
		}
	}
}

// buildSyntheticPackagePanic runs buildSyntheticPackage for a
// temporarily registered damaged source and returns its panic value
// (nil if it did not panic). The caller must have registered
// syntheticSources[path] and defers its removal;
// buildSyntheticPackage (not getSyntheticPackage) is used directly so
// nothing is cached.
func buildSyntheticPackagePanic(t *testing.T, path string) (panicked any) {
	t.Helper()
	defer func() {
		panicked = recover()
	}()
	buildSyntheticPackage(path)
	return nil
}

func TestBuildSyntheticPackageRejectsDamagedImportOrder(t *testing.T) {
	const path = "l06-test/damaged-import"
	syntheticSources[path] = "package damaged\n\nfunc f() {}\n\nimport \"strings\"\n"
	defer delete(syntheticSources, path)
	msg := buildSyntheticPackagePanic(t, path)
	if msg == nil {
		t.Fatal("buildSyntheticPackage did not panic on scattered imports")
	}
	s, _ := msg.(string)
	if !strings.Contains(s, path) || !strings.Contains(s, "is damaged") || !strings.Contains(s, "imports must appear before other declarations") {
		t.Fatalf("panic = %q, want it to name %s, say 'is damaged', and cite the import-order error", s, path)
	}
}

func TestBuildSyntheticPackageRejectsIllegalRuneLiteral(t *testing.T) {
	const path = "l06-test/damaged-rune"
	syntheticSources[path] = "package damaged\n\nvar x = '[:alnum:]'\n"
	defer delete(syntheticSources, path)
	msg := buildSyntheticPackagePanic(t, path)
	if msg == nil {
		t.Fatal("buildSyntheticPackage did not panic on an illegal rune literal")
	}
	s, _ := msg.(string)
	if !strings.Contains(s, path) || !strings.Contains(s, "is damaged") || !strings.Contains(s, "illegal rune literal") {
		t.Fatalf("panic = %q, want it to name %s, say 'is damaged', and cite the illegal rune literal", s, path)
	}
}

func TestBuildSyntheticPackageOrdinaryParseErrorKeepsGenericDiagnostic(t *testing.T) {
	const path = "l06-test/broken-syntax"
	syntheticSources[path] = "package damaged\n\nfunc f( {\n"
	defer delete(syntheticSources, path)
	msg := buildSyntheticPackagePanic(t, path)
	if msg == nil {
		t.Fatal("buildSyntheticPackage did not panic on broken syntax")
	}
	s, _ := msg.(string)
	if strings.Contains(s, "is damaged") {
		t.Fatalf("ordinary parse error was misclassified as generator damage: %q", s)
	}
	if !strings.Contains(s, "parse error") {
		t.Fatalf("panic = %q, want the generic parse-error diagnostic", s)
	}
}

// TestSyntheticRegexpSyntaxTypeChecks: end-to-end through the real
// builder — the generator-fixed source must parse AND type-check
// against the overlay importer (this is what panicked for go-cmp
// before the P1-09 generator fix, when import hoisting lived in the
// frontend).
func TestSyntheticRegexpSyntaxTypeChecks(t *testing.T) {
	sp := getSyntheticPackage("regexp/syntax")
	if sp.pkg.Name() != "syntax" {
		t.Fatalf("package name = %q, want syntax", sp.pkg.Name())
	}
	for _, name := range []string{"Parse", "Compile", "Regexp", "Prog", "Op"} {
		if sp.pkg.Scope().Lookup(name) == nil {
			t.Errorf("regexp/syntax scope missing %s", name)
		}
	}
}

// TestSyntheticRuntimeFuncSurface pins the P3-M9 runtime.FuncForPC
// addition (driver-harden, go-cmp lane): the Func named type exists with
// the Name() string method, and FuncForPC has gc's exact signature
// func(pc uintptr) *Func.
func TestSyntheticRuntimeFuncSurface(t *testing.T) {
	pkg, err := overlayImporter{}.Import("runtime")
	if err != nil {
		t.Fatalf("import runtime: %v", err)
	}
	obj := pkg.Scope().Lookup("Func")
	if obj == nil {
		t.Fatal("runtime scope missing Func")
	}
	tn, ok := obj.(*types.TypeName)
	if !ok {
		t.Fatalf("Func is %T, want *types.TypeName", tn)
	}
	named, ok := tn.Type().(*types.Named)
	if !ok {
		t.Fatalf("Func type is %T, want *types.Named", tn)
	}
	sel := types.NewMethodSet(types.NewPointer(named)).Lookup(pkg, "Name")
	if sel == nil {
		t.Fatal("Func missing method Name")
	}
	nameSig := sel.Type().(*types.Signature)
	if nameSig.Results().Len() != 1 || nameSig.Results().At(0).Type() != types.Typ[types.String] {
		t.Errorf("Func.Name signature wrong: %s", nameSig)
	}
	fnObj := pkg.Scope().Lookup("FuncForPC")
	if fnObj == nil {
		t.Fatal("runtime scope missing FuncForPC")
	}
	fn, ok := fnObj.(*types.Func)
	if !ok {
		t.Fatalf("FuncForPC is %T, want *types.Func", fnObj)
	}
	sig := fn.Type().(*types.Signature)
	if sig.Params().Len() != 1 || sig.Params().At(0).Type() != types.Typ[types.Uintptr] {
		t.Errorf("FuncForPC params wrong: %s", sig)
	}
	ret := sig.Results().At(0).Type()
	ptr, ok := ret.(*types.Pointer)
	if !ok || ptr.Elem() != named {
		t.Errorf("FuncForPC return wrong: %s (want *runtime.Func)", sig.Results())
	}
}
