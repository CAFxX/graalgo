package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Regression tests for the fail-closed gates (review21-fuzzgates): a run
// that compares zero programs must not exit 0 as "green". Both cmdRun
// and cmdSrctestRun fail on zero comparisons by default and pass only
// with an explicit -allow-empty.

// writeDummyConfig writes a two-backend config whose commands are never
// executed in these tests (all programs are excluded), but which passes
// LoadConfig validation (non-empty build/run templates, unique names).
func writeDummyConfig(t *testing.T, dir string) string {
	t.Helper()
	cfg := `{"backends": [
		{"name": "gc-a", "build": ["true"], "run": ["true"]},
		{"name": "graalgo", "build": ["true"], "run": ["true"]}
	], "timeoutSec": 15}`
	p := filepath.Join(dir, "backends.json")
	if err := os.WriteFile(p, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCmdRun_ZeroComparisonsFails(t *testing.T) {
	dir := t.TempDir()
	corpus := filepath.Join(dir, "corpus", "prog")
	if err := os.MkdirAll(corpus, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(corpus, "main.go"),
		[]byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Exclude the only program, so the kept set — and the comparison
	// count — is zero.
	cfgPath := filepath.Join(dir, "backends-excl.json")
	cfg := `{"backends": [
		{"name": "gc-a", "build": ["true"], "run": ["true"]},
		{"name": "graalgo", "build": ["true"], "run": ["true"]}
	], "exclusions": [{"program": "prog", "reason": "test"}],
	   "timeoutSec": 15}`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "report.json")

	if err := cmdRun([]string{"-config", cfgPath, "-corpus",
		filepath.Join(dir, "corpus"), "-out", out}); err == nil {
		t.Fatal("cmdRun with zero comparisons exited nil, want an error")
	} else if !strings.Contains(err.Error(), "no comparisons") {
		t.Fatalf("cmdRun error = %v, want it to mention no comparisons", err)
	}

	// Explicit opt-out restores the old behaviour for callers that
	// genuinely expect an empty run.
	if err := cmdRun([]string{"-config", cfgPath, "-corpus",
		filepath.Join(dir, "corpus"), "-out", out, "-allow-empty"}); err != nil {
		t.Fatalf("cmdRun -allow-empty = %v, want nil", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var rep runReport
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Summary["total"] != 0 {
		t.Fatalf("report total = %d, want 0", rep.Summary["total"])
	}
}

func TestCmdRun_NonEmptyStillPasses(t *testing.T) {
	// Positive control: the zero-comparison guard must not break a
	// normal run. Both dummy backends are `true`, so the single
	// program compares SAME.
	dir := t.TempDir()
	corpus := filepath.Join(dir, "corpus", "prog")
	if err := os.MkdirAll(corpus, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(corpus, "main.go"),
		[]byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgPath := writeDummyConfig(t, dir)
	out := filepath.Join(dir, "report.json")
	if err := cmdRun([]string{"-config", cfgPath, "-corpus",
		filepath.Join(dir, "corpus"), "-out", out}); err != nil {
		t.Fatalf("cmdRun normal run = %v, want nil", err)
	}
}

func TestCmdSrctestRun_ZeroComparisonsFails(t *testing.T) {
	dir := t.TempDir()
	testDir := filepath.Join(dir, "test")
	if err := os.MkdirAll(testDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(testDir, "prog.go"),
		[]byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	excl := filepath.Join(dir, "exclusions.yaml")
	if err := os.WriteFile(excl,
		[]byte("version: 1\nexclusions:\n- file: prog.go\n  reason: backend-specific\n  detail: test exclusion\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	cfgPath := writeDummyConfig(t, dir)
	out := filepath.Join(dir, "report.json")
	args := []string{"-dir", testDir, "-exclusions", excl,
		"-config", cfgPath, "-jobs", "1", "-out", out}

	if err := cmdSrctestRun(args); err == nil {
		t.Fatal("cmdSrctestRun with zero comparisons exited nil, want an error")
	} else if !strings.Contains(err.Error(), "no comparisons") {
		t.Fatalf("cmdSrctestRun error = %v, want it to mention no comparisons", err)
	}
	if err := cmdSrctestRun(append(append([]string{}, args...), "-allow-empty")); err != nil {
		t.Fatalf("cmdSrctestRun -allow-empty = %v, want nil", err)
	}
}
