package typeparam

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"diffharness/internal/harness"
)

// TestLoadExclusionsEdgeCases pins LoadExclusions' edge-case semantics.
// The Java readExclusions parser (TypeparamDifferentialTest) runs the same
// fixture (testdata/exclusions-edge.yaml) and must produce the same file
// set — P2-8 conformance. If either parser's behavior changes, update both.
func TestLoadExclusionsEdgeCases(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	fixture := filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata", "exclusions-edge.yaml")
	version, excl, err := LoadExclusions(fixture)
	if err != nil {
		t.Fatalf("LoadExclusions: %v", err)
	}
	if version != "99" {
		t.Errorf("version = %q, want %q", version, "99")
	}
	// Note: neither parser unquotes detail values; both keep the raw text.
	want := map[string]Exclusion{
		"a.go": {File: "a.go", Reason: "deferred-parity", Detail: `"canonical order"`},
		"b.go": {File: "b.go", Reason: "backend-specific", Detail: `"out of order"`},
		"c.go": {File: "c.go", Reason: "deferred-parity", Detail: ""},
		"d.go": {File: "d.go", Reason: "unspecified-behavior", Detail: `""`},
	}
	if len(excl) != len(want) {
		t.Fatalf("parsed %d entries, want %d: %v", len(excl), len(want), excl)
	}
	for f, w := range want {
		got, ok := excl[f]
		if !ok {
			t.Errorf("missing entry %s", f)
			continue
		}
		if got != w {
			t.Errorf("%s = %+v, want %+v", f, got, w)
		}
	}
}

// testGoRoot resolves the gc toolchain root the way the harness tests do
// (DH_GO, GO binary path, else ~/tools/go) and skips the test when the
// toolchain is absent — without a working ref backend the differential
// cannot run at all.
func testGoRoot(t *testing.T) string {
	t.Helper()
	goRoot := os.Getenv("DH_GO")
	if goRoot == "" {
		if goBin := os.Getenv("GO"); goBin != "" {
			goRoot = filepath.Dir(filepath.Dir(goBin))
		}
	}
	if goRoot == "" {
		goRoot = os.ExpandEnv("${HOME}/tools/go")
	}
	if st, err := os.Stat(filepath.Join(goRoot, "bin", "go")); err != nil || st.IsDir() {
		t.Skipf("no go toolchain at %s", goRoot)
	}
	return goRoot
}

// testBatchConfig builds a two-backend config: gc-a runs the real go
// toolchain (the reference leg), graalgo runs the given launcher as its
// --check/--run command (the batch leg under test).
func testBatchConfig(t *testing.T, launcher string) *harness.Config {
	t.Helper()
	return &harness.Config{
		TimeoutSec: 15,
		Backends: []harness.Backend{
			{
				Name:  "gc-a",
				Vars:  map[string]string{"GO": testGoRoot(t)},
				Build: []string{"{GO}/bin/go", "build", "-o", "{BIN}", "{SRC}"},
				Run:   []string{"{BIN}"},
				Env:   map[string]string{"GOTOOLCHAIN": "local"},
			},
			{
				Name:  "graalgo",
				Vars:  map[string]string{"GRAALGO_RUN": launcher},
				Build: []string{"{GRAALGO_RUN}", "--check", "{SRC}"},
				Run:   []string{"{GRAALGO_RUN}", "{SRC}"},
			},
		},
	}
}

// TestRunBatchLauncherMissing pins B-L1: when the graalgo batch launcher
// cannot even start, Run must fail loudly. Before the fix, runGraalgoBatch
// returned an empty map with no error, so an errorcheck file (which both
// backends must fail to build) reported SAME — a vacuous PASS for a program
// that was never executed under graalgo at all — and compile/run files
// failed with an empty build log ("build: alt failed: ") that hid the dead
// launcher behind apparent per-file failures.
func TestRunBatchLauncherMissing(t *testing.T) {
	dir := t.TempDir()
	// A program gc rejects, so the errorcheck leg is "both must fail".
	bad := filepath.Join(dir, "bad.go")
	if err := os.WriteFile(bad, []byte("package main\n\nfunc main() { undefinedSymbol() }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := testBatchConfig(t, filepath.Join(dir, "no-such-launcher"))
	files := []TFile{{Name: "bad.go", Class: ClassErrorCheck, Srcs: []string{bad}}}
	comps, err := Run(cfg, files, "gc-a", "graalgo", 1)
	if err == nil {
		t.Fatalf("Run returned nil error with a missing batch launcher (comps=%+v); errorcheck file would PASS vacuously", comps)
	}
	if !strings.Contains(err.Error(), "batch") {
		t.Errorf("error %q should name the batch failure (distinguish 'batch crashed' from 'file failed to build')", err.Error())
	}
}

// TestRunBatchBadOutput pins the other two B-L1 fail-open paths: a launcher
// that exits 0 while printing unparseable JSON, and one that prints valid
// JSON with fewer results than batch entries, must both fail Run loudly
// instead of yielding empty per-file outcomes.
func TestRunBatchBadOutput(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload string
	}{
		{name: "unparseable", payload: "not json"},
		{name: "short", payload: "[]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			// Fake launcher: ignores its args, prints the payload, exits 0.
			script := filepath.Join(dir, "fake-launcher.sh")
			content := "#!/bin/sh\nprintf '%s\\n' '" + tc.payload + "'\n"
			if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
				t.Fatal(err)
			}
			bad := filepath.Join(dir, "bad.go")
			if err := os.WriteFile(bad, []byte("package main\n\nfunc main() { undefinedSymbol() }\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			cfg := testBatchConfig(t, script)
			files := []TFile{{Name: "bad.go", Class: ClassErrorCheck, Srcs: []string{bad}}}
			comps, err := Run(cfg, files, "gc-a", "graalgo", 1)
			if err == nil {
				t.Fatalf("Run returned nil error for %s batch output (comps=%+v); errorcheck file would PASS vacuously", tc.name, comps)
			}
			if !strings.Contains(err.Error(), "batch") {
				t.Errorf("error %q should name the batch failure", err.Error())
			}
		})
	}
}
