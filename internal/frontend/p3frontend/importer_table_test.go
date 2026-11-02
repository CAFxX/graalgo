package p3frontend

// Lane F (review 2026-11-02 R13 + P2-12): the mirrored synthetic
// package set must have exactly one enumeration — the syntheticMirrors
// table. The importer used to enumerate the set in a ~30-case switch
// while the loader enumerated it in a second switch, and the two had
// already drifted (the importer served io, sync/atomic and unsafe;
// the loader did not). These tests pin that Import and ResolveImport
// agree on the table, that the dependency edges are acyclic, that
// dependency objects are shared (not rebuilt), and that a drift
// between the sync spec table and its interpreter surfaces as an
// error, not a panic.

import (
	"go/token"
	"sort"
	"strings"
	"testing"
)

func newTestImporter() *Importer {
	return NewImporter(NewLoader(), "/tmp", token.NewFileSet())
}

// TestSyntheticMirrorTableServesAll pins that every table key imports
// to a package of the same path, and that a second import returns the
// cached (identical) package.
func TestSyntheticMirrorTableServesAll(t *testing.T) {
	keys := make([]string, 0, len(syntheticMirrors))
	for k := range syntheticMirrors {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		t.Fatal("syntheticMirrors is empty")
	}
	for _, path := range keys {
		imp := newTestImporter()
		pkg, err := imp.Import(path)
		if err != nil {
			t.Errorf("Import(%q) failed: %v", path, err)
			continue
		}
		if pkg.Path() != path {
			t.Errorf("Import(%q).Path() = %q", path, pkg.Path())
		}
		again, err := imp.Import(path)
		if err != nil {
			t.Errorf("second Import(%q) failed: %v", path, err)
		} else if again != pkg {
			t.Errorf("second Import(%q) rebuilt the package instead of returning the cache", path)
		}
	}
}

// TestSyntheticMirrorTableAcyclic pins that the deps/dep edges form a
// DAG: Import follows them recursively, so a cycle would hang or
// recurse forever instead of failing.
func TestSyntheticMirrorTableAcyclic(t *testing.T) {
	var visit func(path string, stack []string)
	visit = func(path string, stack []string) {
		for _, p := range stack {
			if p == path {
				t.Fatalf("mirror dependency cycle: %s", strings.Join(append(append([]string{}, stack...), path), " -> "))
			}
		}
		m, ok := syntheticMirrors[path]
		if !ok {
			t.Fatalf("mirror %q depends on unknown path %q", stack, path)
		}
		stack = append(stack, path)
		for _, dep := range m.deps {
			visit(dep, stack)
		}
		if m.dep != "" {
			visit(m.dep, stack)
		}
	}
	keys := make([]string, 0, len(syntheticMirrors))
	for k := range syntheticMirrors {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, path := range keys {
		visit(path, nil)
	}
}

// TestSyntheticMirrorDepsCached pins the dependency behaviour the old
// switch implemented inline: importing bytes caches io, importing
// context caches time, and importing pem caches base64.
func TestSyntheticMirrorDepsCached(t *testing.T) {
	cases := []struct{ path, dep string }{
		{"bytes", "io"},
		{"context", "time"},
		{"encoding/pem", "encoding/base64"},
	}
	for _, c := range cases {
		imp := newTestImporter()
		if _, err := imp.Import(c.path); err != nil {
			t.Fatalf("Import(%q) failed: %v", c.path, err)
		}
		if _, ok := imp.pkgs[c.dep]; !ok {
			t.Errorf("Import(%q) did not cache dependency %q", c.path, c.dep)
		}
	}
}

// TestLoaderClassifiesMirrorsSynthetic pins that the loader's synthetic
// classification is exactly the mirror table — no second list to drift
// — and that a genuine stdlib path outside the table stays non-synthetic.
func TestLoaderClassifiesMirrorsSynthetic(t *testing.T) {
	l := NewLoader()
	keys := make([]string, 0, len(syntheticMirrors))
	for k := range syntheticMirrors {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, path := range keys {
		ref, err := l.ResolveImport("/tmp", path)
		if err != nil {
			t.Errorf("ResolveImport(%q) failed: %v", path, err)
			continue
		}
		if ref.Kind != kindSynthetic {
			t.Errorf("ResolveImport(%q).Kind = %v, want kindSynthetic (loader must agree with the mirror table)", path, ref.Kind)
		}
	}
	// Boundary: a stdlib path with no mirror is not synthetic.
	// "os" resolves via GOROOT in this environment (see also
	// TestStdlibScopeError's os pin).
	if ref, err := l.ResolveImport("/tmp", "os"); err != nil {
		t.Fatalf("ResolveImport(os) failed: %v", err)
	} else if ref.Kind == kindSynthetic {
		t.Error("ResolveImport(os) classified a non-mirror stdlib path synthetic")
	}
}

// TestSyncBuilderErrorOnDrift pins P2-12: a syncMethodSpecs entry
// naming a type the interpreter does not know must surface as an
// error from the builder (and therefore from Import), never as a
// panic crashing the frontend. The old code panicked here.
func TestSyncBuilderErrorOnDrift(t *testing.T) {
	saved := syncMethodSpecs["Mutex"]
	syncMethodSpecs["Mutex"] = append(append([]struct {
		name    string
		params  []string
		results []string
	}{}, saved...), struct {
		name    string
		params  []string
		results []string
	}{"Bogus", []string{"x:nosuchtype"}, nil})
	defer func() { syncMethodSpecs["Mutex"] = saved }()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("syntheticSyncPackage panicked on spec drift: %v (must return an error)", r)
		}
	}()
	pkg, err := syntheticSyncPackage()
	if err == nil {
		t.Fatalf("syntheticSyncPackage with a drifted spec succeeded (pkg %v); want an error", pkg)
	}
	if !strings.Contains(err.Error(), "unknown sync param type") {
		t.Fatalf("unexpected drift error: %v", err)
	}
	// And through Import: the table entry propagates the error.
	imp := newTestImporter()
	if _, err := imp.Import("sync"); err == nil {
		t.Fatal("Import(sync) with a drifted spec succeeded; want an error")
	} else if !strings.Contains(err.Error(), "unknown sync param type") {
		t.Fatalf("unexpected Import drift error: %v", err)
	}
}
