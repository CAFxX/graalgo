// Regression tests for the vacuous-pass guards (R08-H-01, R08-H-03):
// diffharness run / srctest run / typeparam run / snapshot record|check
// must fail instead of exiting 0 when they compare nothing — a
// one-backend config, or a corpus/exclusion set that leaves zero programs.
//
// These tests never execute a backend: the guarded commands fail before
// any program runs, so the configs use dummy /bin/true backends. The one
// control test (TestCmdRun_TwoBackendsCompares) does run the dummy backend
// to prove the guards are not over-broad.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"diffharness/internal/harness"
)

func writeFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// mkCorpus builds a corpus dir with one main.go per named program.
func mkCorpus(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		writeFixture(t, filepath.Join(dir, n, "main.go"),
			"package main\n\nfunc main() { println(\"hi\") }\n")
	}
	return dir
}

// mkConfig writes a backend config JSON with dummy /bin/true backends
// (never executed by the guarded paths) and the given exclusions.
func mkConfig(t *testing.T, backendNames []string, exclusions [][2]string) string {
	t.Helper()
	type backend struct {
		Name  string   `json:"name"`
		Build []string `json:"build"`
		Run   []string `json:"run"`
	}
	type exclusion struct {
		Program string `json:"program"`
		Reason  string `json:"reason"`
	}
	cfg := struct {
		Backends   []backend   `json:"backends"`
		Exclusions []exclusion `json:"exclusions"`
		TimeoutSec int         `json:"timeoutSec"`
	}{TimeoutSec: 15}
	for _, n := range backendNames {
		cfg.Backends = append(cfg.Backends, backend{
			Name: n, Build: []string{"/bin/true"}, Run: []string{"/bin/true"},
		})
	}
	for _, e := range exclusions {
		cfg.Exclusions = append(cfg.Exclusions, exclusion{Program: e[0], Reason: e[1]})
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "backends.json")
	writeFixture(t, p, string(data))
	return p
}

// mkExclusionsYAML writes a versioned exclusion file covering files.
func mkExclusionsYAML(t *testing.T, files ...string) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("version: 1\nexclusions:\n")
	for _, f := range files {
		b.WriteString("- file: " + f + "\n  reason: deferred-parity\n  detail: test fixture\n")
	}
	p := filepath.Join(t.TempDir(), "exclusions.yaml")
	writeFixture(t, p, b.String())
	return p
}

func TestCmdRun_OneBackendFails(t *testing.T) {
	cfg := mkConfig(t, []string{"gc-a"}, nil)
	corpus := mkCorpus(t, "01_hello")
	out := filepath.Join(t.TempDir(), "results.json")
	err := cmdRun([]string{"-config", cfg, "-corpus", corpus, "-out", out})
	if err == nil {
		t.Fatal("run with a one-backend config exited 0; want an error (0 comparisons are vacuous)")
	}
	if !strings.Contains(err.Error(), "at least 2 backends") {
		t.Fatalf("error should name the 2-backend requirement, got: %v", err)
	}
}

func TestCmdRun_AllExcludedFails(t *testing.T) {
	cfg := mkConfig(t, []string{"gc-a", "graalgo"},
		[][2]string{{"01_hello", "flaky"}})
	corpus := mkCorpus(t, "01_hello")
	out := filepath.Join(t.TempDir(), "results.json")
	err := cmdRun([]string{"-config", cfg, "-corpus", corpus, "-out", out})
	if err == nil {
		t.Fatal("run with a fully-excluded corpus exited 0; want an error (0 comparisons are vacuous)")
	}
	if !strings.Contains(err.Error(), "0 comparisons") {
		t.Fatalf("error should report 0 comparisons, got: %v", err)
	}
	// The report is still written, so CI keeps the evidence of what was skipped.
	data, rerr := os.ReadFile(out)
	if rerr != nil {
		t.Fatalf("run should still write its report: %v", rerr)
	}
	var rep runReport
	if jerr := json.Unmarshal(data, &rep); jerr != nil {
		t.Fatalf("report is not valid JSON: %v", jerr)
	}
	if rep.Summary["total"] != 0 || rep.Summary["skipped"] != 1 {
		t.Fatalf("report summary = %v, want total=0 skipped=1", rep.Summary)
	}
}

// TestCmdRun_TwoBackendsCompares is the positive control: two backends over
// one (dummy-built) program yields one comparison and exits 0. It proves
// the guards above are not over-broad.
func TestCmdRun_TwoBackendsCompares(t *testing.T) {
	cfg := mkConfig(t, []string{"gc-a", "gc-b"}, nil)
	corpus := mkCorpus(t, "01_hello")
	out := filepath.Join(t.TempDir(), "results.json")
	if err := cmdRun([]string{"-config", cfg, "-corpus", corpus, "-out", out}); err != nil {
		t.Fatalf("run with two backends and one program should exit 0, got: %v", err)
	}
	data, rerr := os.ReadFile(out)
	if rerr != nil {
		t.Fatalf("read report: %v", rerr)
	}
	var rep runReport
	if jerr := json.Unmarshal(data, &rep); jerr != nil {
		t.Fatalf("report is not valid JSON: %v", jerr)
	}
	if rep.Summary["total"] != 1 {
		t.Fatalf("report summary = %v, want total=1", rep.Summary)
	}
}

func TestCmdTypeparamRun_AllExcludedFails(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, filepath.Join(dir, "a.go"), "// run\n\npackage main\n\nfunc main() {}\n")
	excl := mkExclusionsYAML(t, "a.go")
	cfg := mkConfig(t, []string{"gc-a", "graalgo"}, nil)
	out := filepath.Join(t.TempDir(), "report.json")
	err := cmdTypeparamRun([]string{"-dir", dir, "-exclusions", excl, "-out", out,
		"-jobs", "1", "-config", cfg})
	if err == nil {
		t.Fatal("typeparam run with a fully-excluded file set exited 0; want an error (0 comparisons are vacuous)")
	}
	if !strings.Contains(err.Error(), "0 comparisons") {
		t.Fatalf("error should report 0 comparisons, got: %v", err)
	}
}

func TestCmdSrctestRun_AllExcludedFails(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, filepath.Join(dir, "a.go"),
		"package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(\"hi\") }\n")
	excl := mkExclusionsYAML(t, "a.go")
	cfg := mkConfig(t, []string{"gc-a", "graalgo"}, nil)
	out := filepath.Join(t.TempDir(), "report.json")
	err := cmdSrctestRun([]string{"-dir", dir, "-exclusions", excl, "-out", out,
		"-jobs", "1", "-config", cfg})
	if err == nil {
		t.Fatal("srctest run with a fully-excluded file set exited 0; want an error (0 comparisons are vacuous)")
	}
	if !strings.Contains(err.Error(), "0 comparisons") {
		t.Fatalf("error should report 0 comparisons, got: %v", err)
	}
}

// mkSnapshot writes a snapshot file with the given programs/exclusions.
func mkSnapshot(t *testing.T, programs []string, exclusions [][2]string) string {
	t.Helper()
	snap := SnapshotFile{
		Format:     snapshotFormatV1,
		Toolchain:  "go1.27.1",
		Backend:    "graalgo",
		RecordedAt: "2026-11-09T00:00:00Z",
		Corpus:     "corpus",
		Normalizer: snapshotNormalizerV1,
		Excluded:   []harness.Exclusion{},
	}
	for _, p := range programs {
		snap.Programs = append(snap.Programs, SnapshotEntry{
			Name: p, BuildOK: true, ExitCode: 0, Stdout: "hi\n",
		})
	}
	for _, e := range exclusions {
		snap.Excluded = append(snap.Excluded,
			harness.Exclusion{Program: e[0], Reason: e[1]})
	}
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "snap.json")
	writeFixture(t, p, string(data))
	return p
}

func TestCmdSnapshotRecord_AllExcludedFails(t *testing.T) {
	cfg := mkConfig(t, []string{"gc-a", "graalgo"},
		[][2]string{{"01_hello", "flaky"}})
	corpus := mkCorpus(t, "01_hello")
	out := filepath.Join(t.TempDir(), "snap.json")
	err := cmdSnapshotRecord([]string{"-config", cfg, "-corpus", corpus,
		"-backend", "graalgo", "-out", out})
	if err == nil {
		t.Fatal("snapshot record with a fully-excluded corpus exited 0; want an error (it would write an empty snapshot)")
	}
	if !strings.Contains(err.Error(), "empty snapshot") {
		t.Fatalf("error should refuse an empty snapshot, got: %v", err)
	}
	if _, serr := os.Stat(out); !os.IsNotExist(serr) {
		t.Fatalf("record must not write a snapshot file on refusal (stat: %v)", serr)
	}
}

func TestCmdSnapshotCheck_EmptySnapshotFails(t *testing.T) {
	cfg := mkConfig(t, []string{"gc-a", "graalgo"}, nil)
	corpus := mkCorpus(t, "01_hello")
	snap := mkSnapshot(t, nil, nil)
	err := cmdSnapshotCheck([]string{"-config", cfg, "-corpus", corpus,
		"-backend", "graalgo", "-snapshot", snap})
	if err == nil {
		t.Fatal("snapshot check against an empty snapshot exited 0; want an error (0 programs, 0 drifted is vacuous)")
	}
	if !strings.Contains(err.Error(), "0 programs") {
		t.Fatalf("error should report 0 programs, got: %v", err)
	}
}

func TestCmdSnapshotCheck_AllExcludedFails(t *testing.T) {
	// Snapshot and config agree on the exclusion, so the exclusion-drift
	// gate passes and only the zero-program guard can fire.
	cfg := mkConfig(t, []string{"gc-a", "graalgo"},
		[][2]string{{"01_hello", "flaky"}})
	corpus := mkCorpus(t, "01_hello")
	snap := mkSnapshot(t, []string{"01_hello"}, [][2]string{{"01_hello", "flaky"}})
	err := cmdSnapshotCheck([]string{"-config", cfg, "-corpus", corpus,
		"-backend", "graalgo", "-snapshot", snap})
	if err == nil {
		t.Fatal("snapshot check with a fully-excluded corpus exited 0; want an error")
	}
	if !strings.Contains(err.Error(), "excluded") {
		t.Fatalf("error should name the exclusion, got: %v", err)
	}
}
