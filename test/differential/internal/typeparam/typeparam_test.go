package typeparam

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"diffharness/internal/harness"
)

// TestRunEmptyInputFailsLoudly pins R-1108-143: Run over zero programs
// must fail, never return an empty green report. Both vacuous shapes are
// errors — an empty scan and a total exclusion — and the guard runs before
// any backend lookup, so no backend config is needed. Each subcase is
// demonstrably capable of failing: without the guard, the empty-input
// case fails on the backend-lookup error instead of naming the empty
// input, and the all-excluded case returns (empty, nil).
func TestRunEmptyInputFailsLoudly(t *testing.T) {
	cfg := &harness.Config{}
	if _, err := Run(cfg, nil, "gc-a", "graalgo", 4); err == nil {
		t.Errorf("Run(nil files): got nil error, want an error naming the empty input")
	} else if !strings.Contains(err.Error(), "no input files") {
		t.Errorf("Run(nil files): error %q, want it to name the empty input", err)
	}
	excluded := []TFile{
		{Name: "a.go", Class: "exclude:deferred-parity"},
		{Name: "b.go", Class: "exclude:backend-specific"},
	}
	if _, err := Run(cfg, excluded, "gc-a", "graalgo", 4); err == nil {
		t.Errorf("Run(all excluded): got nil error, want an error naming the total exclusion")
	} else if !strings.Contains(err.Error(), "all 2 input files excluded") {
		t.Errorf("Run(all excluded): error %q, want it to name the total exclusion", err)
	}
}

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
