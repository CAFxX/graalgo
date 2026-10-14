package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"strings"
	"testing"
)

// mustParseHoist is a test helper: parse src or fail.
func mustParseHoist(t *testing.T, name, src string) *ast.File {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, parser.AllErrors|parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return f
}

// TestHoistSyntheticImportsMergesScatteredImports pins the general
// mechanism: imports scattered after other declarations are merged into
// a single block right after the package clause; duplicates are dropped;
// everything parses; non-import declarations are preserved.
func TestHoistSyntheticImportsMergesScatteredImports(t *testing.T) {
	src := `package p

import "a"

func f() {}

import "b"
import (
	"c"
	"a"
)

var v = 1

// a comment mentioning import "z" must not move.
var s = "import \"q\""
var r = ` + "`import raw`" + `
`
	// Structural assertions on the rewritten source.
	rewritten := hoistSyntheticImports(src)
	f := mustParseHoist(t, "p.go", rewritten)

	if len(f.Imports) != 3 {
		t.Fatalf("got %d imports, want 3 (a, b, c)", len(f.Imports))
	}
	var paths []string
	for _, imp := range f.Imports {
		paths = append(paths, strings.Trim(imp.Path.Value, `"`))
	}
	sort.Strings(paths)
	want := []string{"a", "b", "c"}
	for i := range want {
		if paths[i] != want[i] {
			t.Fatalf("imports = %v, want %v", paths, want)
		}
	}
	// Exactly one import declaration, and it must precede every other
	// declaration (only the package clause may come first).
	var importDecls, otherDecls int
	for _, d := range f.Decls {
		if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
			importDecls++
			if otherDecls > 0 {
				t.Fatalf("import declaration appears after other declarations")
			}
			continue
		}
		otherDecls++
	}
	if importDecls != 1 {
		t.Fatalf("got %d import declarations, want exactly 1", importDecls)
	}
	// Non-import declarations preserved.
	if otherDecls != 4 { // f, v, s, r
		t.Fatalf("got %d non-import declarations, want 4", otherDecls)
	}
	// The merged block sits immediately after the package clause.
	lines := strings.Split(rewritten, "\n")
	if !strings.HasPrefix(lines[1], "import (") {
		t.Fatalf("merged import block not right after package clause:\n%s", strings.Join(lines[:4], "\n"))
	}
}

// TestHoistSyntheticImportsExoticForms pins robustness on unusual but
// legal import spellings: single-line parenthesized blocks, missing
// space after the keyword, dot imports, and blank imports.
func TestHoistSyntheticImportsExoticForms(t *testing.T) {
	src := `package p

import "a"

func f() {}

import("b"; "c")
import"d"
import . "e"
import _ "f"
`
	rewritten := hoistSyntheticImports(src)
	f := mustParseHoist(t, "p.go", rewritten)
	var paths []string
	for _, imp := range f.Imports {
		name := ""
		if imp.Name != nil {
			name = imp.Name.Name + " "
		}
		paths = append(paths, name+imp.Path.Value)
	}
	sort.Strings(paths)
	want := []string{`"a"`, `"b"`, `"c"`, `"d"`, `. "e"`, `_ "f"`}
	sort.Strings(want)
	if len(paths) != len(want) {
		t.Fatalf("imports = %v, want %v", paths, want)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Fatalf("imports = %v, want %v", paths, want)
		}
	}
}

// TestRestoreRawStringsRepairsBacktickDamage pins the second general
// repair: illegal rune literals produced by naive backtick-stripping
// generators become raw strings again; legal rune literals are
// untouched; comments and interpreted strings are never rewritten.
func TestRestoreRawStringsRepairsBacktickDamage(t *testing.T) {
	src := `package p

var (
	a = '[:alnum:]' // damaged raw string
	b = '\d'        // damaged raw string with escape-like content
	c = 'x'         // legal: untouched
	d = '\n'        // legal escape: untouched
	e = '\x60'      // legal hex escape: untouched
	f = "don't"     // apostrophe in string: untouched
	g = '\\'        // legal backslash: untouched
)

// 'not code' stays a comment.
var h = 'ab' + 'cd'
`
	got := restoreRawStrings(src)
	f := mustParseHoist(t, "p.go", got)
	// Find the var specs and check their values' literal kinds.
	found := map[string]string{}
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, sp := range gd.Specs {
			vs := sp.(*ast.ValueSpec)
			for i, name := range vs.Names {
				if bl, ok := vs.Values[i].(*ast.BasicLit); ok {
					found[name.Name] = bl.Kind.String()
				}
			}
		}
	}
	// a, b, h's parts must be raw strings (backtick kind); the rest char/string.
	for _, name := range []string{"a", "b"} {
		if found[name] != "STRING" {
			t.Fatalf("%s kind = %s, want STRING (raw)", name, found[name])
		}
	}
	if !strings.Contains(got, "`[:alnum:]`") || !strings.Contains(got, "`\\d`") {
		t.Fatalf("damaged literals not restored to raw strings:\n%s", got)
	}
	for _, want := range []string{`'x'`, `'\n'`, `'\x60'`, `"don't"`, `'\\'`} {
		if !strings.Contains(got, want) {
			t.Fatalf("legal literal %s was rewritten:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "// 'not code' stays a comment.") {
		t.Fatalf("comment was rewritten:\n%s", got)
	}
	if !strings.Contains(got, "`ab` + `cd`") {
		t.Fatalf("adjacent damaged literals not restored:\n%s", got)
	}
}

// TestRestoreRawStringsNoopWhenHealthy: every registered synthetic
// source that parses today must be byte-identical after raw-string
// restoration.
func TestRestoreRawStringsNoopWhenHealthy(t *testing.T) {
	for path, src := range syntheticSources {
		fset := token.NewFileSet()
		if _, err := parser.ParseFile(fset, path+".go", src, parser.AllErrors); err != nil {
			continue // pre-existing damage; covered below
		}
		if got := restoreRawStrings(src); got != src {
			t.Errorf("synthetic %s: raw-string repair changed a healthy source", path)
		}
	}
}

// TestSyntheticRepairsEliminateDamageClass: for EVERY registered
// synthetic source, both damage classes (illegal rune literals from
// backtick-stripping, imports after declarations) must be gone after
// the general repairs — the guarantee is per-mechanism, not per-package.
func TestSyntheticRepairsEliminateDamageClass(t *testing.T) {
	for path, src := range syntheticSources {
		repaired := hoistSyntheticImports(restoreRawStrings(src))
		_, err := parser.ParseFile(token.NewFileSet(), path+".go", repaired, parser.AllErrors)
		if err == nil {
			continue
		}
		msg := err.Error()
		if strings.Contains(msg, "imports must appear before other declarations") ||
			strings.Contains(msg, "illegal rune literal") ||
			strings.Contains(msg, "unknown escape") {
			t.Errorf("synthetic %s: generator damage survives repair: %v", path, err)
		} else {
			t.Logf("synthetic %s: still fails to parse for another reason: %v", path, err)
		}
	}
}

// TestHoistSyntheticImportsNoopWhenHealthy: every registered synthetic
// source that parses today must be byte-identical after normalization.
// The repair must only touch sources that are actually broken.
func TestHoistSyntheticImportsNoopWhenHealthy(t *testing.T) {
	for path, src := range syntheticSources {
		fset := token.NewFileSet()
		if _, err := parser.ParseFile(fset, path+".go", src, parser.AllErrors); err != nil {
			t.Logf("synthetic %s does not parse unmodified (pre-existing): %v", path, err)
			continue
		}
		if got := hoistSyntheticImports(src); got != src {
			t.Errorf("synthetic %s: normalizer changed a healthy source", path)
		}
	}
}

// TestHoistSyntheticImportsEliminatesOrderError: for EVERY registered
// synthetic source, the import-order error class must not survive
// normalization — the general guarantee, not just regexp/syntax.
func TestHoistSyntheticImportsEliminatesOrderError(t *testing.T) {
	for path, src := range syntheticSources {
		norm := hoistSyntheticImports(src)
		_, err := parser.ParseFile(token.NewFileSet(), path+".go", norm, parser.AllErrors)
		if err != nil && strings.Contains(err.Error(), "imports must appear before other declarations") {
			t.Errorf("synthetic %s: import-order error survives normalization: %v", path, err)
		}
	}
}

// TestSyntheticSyncPoolSurface pins the P3-M9 sync.Pool addition
// (driver-harden, go-cmp lane): the Pool named type exists with gc's
// exact method signatures, Get/Put are in scope, and Pool.New is absent
// (out of scope — opaque Java object cannot carry Go-visible fields).
func TestSyntheticSyncPoolSurface(t *testing.T) {
	pkg := syntheticSyncPackage()
	obj := pkg.Scope().Lookup("Pool")
	if obj == nil {
		t.Fatal("sync scope missing Pool")
	}
	tn, ok := obj.(*types.TypeName)
	if !ok {
		t.Fatalf("Pool is %T, want *types.TypeName", obj)
	}
	named, ok := tn.Type().(*types.Named)
	if !ok {
		t.Fatalf("Pool type is %T, want *types.Named", tn.Type())
	}
	msel := func(name string) *types.Selection {
		// Methods have pointer receivers (gc-exact), so they live in
		// *Pool's method set.
		return types.NewMethodSet(types.NewPointer(named)).Lookup(pkg, name)
	}
	for _, m := range []string{"Get", "Put"} {
		if msel(m) == nil {
			t.Errorf("Pool missing method %s", m)
		}
		if !syncOpsInScope["Pool"][m] {
			t.Errorf("Pool.%s not in syncOpsInScope", m)
		}
	}
	// gc signatures: func (p *Pool) Get() any, func (p *Pool) Put(x any).
	if sel := msel("Get"); sel == nil {
		t.Error("Pool.Get missing; skipping signature check")
	} else if getSig := sel.Type().(*types.Signature); getSig.Results().Len() != 1 ||
		!types.IsInterface(getSig.Results().At(0).Type()) {
		t.Errorf("Pool.Get signature wrong: %s", getSig)
	}
	if sel := msel("Put"); sel == nil {
		t.Error("Pool.Put missing; skipping signature check")
	} else if putSig := sel.Type().(*types.Signature); putSig.Params().Len() != 1 ||
		!types.IsInterface(putSig.Params().At(0).Type()) {
		t.Errorf("Pool.Put signature wrong: %s", putSig)
	}
	// Pool.New must NOT exist (out of scope).
	if msel("New") != nil {
		t.Error("Pool.New method unexpectedly present")
	}
	for i := 0; i < named.Underlying().(*types.Struct).NumFields(); i++ {
		t.Errorf("Pool has struct field %s (must be fieldless)",
			named.Underlying().(*types.Struct).Field(i).Name())
	}
	if !isSyncPoolNamed(named) {
		t.Error("isSyncPoolNamed(Pool) = false")
	}
}

// TestSyntheticRegexpSyntaxTypeChecks: end-to-end through the real
// builder — the repaired source must parse AND type-check against the
// overlay importer (this is what panicked for go-cmp).
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

// TestHoistSyntheticImportsRegexpSyntaxParses: the real defective input.
// The embedded regexp/syntax source concatenates gc's per-file sources,
// leaving each file's import block in place.
func TestHoistSyntheticImportsRegexpSyntaxParses(t *testing.T) {
	src := syntheticRegexpSyntaxSource
	fset := token.NewFileSet()
	_, err := parser.ParseFile(fset, "syntax.go", src, parser.AllErrors)
	if err == nil {
		t.Skip("source parses unmodified; generator already fixed")
	}
	if !strings.Contains(err.Error(), "imports must appear before other declarations") {
		t.Fatalf("unexpected pre-existing parse error: %v", err)
	}
	norm := hoistSyntheticImports(restoreRawStrings(src))
	if norm == src {
		t.Fatal("repairs were a no-op on the defective source")
	}
	mustParseHoist(t, "syntax.go", norm)
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
		t.Fatalf("Func is %T, want *types.TypeName", obj)
	}
	named, ok := tn.Type().(*types.Named)
	if !ok {
		t.Fatalf("Func type is %T, want *types.Named", tn.Type())
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
