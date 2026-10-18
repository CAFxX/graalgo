package typeparam

import (
	"encoding/json"
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

// TestWriteReportJSONXpass pins the recheck report shape: the xpass list is
// reported distinctly (summary["xpass"] + xpass_programs) and a normal run
// (nil xpass) reports zero.
func TestWriteReportJSONXpass(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.json")
	comps := []Comparison{
		{Program: "a.go", Same: true},
		{Program: "b.go", Same: false, Reasons: []string{"diverged"}},
	}
	if err := WriteReportJSON(path, comps, 3, []string{"a.go"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Summary       map[string]int `json:"summary"`
		XpassPrograms []string       `json:"xpass_programs"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("report is not valid JSON: %v", err)
	}
	if doc.Summary["xpass"] != 1 {
		t.Errorf("summary[xpass] = %d, want 1", doc.Summary["xpass"])
	}
	if doc.Summary["skipped"] != 3 {
		t.Errorf("summary[skipped] = %d, want 3", doc.Summary["skipped"])
	}
	if doc.Summary["diverged"] != 1 {
		t.Errorf("summary[diverged] = %d, want 1", doc.Summary["diverged"])
	}
	if len(doc.XpassPrograms) != 1 || doc.XpassPrograms[0] != "a.go" {
		t.Errorf("xpass_programs = %v, want [a.go]", doc.XpassPrograms)
	}

	// A normal run (nil xpass) reports zero unexpected passes.
	path2 := filepath.Join(dir, "report2.json")
	if err := WriteReportJSON(path2, comps, 0, nil); err != nil {
		t.Fatal(err)
	}
	data2, err := os.ReadFile(path2)
	if err != nil {
		t.Fatal(err)
	}
	var doc2 struct {
		Summary map[string]int `json:"summary"`
	}
	if err := json.Unmarshal(data2, &doc2); err != nil {
		t.Fatal(err)
	}
	if doc2.Summary["xpass"] != 0 {
		t.Errorf("normal-run summary[xpass] = %d, want 0", doc2.Summary["xpass"])
	}
}
