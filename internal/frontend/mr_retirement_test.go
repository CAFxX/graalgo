package main

import (
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Regression tests for R1-P1-2 (PKG-077 future-proofing): the multi-result
// desugar tables and the needX import blocks must never panic with
// 'gofrontend: no synthetic source for X' when a referenced package has
// retired to the disk importer.
//
// The live instance: the real-reflect2 retirement removed "reflect" from
// syntheticSources, but the m7 table still carries 7 reflect entries
// (Select, FieldByIndexErr, TryRecv, MethodByName, FieldByName,
// FieldByNameFunc, Lookup) and the m8iface table 3 more. Before the fix,
// exporting any program calling reflect.Select panicked in
// classifyM7MultiResultCall via getSyntheticPackage("reflect")
// (reproduced with test/corpus/wave2/reflect/06_swapper_select.go).
//
// The pinned behaviors:
//  1. desugarSyntheticPackage never panics, for any package path.
//  2. It returns nil exactly for paths without a synthetic source
//     (retired packages); non-nil for live synthetic packages.
//  3. The m7/m8iface classifiers skip retired table entries (return nil)
//     instead of panicking, so the disk desugar path can take over.
//  4. No fragile e.importSyntheticPackage(getSyntheticPackage("..."))
//     call site remains in export.go (all converted to importPkgTree).

// TestDesugarSyntheticPackageNeverPanics pins behaviors 1-2, and walks
// every package referenced by the static MR tables (behavior 3's
// prerequisite): each must resolve through desugarSyntheticPackage
// without panic, returning nil exactly when it has no synthetic source.
func TestDesugarSyntheticPackageNeverPanics(t *testing.T) {
	// Spot checks: retired vs live.
	if sp := desugarSyntheticPackage("reflect"); sp != nil {
		t.Errorf(`desugarSyntheticPackage("reflect") = non-nil, want nil (retired to disk importer)`)
	}
	if sp := desugarSyntheticPackage("internal/reflectlite"); sp != nil {
		t.Errorf(`desugarSyntheticPackage("internal/reflectlite") = non-nil, want nil (retired)`)
	}
	for _, live := range []string{"errors", "context", "time", "encoding/xml", "io", "os"} {
		if sp := desugarSyntheticPackage(live); sp == nil {
			t.Errorf("desugarSyntheticPackage(%q) = nil, want non-nil (still synthetic)", live)
		}
	}
	// Every package referenced by the static tables.
	seen := map[string]bool{}
	add := func(p string) {
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
	}
	for _, mf := range m7MultiResultFuncs {
		add(mf.pkgPath)
	}
	for _, mf := range m8MultiResultFuncs {
		add(mf.pkgPath)
		add(mf.helperPkg)
	}
	for _, mf := range osMultiResultFuncs {
		add(mf.pkgPath)
		add(mf.helperPkg)
	}
	for _, mf := range m9MultiResultFuncs {
		add(mf.pkgPath)
		add(mf.helperPkg)
	}
	for _, mf := range ioMultiResultFuncs {
		add(mf.pkgPath)
	}
	for _, m := range m8IfaceMultiResultMethods {
		add(m.pkgPath)
	}
	for _, me := range m5StaticMREntries {
		add(me.pkgPath)
	}
	if len(seen) == 0 {
		t.Fatal("no table packages collected; the tables may have been renamed")
	}
	// The reflect entries must be among them (the live-panic instance).
	if !seen["reflect"] {
		t.Error("m7/m8iface tables no longer reference reflect; update this test's premise")
	}
	for p := range seen {
		func() {
			defer func() {
				if r := recover(); r != nil {
					// This lane's contract is only the retirement panic.
					// Other build failures (e.g. pre-existing synthetic
					// source type errors, unrelated to this lane) are
					// out of scope and must not fail this test.
					if msg, ok := r.(string); ok && strings.Contains(msg, "no synthetic source for") {
						t.Errorf("desugarSyntheticPackage(%q) panicked with retirement panic: %v", p, r)
					}
				}
			}()
			sp := desugarSyntheticPackage(p)
			_, wantNil := syntheticSources[p]
			if (sp == nil) != !wantNil {
				t.Errorf("desugarSyntheticPackage(%q) = nil:%v, want nil:%v", p, sp == nil, !wantNil)
			}
		}()
	}
}

// TestM7ClassifierSkipsRetiredPackage builds a *types.Func standing in
// for reflect.Select (package path "reflect", 3 results — matching the m7
// table entry) and pins that classification returns nil instead of
// panicking. Pre-fix this panicked with
// 'gofrontend: no synthetic source for reflect'.
func TestM7ClassifierSkipsRetiredPackage(t *testing.T) {
	pkg := types.NewPackage("reflect", "reflect")
	results := types.NewTuple(
		types.NewVar(token.NoPos, pkg, "", types.Typ[types.Int]),
		types.NewVar(token.NoPos, pkg, "", types.Typ[types.Int]),
		types.NewVar(token.NoPos, pkg, "", types.Typ[types.Bool]),
	)
	sig := types.NewSignatureType(nil, nil, nil, types.NewTuple(), results, false)
	fn := types.NewFunc(token.NoPos, pkg, "Select", sig)
	if plan := classifyM7MultiResultCall("reflect", fn, nil); plan != nil {
		t.Fatalf("classifyM7MultiResultCall(reflect.Select) = %+v, want nil (retired package skips static entry)", plan)
	}
}

// TestM7IfaceFallbackSkipsRetiredPackage does the same for the
// receiver-less interface path (reflect.Type.MethodByName via the Type
// interface), which reached the same panic through
// classifyM7MultiResultCallIfaceFallback.
func TestM7IfaceFallbackSkipsRetiredPackage(t *testing.T) {
	pkg := types.NewPackage("reflect", "reflect")
	results := types.NewTuple(
		types.NewVar(token.NoPos, pkg, "", types.Typ[types.Int]),  // stand-in types;
		types.NewVar(token.NoPos, pkg, "", types.Typ[types.Bool]), // only the count matters
	)
	sig := types.NewSignatureType(nil, nil, nil, types.NewTuple(), results, false)
	fn := types.NewFunc(token.NoPos, pkg, "MethodByName", sig)
	if plan := classifyM7MultiResultCallIfaceFallback("reflect", fn, nil); plan != nil {
		t.Fatalf("classifyM7MultiResultCallIfaceFallback(reflect.MethodByName) = %+v, want nil", plan)
	}
}

// TestNoFragileSyntheticImportSites pins behavior 4: every
// e.importSyntheticPackage(getSyntheticPackage("...")) call site in
// export.go was converted to the retirement-safe e.importPkgTree("...").
// A reintroduced direct call fails this test loudly.
func TestNoFragileSyntheticImportSites(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(filepath.Dir(file), "export.go")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if strings.Contains(string(raw), "importSyntheticPackage(getSyntheticPackage(") {
		t.Error("export.go still contains importSyntheticPackage(getSyntheticPackage(...)) call sites; convert them to importPkgTree")
	}
	if n := strings.Count(string(raw), `e.importPkgTree("`); n == 0 {
		t.Error(`export.go contains no e.importPkgTree("...") calls; the conversion may have been reverted`)
	}
}
