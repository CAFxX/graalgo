package typeparam

import (
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
