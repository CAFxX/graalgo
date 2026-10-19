package main

// P1-13: buildSyntheticPackage / getSyntheticPackage return clean,
// position-carrying errors on broken committed synthetic sources instead
// of panicking. These tests inject deliberately broken sources through
// the syntheticSources registry (test-only fixture wiring) and assert a
// returned error — never a panic.

import (
	"strings"
	"testing"
)

// mustGetSyntheticPackage is the test helper for the error-returning
// getSyntheticPackage: it fails the test on build error (committed test
// sources must build) and never returns nil.
func mustGetSyntheticPackage(t *testing.T, path string) *syntheticPackage {
	t.Helper()
	sp, err := getSyntheticPackage(path)
	if err != nil {
		t.Fatalf("getSyntheticPackage(%q): unexpected error: %v", path, err)
	}
	if sp == nil || sp.pkg == nil {
		t.Fatalf("getSyntheticPackage(%q): returned nil package", path)
	}
	return sp
}

// withBrokenSyntheticSource installs src as the synthetic source for path
// for the duration of the test, evicting any cached build afterwards so
// the fixture cannot leak into other tests.
func withBrokenSyntheticSource(t *testing.T, path, src string) {
	t.Helper()
	syntheticSources[path] = src
	t.Cleanup(func() {
		delete(syntheticSources, path)
		synthMu.Lock()
		delete(synthCache, path)
		synthMu.Unlock()
	})
}

// assertSyntheticBuildError fails the test if err is nil or does not
// carry the expected diagnostic shape: the "gofrontend: synthetic"
// prefix, the failure kind, and file/position info.
func assertSyntheticBuildError(t *testing.T, err error, path, kind string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected a build error for broken synthetic source %q, got nil", path)
	}
	msg := err.Error()
	if !strings.Contains(msg, "gofrontend: synthetic") {
		t.Errorf("error should identify the synthetic package, got: %v", err)
	}
	if !strings.Contains(msg, kind) {
		t.Errorf("error should mention %q, got: %v", kind, err)
	}
	// Position info: parser/type-checker report as path.go:line:col.
	if !strings.Contains(msg, path+".go:") {
		t.Errorf("error should carry file/position info (%q.go:line:col), got: %v", path, err)
	}
}

func TestGetSyntheticPackageParseError(t *testing.T) {
	const path = "test/broken-synthetic-parse"
	withBrokenSyntheticSource(t, path, "package brokenparse\nfunc Foo( {\n}\n")

	// Explicit no-panic assertion: a panic here fails the test with the
	// recovered value instead of crashing the test binary.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("getSyntheticPackage panicked on broken source: %v", r)
		}
	}()

	_, err := getSyntheticPackage(path)
	assertSyntheticBuildError(t, err, path, "parse error")

	// Failures are not cached: fixing the source lets a later call succeed.
	syntheticSources[path] = "package brokenparse\nfunc Foo() {}\n"
	sp, err := getSyntheticPackage(path)
	if err != nil {
		t.Fatalf("getSyntheticPackage after fixing the source: unexpected error: %v", err)
	}
	if sp == nil || sp.pkg == nil {
		t.Fatal("getSyntheticPackage after fixing the source: nil package")
	}
}

func TestGetSyntheticPackageTypeError(t *testing.T) {
	const path = "test/broken-synthetic-type"
	withBrokenSyntheticSource(t, path,
		"package brokentype\nfunc Foo() int {\n    return undefinedIdent\n}\n")

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("getSyntheticPackage panicked on broken source: %v", r)
		}
	}()

	_, err := getSyntheticPackage(path)
	assertSyntheticBuildError(t, err, path, "type error")
	if !strings.Contains(err.Error(), "undefined: undefinedIdent") {
		t.Errorf("type error should name the undefined identifier, got: %v", err)
	}
}
