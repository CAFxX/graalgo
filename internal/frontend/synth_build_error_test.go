package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// P1-06 (review 2026-11-04): synthetic-package build failures used to
// abort gofrontend with a panic stack trace: buildSyntheticPackage
// panicked on a missing source, a parse error, and a type error, and
// only --check-synthetic recovered. These tests pin the fixed contract:
// builds return errors, failures are never cached, the overlay importer
// surfaces the failure, and ExportFiles returns a clean error instead
// of panicking — including for the classifier path, whose panic
// ExportFiles converts at its boundary.
//
// All tests below mutate the syntheticSources map and must NOT run in
// parallel with each other or with tests that build synthetic packages;
// they run sequentially (no t.Parallel) and restore every entry.

// withSyntheticSource temporarily replaces (or adds) a synthetic source,
// restoring the original afterwards. It fails the test if the path was
// already absent/present in the unexpected way for the case at hand.
func withSyntheticSource(t *testing.T, path, src string) {
	t.Helper()
	orig, ok := syntheticSources[path]
	if ok {
		t.Cleanup(func() { syntheticSources[path] = orig })
	} else {
		t.Cleanup(func() { delete(syntheticSources, path) })
	}
	syntheticSources[path] = src
}

func TestBuildSyntheticPackageUnknownPathReturnsError(t *testing.T) {
	const path = "definitely/not/a/synthetic/package"
	if _, ok := syntheticSources[path]; ok {
		t.Fatalf("test path %q unexpectedly present in syntheticSources", path)
	}
	if _, err := buildSyntheticPackage(path); err == nil {
		t.Fatal("buildSyntheticPackage(unknown path): got nil error, want a no-source error")
	} else if !strings.Contains(err.Error(), path) {
		t.Fatalf("buildSyntheticPackage error = %v, want it to name %q", err, path)
	}
	if _, err := getSyntheticPackage(path); err == nil {
		t.Fatal("getSyntheticPackage(unknown path): got nil error, want a no-source error")
	}
}

func TestBuildSyntheticPackageParseAndTypeErrors(t *testing.T) {
	const path = "test/synthbroken"
	if _, ok := syntheticSources[path]; ok {
		t.Fatalf("test path %q unexpectedly present in syntheticSources", path)
	}

	// Parse error: the source does not parse, so no package is built.
	withSyntheticSource(t, path, "package synthbroken\nfunc Broken( {\n")
	if _, err := buildSyntheticPackage(path); err == nil {
		t.Fatal("buildSyntheticPackage(unparseable source): got nil error")
	} else if !strings.Contains(err.Error(), "parse error") {
		t.Fatalf("buildSyntheticPackage error = %v, want a parse error", err)
	}

	// Type error: parses, but does not type-check.
	withSyntheticSource(t, path, "package synthbroken\nvar X = undefinedIdent\n")
	if _, err := buildSyntheticPackage(path); err == nil {
		t.Fatal("buildSyntheticPackage(ill-typed source): got nil error")
	} else if !strings.Contains(err.Error(), "type error") {
		t.Fatalf("buildSyntheticPackage error = %v, want a type error", err)
	}

	// A failed build must not poison later builds: with a valid source
	// restored, the same path builds fine (failures are never cached).
	withSyntheticSource(t, path, "package synthbroken\nfunc Hello() string { return \"hi\" }\n")
	sp, err := getSyntheticPackage(path)
	if err != nil {
		t.Fatalf("getSyntheticPackage after restoring a valid source: %v", err)
	}
	if sp.pkg.Scope().Lookup("Hello") == nil {
		t.Fatal("rebuilt package is missing Hello")
	}
}

func TestOverlayImporterReturnsSyntheticBuildError(t *testing.T) {
	const path = "test/synthbroken-import"
	withSyntheticSource(t, path, "package synthbroken\nfunc Broken( {\n")
	_, err := overlayImporter{}.Import(path)
	if err == nil {
		t.Fatal("overlayImporter.Import(corrupt synthetic): got nil error, want the parse error")
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("overlayImporter.Import error = %v, want it to name %q", err, path)
	}
}

func TestExportFilesCorruptSyntheticReturnsCleanError(t *testing.T) {
	const path = "test/synthbroken-export"
	withSyntheticSource(t, path, "package synthbroken\nfunc Broken( {\n")
	dir := t.TempDir()
	f := filepath.Join(dir, "main.go")
	src := "package main\n\nimport _ \"" + path + "\"\n\nfunc main() {}\n"
	if err := os.WriteFile(f, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	// Before the fix this call panicked with a stack trace; now it
	// must return a clean error naming the broken package. (The test
	// completing at all is the no-panic proof.)
	_, err := ExportFiles([]string{f}, "")
	if err == nil {
		t.Fatal("ExportFiles importing a corrupt synthetic package: got nil error")
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("ExportFiles error = %v, want it to name %q", err, path)
	}
}

func TestExportFilesValidSyntheticStillExports(t *testing.T) {
	// Positive control for the test above: a healthy synthetic import
	// still exports through the same path.
	dir := t.TempDir()
	f := filepath.Join(dir, "main.go")
	src := "package main\n\nimport \"errors\"\n\nvar _ = errors.New\n\nfunc main() {}\n"
	if err := os.WriteFile(f, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ExportFiles([]string{f}, ""); err != nil {
		t.Fatalf("ExportFiles importing healthy synthetic errors: %v", err)
	}
}

func TestMustSyntheticPackagePanicsWithBuildError(t *testing.T) {
	const path = "test/synthbroken-must"
	withSyntheticSource(t, path, "package synthbroken\nfunc Broken( {\n")
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("mustSyntheticPackage(corrupt source): got no panic, want the build error")
		}
		err, ok := r.(error)
		if !ok || !strings.Contains(err.Error(), path) {
			t.Fatalf("mustSyntheticPackage panic value = %v, want an error naming %q", r, path)
		}
		if _, ok := syntheticBuildError(r); !ok {
			t.Fatalf("syntheticBuildError did not recognize mustSyntheticPackage's panic: %v", r)
		}
	}()
	_ = mustSyntheticPackage(path)
}

func TestSyntheticBuildErrorClassification(t *testing.T) {
	// Build failures are recognized; unrelated panics are not (ExportFiles
	// must re-panic those instead of swallowing a genuine bug).
	if _, ok := syntheticBuildError(errTestSyntheticBuild); !ok {
		t.Fatal("syntheticBuildError did not recognize a synthetic build error")
	}
	if _, ok := syntheticBuildError(errTestUnrelated); ok {
		t.Fatal("syntheticBuildError recognized an unrelated error")
	}
	if _, ok := syntheticBuildError("gofrontend: synthetic x parse error: boom"); ok {
		t.Fatal("syntheticBuildError recognized a non-error panic value")
	}
}

var (
	errTestSyntheticBuild = &testError{"gofrontend: synthetic test/synthbroken parse error: boom"}
	errTestUnrelated      = &testError{"something else went wrong"}
)

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }

// TestExportFilesSyntheticBuildPanicRecovered pins the ExportFiles
// boundary recover: a corrupt synthetic package that the user program
// never imports — so type-checking succeeds — is first reached by the
// interface multi-result classifier (structural match on a
// user-declared interface with io.Reader's method shape), which can only
// report the failure by panicking. ExportFiles must convert that panic
// into a clean error, not a stack trace.
//
// The test is skipped when "io" is already cached (e.g. in a full-suite
// run): a cached package is never rebuilt, so corrupting its source
// cannot affect the run. Run standalone
// (go test -run TestExportFilesSyntheticBuildPanicRecovered) for the
// cold-cache path.
func TestExportFilesSyntheticBuildPanicRecovered(t *testing.T) {
	synthMu.Lock()
	_, cached := synthCache["io"]
	synthMu.Unlock()
	if cached {
		t.Skip("synthetic io already cached; corruption cannot affect this run (run standalone for the cold-cache path)")
	}
	withSyntheticSource(t, "io", "package io\nfunc Broken( {\n")
	dir := t.TempDir()
	f := filepath.Join(dir, "main.go")
	src := "package main\n\n" +
		"type Reader interface { Read([]byte) (int, error) }\n\n" +
		"func main() {\n\tvar r Reader\n\t_, _ = r.Read(nil)\n}\n"
	if err := os.WriteFile(f, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	// Before the fix this panicked; now it must return a clean error
	// naming io. (Completing at all is the no-panic proof.)
	_, err := ExportFiles([]string{f}, "")
	if err == nil {
		t.Fatal("ExportFiles with a corrupt classifier-reached synthetic package: got nil error")
	}
	if !strings.Contains(err.Error(), "io") {
		t.Fatalf("ExportFiles error = %v, want it to name io", err)
	}
}
