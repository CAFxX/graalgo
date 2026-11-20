package typeparam

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
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

// The `timing: true` marker reaches the scanned file (the verdict code reads
// it from the comparison, which copies it from here).
func TestScanCarriesTimingMarker(t *testing.T) {
	dir := t.TempDir()
	src := "// run\n\npackage main\n\nfunc main() {}\n"
	for _, n := range []string{"slow.go", "plain.go"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	y := filepath.Join(dir, "x.yaml")
	if err := os.WriteFile(y, []byte("version: 1\nexclusions:\n- file: slow.go\n  reason: deferred-parity\n  detail: BUG-0001\n  timing: true\n- file: plain.go\n  reason: deferred-parity\n  detail: BUG-0001\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, excl, err := LoadExclusions(y)
	if err != nil {
		t.Fatal(err)
	}
	files, err := Scan(dir, excl)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, f := range files {
		got[f.Name] = f.Timing
	}
	if !got["slow.go"] || got["plain.go"] {
		t.Fatalf("timing marker not carried per file: %v", got)
	}
}
