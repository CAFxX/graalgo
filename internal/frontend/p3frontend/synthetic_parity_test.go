package p3frontend

// L22 parity tests: the synthetic-package set exists in exactly one place
// (synthetic.go), and everything p3frontend resolves synthetically must be
// importable by the frozen gofrontend overlay (export.go), which is the
// consumer of the flattened output.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"testing"
)

// p3repoRoot returns the graalgo repo root: the test file lives in
// <root>/internal/frontend/p3frontend, three levels below the root.
// (The export_test.go repoRoot helper goes four levels up and is only
// used by tests that are already broken on master; do not reuse it.)
func p3repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..")
}

// sortedSyntheticPaths returns the synthetic set in a deterministic order.
func sortedSyntheticPaths() []string {
	paths := make([]string, 0, len(syntheticPackages))
	for p := range syntheticPackages {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

// TestSyntheticSetLoaderImporterAgree pins the L22 single source of truth:
// every path in the syntheticPackages table must resolve as kindSynthetic
// through Loader.ResolveImport AND build through Importer.Import with the
// matching package path. Before L22 the loader and the importer kept
// private switches that disagreed (e.g. "sync/atomic", "io", "unsafe"
// were synthetic to the importer but kindStdlib to the loader).
func TestSyntheticSetLoaderImporterAgree(t *testing.T) {
	for _, path := range sortedSyntheticPaths() {
		l := NewLoader()
		ref, err := l.ResolveImport("/tmp", path)
		if err != nil {
			t.Errorf("ResolveImport(%q) failed: %v", path, err)
			continue
		}
		if ref.Kind != kindSynthetic {
			t.Errorf("ResolveImport(%q).Kind = %v, want kindSynthetic (loader/importer drift)", path, ref.Kind)
		}
		imp := NewImporter(l, "/tmp", token.NewFileSet())
		pkg, err := imp.Import(path)
		if err != nil {
			t.Errorf("Import(%q) failed: %v", path, err)
			continue
		}
		if pkg.Path() != path {
			t.Errorf("Import(%q).Path() = %q", path, pkg.Path())
		}
	}
}

// mapLiteralStringKeys extracts the string keys of a top-level
// `var <name> = map[...]...{...}` composite literal from a Go source file,
// using go/ast (no regexes). It fails fast — never silently — if the
// variable cannot be found or the literal does not have the expected shape,
// so a refactor of the source file surfaces here instead of passing
// vacuously.
func mapLiteralStringKeys(t *testing.T, srcPath, varName string) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, srcPath, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", srcPath, err)
	}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Names) != 1 || vs.Names[0].Name != varName || len(vs.Values) != 1 {
				continue
			}
			cl, ok := vs.Values[0].(*ast.CompositeLit)
			if !ok {
				t.Fatalf("%s: var %s is not a composite literal; update the L22 parity locator", srcPath, varName)
			}
			keys := make(map[string]bool, len(cl.Elts))
			for _, e := range cl.Elts {
				kv, ok := e.(*ast.KeyValueExpr)
				if !ok {
					t.Fatalf("%s: var %s has a non-key/value element; update the L22 parity locator", srcPath, varName)
				}
				kl, ok := kv.Key.(*ast.BasicLit)
				if !ok || kl.Kind != token.STRING {
					t.Fatalf("%s: var %s has a non-string-literal key; update the L22 parity locator", srcPath, varName)
				}
				s, err := strconv.Unquote(kl.Value)
				if err != nil {
					t.Fatalf("%s: var %s: unquoting key: %v", srcPath, varName, err)
				}
				keys[s] = true
			}
			return keys
		}
	}
	t.Fatalf("%s: could not locate var %s; update the L22 parity locator", srcPath, varName)
	return nil
}

// TestSyntheticSetCoveredByFrozenOverlay asserts the third leg of the L22
// triple: every path p3frontend treats as synthetic must be importable by
// the frozen gofrontend overlay (internal/frontend/export.go), which
// type-checks the flattened output. The overlay admits a path when it is
// in syntheticSources (synth_machinery.go), has a dedicated stub
// (export.go), or resolves from disk (real GOROOT sources, e.g. fmt and
// sync/sync-atomic which the overlay compiles from disk).
//
// internal/frontend is package main and therefore unimportable from this
// module by construction (see go.mod); the test reads its capability from
// the same map literals the overlay consults, via go/ast — no copies, no
// regexes. A p3frontend synthetic with no overlay coverage would produce
// flattened code gofrontend cannot type-check, so this fails loudly.
func TestSyntheticSetCoveredByFrozenOverlay(t *testing.T) {
	root := p3repoRoot(t)
	syntheticSources := mapLiteralStringKeys(t, filepath.Join(root, "internal/frontend/synth_machinery.go"), "syntheticSources")
	dedicatedStubs := mapLiteralStringKeys(t, filepath.Join(root, "internal/frontend/export.go"), "dedicatedSyntheticStubs")
	goroot := NewLoader().GOROOT()
	diskImportable := func(path string) bool {
		st, err := os.Stat(filepath.Join(goroot, "src", path))
		return err == nil && st.IsDir()
	}
	for _, path := range sortedSyntheticPaths() {
		covered := syntheticSources[path] || dedicatedStubs[path] || diskImportable(path)
		if !covered {
			t.Errorf("synthetic %q is not importable by the frozen gofrontend overlay "+
				"(not in syntheticSources, not a dedicated stub, not on disk under %s): "+
				"flattened output importing it would fail gofrontend type-check",
				path, filepath.Join(goroot, "src"))
		}
	}
	// Informational only (not a failure): exporter synthetics the
	// p3frontend mirror has not reached yet. This is the capability-gap
	// radar — e.g. "os" compiles in the frozen exporter but p3frontend
	// still scope-errors it. Closing a gap means adding a mirror entry
	// (synthetic.go), never weakening this test.
	var gap []string
	for p := range syntheticSources {
		if _, ok := syntheticPackages[p]; !ok {
			gap = append(gap, p)
		}
	}
	sort.Strings(gap)
	t.Logf("exporter synthetic packages not mirrored by p3frontend (%d): %v", len(gap), gap)
}
