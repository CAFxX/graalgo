package typeparam

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
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
		"a.go": {File: "a.go", Reason: "deferred-parity", Detail: `"canonical order"`, Revalidated: "2026-10-04", Issue: "F-1"},
		"b.go": {File: "b.go", Reason: "backend-specific", Detail: `"out of order"`, Revalidated: "2026-10-05"},
		"c.go": {File: "c.go", Reason: "deferred-parity", Detail: "", Revalidated: "2026-10-04"},
		"d.go": {File: "d.go", Reason: "unspecified-behavior", Detail: `""`, Revalidated: "2026-10-04"},
		"e.go": {File: "e.go", Reason: "backend-specific", Detail: `"no date"`},
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

// TestExclusionListsFresh is the CI gate for the exclusion amnesty (P1-Q)
// on the versioned YAML lists: every entry of typeparam-exclusions.yaml
// and srctest-exclusions.yaml must carry a last-revalidated date no older
// than harness.MaxExclusionAgeDays, so a fixed bug cannot hide behind its
// exclusion forever. Runs in `go test ./internal/...` (run.sh step 3, CI).
func TestExclusionListsFresh(t *testing.T) {
	for _, name := range []string{"typeparam-exclusions.yaml", "srctest-exclusions.yaml"} {
		path := filepath.Join("..", "..", name)
		_, excl, err := LoadExclusions(path)
		if err != nil {
			t.Fatalf("LoadExclusions(%s): %v", name, err)
		}
		if len(excl) == 0 {
			t.Fatalf("%s: no exclusions parsed; gate vacuous", name)
		}
		for _, p := range CheckExclusionFreshness(excl) {
			t.Errorf("%s: %s", name, p)
		}
	}
}

// TestCheckExclusionFreshnessPinsStale pins the stale-entry detection: a
// map with one stale entry must produce exactly one problem naming it,
// and a fresh map must produce none.
func TestCheckExclusionFreshnessPinsStale(t *testing.T) {
	today := time.Now().UTC().Truncate(24 * time.Hour).Format("2006-01-02")
	excl := map[string]Exclusion{
		"fresh.go":  {File: "fresh.go", Reason: "backend-specific", Revalidated: today},
		"stale.go":  {File: "stale.go", Reason: "backend-specific", Revalidated: "2000-01-01"},
		"nodate.go": {File: "nodate.go", Reason: "backend-specific"},
	}
	problems := CheckExclusionFreshness(excl)
	if len(problems) != 2 {
		t.Fatalf("want 2 problems (stale.go, nodate.go), got %d: %v", len(problems), problems)
	}
	// Sorted by file name: nodate.go before stale.go.
	if !strings.Contains(problems[0], "nodate.go") || !strings.Contains(problems[1], "stale.go") {
		t.Errorf("problems misattributed: %v", problems)
	}
}

// TestFilterExcludedKeepsOnlyExcluded pins the amnesty-mode file
// selection: from a naturally-classified scan (no YAML override) only the
// files named on the exclusion list are kept for re-probing.
func TestFilterExcludedKeepsOnlyExcluded(t *testing.T) {
	files := []TFile{
		{Name: "a.go", Class: "run"},
		{Name: "b.go", Class: "exclude:deferred-parity"},
		{Name: "c.go", Class: "compile"},
	}
	excl := map[string]Exclusion{
		"a.go": {File: "a.go", Reason: "backend-specific"},
		"b.go": {File: "b.go", Reason: "deferred-parity"},
	}
	got := FilterExcluded(files, excl)
	if len(got) != 2 || got[0].Name != "a.go" || got[1].Name != "b.go" {
		t.Fatalf("FilterExcluded = %+v, want [a.go b.go]", got)
	}
	if got := FilterExcluded(files, nil); len(got) != 0 {
		t.Fatalf("FilterExcluded with nil exclusions = %+v, want []", got)
	}
}
