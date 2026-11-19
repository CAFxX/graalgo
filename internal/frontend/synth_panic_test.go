package main

// review-1804-p1-synth-panic: buildSyntheticPackage used to panic on a
// missing source, a parse error, or a type error, crashing any user
// compile that imported the broken package with a stack trace.
// getSyntheticPackage now returns errors; these tests pin the error
// path (never a panic), that failures are NOT cached, and that
// ExportFiles surfaces a broken synthetic import as a returned error.
//
// No CLI-level --check-synthetic test exists: main() exits the process,
// so the CLI is verified manually (healthy package prints ok, exit 0;
// missing path prints the diagnostic, exit 2). The unit tests below
// cover the same code path (getSyntheticPackage) plus the ExportFiles
// path a user compile takes.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mustSyntheticPackage builds (or reuses the cached) synthetic package
// for path, failing the test on error. Shared by the pre-existing
// synthetic tests updated for the error-returning getSyntheticPackage.
func mustSyntheticPackage(t *testing.T, path string) *syntheticPackage {
	t.Helper()
	sp, err := getSyntheticPackage(path)
	if err != nil {
		t.Fatalf("getSyntheticPackage(%q): %v", path, err)
	}
	return sp
}

// synthPanicTestPath is a synthetic path used only by these tests. It
// is non-internal, so the import gate admits it whenever it is present
// in syntheticSources (see importAdmitted).
const synthPanicTestPath = "testbroken/synthpanic"

// injectTestSynthetic installs src as the synthetic source for the
// test-only path, evicting any cached build, and returns a restore func
// that removes the entry and evicts the cache again. Callers defer the
// restore, so a broken entry can never leak into other tests.
func injectTestSynthetic(t *testing.T, src string) func() {
	t.Helper()
	synthMu.Lock()
	syntheticSources[synthPanicTestPath] = src
	delete(synthCache, synthPanicTestPath)
	synthMu.Unlock()
	return func() {
		synthMu.Lock()
		delete(syntheticSources, synthPanicTestPath)
		delete(synthCache, synthPanicTestPath)
		synthMu.Unlock()
	}
}

const (
	brokenParseSource = "package testbroken\nfunc Broken( {\n"
	brokenTypeSource  = "package testbroken\nfunc Broken() string { return 42 }\n"
	healthyTestSource = "package testbroken\nfunc Hello() string { return \"hi\" }\n"
)

func TestGetSyntheticPackageParseError(t *testing.T) {
	defer injectTestSynthetic(t, brokenParseSource)()
	sp, err := getSyntheticPackage(synthPanicTestPath)
	if err == nil {
		t.Fatalf("getSyntheticPackage(%q) = %+v, want error (unparseable source)", synthPanicTestPath, sp)
	}
	if !strings.Contains(err.Error(), "parse error") {
		t.Errorf("error = %v, want it to mention 'parse error'", err)
	}
	if !strings.Contains(err.Error(), synthPanicTestPath) {
		t.Errorf("error = %v, want it to name the path %q", err, synthPanicTestPath)
	}
}

func TestGetSyntheticPackageTypeError(t *testing.T) {
	defer injectTestSynthetic(t, brokenTypeSource)()
	sp, err := getSyntheticPackage(synthPanicTestPath)
	if err == nil {
		t.Fatalf("getSyntheticPackage(%q) = %+v, want error (type-error source)", synthPanicTestPath, sp)
	}
	if !strings.Contains(err.Error(), "type error") {
		t.Errorf("error = %v, want it to mention 'type error'", err)
	}
}

func TestGetSyntheticPackageMissingPath(t *testing.T) {
	sp, err := getSyntheticPackage("testbroken/definitely-missing")
	if err == nil {
		t.Fatalf("getSyntheticPackage(missing) = %+v, want error", sp)
	}
	if !strings.Contains(err.Error(), "no synthetic source") {
		t.Errorf("error = %v, want it to mention 'no synthetic source'", err)
	}
}

func TestGetSyntheticPackageErrorNotCached(t *testing.T) {
	defer injectTestSynthetic(t, brokenParseSource)()
	// First call fails ...
	if _, err := getSyntheticPackage(synthPanicTestPath); err == nil {
		t.Fatal("first call: want error (unparseable source)")
	}
	// ... and the second call re-builds and fails again (the failure
	// must not be cached as an error value).
	if _, err := getSyntheticPackage(synthPanicTestPath); err == nil {
		t.Fatal("second call: want error again, failure must not be cached")
	}
	// Repair the source: the next call must re-build and succeed,
	// proving the earlier failure was never cached.
	synthMu.Lock()
	syntheticSources[synthPanicTestPath] = healthyTestSource
	synthMu.Unlock()
	sp, err := getSyntheticPackage(synthPanicTestPath)
	if err != nil {
		t.Fatalf("after repair: getSyntheticPackage(%q): %v", synthPanicTestPath, err)
	}
	if sp.pkg.Scope().Lookup("Hello") == nil {
		t.Errorf("repaired package scope missing Hello")
	}
}

func TestExportFilesBrokenSyntheticReturnsError(t *testing.T) {
	defer injectTestSynthetic(t, brokenParseSource)()
	dir := t.TempDir()
	main := filepath.Join(dir, "main.go")
	src := "package main\n\nimport _ \"testbroken/synthpanic\"\n\nfunc main() {}\n"
	if err := os.WriteFile(main, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	// Must return an error, not panic: a broken synthetic import must
	// never crash a user compile with a stack trace.
	out, err := ExportFiles([]string{main}, "")
	if err == nil {
		t.Fatalf("ExportFiles importing broken synthetic = %q, want error", out)
	}
	if out != "" {
		t.Errorf("ExportFiles out = %q, want empty on error", out)
	}
	if !strings.Contains(err.Error(), synthPanicTestPath) {
		t.Errorf("error = %v, want it to name the broken path %q", err, synthPanicTestPath)
	}
}
