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

// TestRunGraalgoBatchCrashReturnsError (R1-30): a batch JVM that exits
// nonzero must surface as an error, never as an empty result map — the
// empty map used to read as zero Outcomes and pass errorcheck files
// vacuously.
func TestRunGraalgoBatchCrashReturnsError(t *testing.T) {
	workRoot := t.TempDir()
	batchPath := filepath.Join(workRoot, "batch.txt")
	if err := os.WriteFile(batchPath, []byte("check:/tmp/x.go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	alt := &harness.Backend{Name: "alt", Build: []string{"false"}}
	if _, err := runGraalgoBatch(&harness.Config{}, alt, batchPath, workRoot); err == nil {
		t.Errorf("runGraalgoBatch with crashing launcher: err = nil, want non-nil")
	}
}

// TestRunGraalgoBatchUnparseableJSONReturnsError (R1-30): batch output
// that is not a JSON array must surface as an error, never as an
// empty result map.
func TestRunGraalgoBatchUnparseableJSONReturnsError(t *testing.T) {
	workRoot := t.TempDir()
	batchPath := filepath.Join(workRoot, "batch.txt")
	if err := os.WriteFile(batchPath, []byte("check:/tmp/x.go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	alt := &harness.Backend{Name: "alt", Build: []string{"echo", "not-json"}}
	if _, err := runGraalgoBatch(&harness.Config{}, alt, batchPath, workRoot); err == nil {
		t.Errorf("runGraalgoBatch with non-JSON output: err = nil, want non-nil")
	}
}

// TestRunBatchedBatchFailureFailsErrorcheck (R1-30 positive control):
// with the reference backend failing (as gc would for an errorcheck
// file) and the graalgo batch JVM crashing, the errorcheck file must
// FAIL. Before the fix it passed: the missing batch result read as a
// zero Outcome (BuildOK=false), satisfying the errorcheck condition
// without graalgo ever running.
func TestRunBatchedBatchFailureFailsErrorcheck(t *testing.T) {
	workRoot := t.TempDir()
	src := filepath.Join(workRoot, "bad.go")
	if err := os.WriteFile(src, []byte("package main\nfunc main() { undefined }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ref := &harness.Backend{Name: "ref", Build: []string{"false"}}
	alt := &harness.Backend{Name: "alt", Build: []string{"false"}}
	included := []TFile{{Name: "bad.go", Class: ClassErrorCheck, Srcs: []string{src}}}
	comps := runBatched(&harness.Config{}, ref, alt, included, []int{0}, workRoot, 0)
	if len(comps) != 1 {
		t.Fatalf("runBatched: got %d comparisons, want 1", len(comps))
	}
	if comps[0].Same {
		t.Errorf("errorcheck file passed with crashed batch JVM; want FAIL (R1-30)")
	}
	found := false
	for _, r := range comps[0].Reasons {
		if strings.Contains(r, "batch:") {
			found = true
		}
	}
	if !found {
		t.Errorf("reasons %v do not name the batch failure", comps[0].Reasons)
	}
}
