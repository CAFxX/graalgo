package typeparam

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"diffharness/internal/harness"
)

// goBackend builds a harness.Backend pointing at the local go toolchain for
// both build and run. It deliberately does NOT support --batch: using it as
// the "alt" backend makes runGraalgoBatch fail, exercising the per-file
// fallback in runBatched.
func goBackend(t *testing.T, name string) *harness.Backend {
	t.Helper()
	goRoot := os.Getenv("DH_GO")
	if goRoot == "" {
		goRoot = os.ExpandEnv("${HOME}/tools/go")
	}
	return &harness.Backend{
		Name:  name,
		Vars:  map[string]string{"GO": goRoot},
		Build: []string{"{GO}/bin/go", "build", "-o", "{BIN}", "{SRC}"},
		Run:   []string{"{BIN}"},
		Env:   map[string]string{"GOMAXPROCS": "1", "GOTOOLCHAIN": "local"},
	}
}

// TestRunBatchedBatchDeathFallback pins the batch-death fallback: when the
// --batch JVM dies (or the alt backend has no batch support), runBatched
// must fall back to per-file invocations rather than reporting every entry
// as a build failure with an empty log.
//
// Observed 2026-10-18: ConvertNode's uncaught IllegalArgumentException on
// bigmap.go kills the batch JVM mid-run; without the fallback, the whole
// job's entries come back "build: alt failed: " (empty log) — a false
// XFAIL that would hide real XPASSes from the recheck.
func TestRunBatchedBatchDeathFallback(t *testing.T) {
	ref := goBackend(t, "ref")
	alt := goBackend(t, "alt") // no --batch support: batch always "dies"
	cfg := &harness.Config{Backends: []harness.Backend{*ref, *alt}, TimeoutSec: 60}

	_, thisFile, _, _ := runtime.Caller(0)
	td := filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata")
	mkFile := func(name, cls string) TFile {
		return TFile{
			Name:  name,
			Class: cls,
			Srcs:  []string{filepath.Join(td, "hello", "main.go")},
		}
	}
	files := []TFile{mkFile("hello", ClassRun), mkFile("hello2", ClassRun)}

	comps := runBatched(cfg, ref, alt, files, []int{0, 1}, t.TempDir(), 0)
	if len(comps) != 2 {
		t.Fatalf("want 2 comparisons, got %d", len(comps))
	}
	for _, c := range comps {
		if !c.Same {
			t.Errorf("%s: want Same (per-file fallback), got reasons %v", c.Program, c.Reasons)
		}
	}
}

// TestRunBatchedBatchDeathFallbackBuildFailure ensures the fallback still
// reports a genuine build failure (not a phantom batch-death): a program
// gc rejects must come back errorcheck-clean via the fallback path.
func TestRunBatchedBatchDeathFallbackBuildFailure(t *testing.T) {
	ref := goBackend(t, "ref")
	alt := goBackend(t, "alt")
	cfg := &harness.Config{Backends: []harness.Backend{*ref, *alt}, TimeoutSec: 60}

	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.go")
	// Invalid Go: both backends must fail to build (errorcheck parity).
	if err := os.WriteFile(bad, []byte("package main\n\nvar x = '\\x'\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files := []TFile{{Name: "bad.go", Class: ClassErrorCheck, Srcs: []string{bad}}}

	comps := runBatched(cfg, ref, alt, files, []int{0}, t.TempDir(), 0)
	if len(comps) != 1 {
		t.Fatalf("want 1 comparison, got %d", len(comps))
	}
	if !comps[0].Same {
		t.Errorf("want errorcheck parity via fallback, got reasons %v", comps[0].Reasons)
	}
}
