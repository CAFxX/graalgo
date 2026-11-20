package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"diffharness/internal/harness"
	"diffharness/internal/typeparam"
)

func mkEntry(name string) SnapshotEntry {
	return SnapshotEntry{Name: name, BuildOK: true, Stdout: "hello\n"}
}

func TestDiffEntries_Equal(t *testing.T) {
	if d := diffEntries(mkEntry("p"), mkEntry("p")); d != "" {
		t.Fatalf("equal entries reported drift: %s", d)
	}
}

func TestDiffEntries_StdoutDiff(t *testing.T) {
	a, b := mkEntry("p"), mkEntry("p")
	b.Stdout = "goodbye\n"
	d := diffEntries(a, b)
	if !strings.Contains(d, "stdout") || !strings.Contains(d, "line 1") {
		t.Fatalf("drift description should name field and line: %q", d)
	}
}

func TestDiffEntries_ExitCode(t *testing.T) {
	a, b := mkEntry("p"), mkEntry("p")
	b.ExitCode = 2
	if d := diffEntries(a, b); !strings.Contains(d, "exitCode") {
		t.Fatalf("exit code drift not reported: %q", d)
	}
}

func TestDiffEntries_BuildLogSameBackend(t *testing.T) {
	a, b := mkEntry("p"), mkEntry("p")
	a.BuildOK, b.BuildOK = false, false
	a.BuildLog, b.BuildLog = "old error", "new error"
	if d := diffEntries(a, b); !strings.Contains(d, "buildLog") {
		t.Fatalf("build log drift not reported: %q", d)
	}
	// Cross-toolchain comparison ignores compiler-specific build logs ...
	if d := diffBehavior(a, b); d != "" {
		t.Fatalf("cross-toolchain build logs compared: %q", d)
	}
	// ... but never the build outcome.
	b.BuildOK = true
	if d := diffBehavior(a, b); !strings.Contains(d, "buildOk") {
		t.Fatalf("build outcome difference missed: %q", d)
	}
}

func TestScrubEnvPaths(t *testing.T) {
	in := "build [/home/u/c/internal/backend/graalgo-sync-run --check /home/u/c/test/differential/corpus/13_x/main.go] failed"
	got := scrubEnvPaths(in, "/home/u/c/test/differential/corpus")
	if strings.Contains(got, "/home/u/c") {
		t.Fatalf("machine paths not scrubbed: %q", got)
	}
	if !strings.Contains(got, "{GRAALGO_RUN}") || !strings.Contains(got, "{CORPUS}/13_x/main.go") {
		t.Fatalf("scrub tokens wrong: %q", got)
	}
	if out := scrubEnvPaths("hello, world\n", "/x"); out != "hello, world\n" {
		t.Fatalf("program output mangled: %q", out)
	}
}

func TestClassify(t *testing.T) {
	exp := map[string]harness.ReviewedExclusion{
		"x": {File: "x", Reason: "deferred-parity", Detail: "d", Signature: harness.DiffSignature("stdout differs")},
		"u": {File: "u", Reason: "deferred-parity", Detail: "unpinned"},
	}
	for _, c := range []struct {
		name, diff string
		infra      bool
		want       string
	}{
		{"y", "", false, "MATCH"},
		{"y", "stdout differs", false, "DRIFT"},
		{"x", "stdout differs", false, "EXPECTED"},
		// G2: a listed program diverging in a NEW way (a crash instead of
		// the pinned wrong output) is drift, not expected.
		{"x", "exitCode: want=0 got=1 (stderr: fatal internal error)", false, "DRIFT"},
		{"u", "stdout differs", false, "UNPINNED"},
		{"x", "", false, "STALE"},
		{"y", "buildOk", true, "INFRA"},
	} {
		if got := classify(c.name, c.diff, c.infra, exp).kind; got != c.want {
			t.Errorf("classify(%s,%q,%v)=%s want %s", c.name, c.diff, c.infra, got, c.want)
		}
	}
}

func TestReport_FailsOnDriftStaleInfra(t *testing.T) {
	snap := &SnapshotFile{Reference: "gc-a", Toolchain: "go1.x"}
	if err := report([]verdict{{"a", "MATCH", ""}, {"b", "EXPECTED", "x"}}, 2, snap, "g"); err != nil {
		t.Fatalf("clean report failed: %v", err)
	}
	for _, k := range []string{"DRIFT", "STALE", "UNPINNED", "REF-DRIFT"} {
		if err := report([]verdict{{"a", k, ""}}, 1, snap, "g"); err == nil {
			t.Errorf("%s did not fail", k)
		}
	}
	if err := report([]verdict{{"a", "INFRA", ""}}, 1, snap, "g"); err == nil || !strings.Contains(err.Error(), errInfra.Error()) {
		t.Errorf("INFRA not reported as infra failure: %v", err)
	}
}

func TestIsInfra(t *testing.T) {
	if !isInfra(SnapshotEntry{BuildLog: "internal error building AST: JSON parse error at line 3"}) {
		t.Fatal("frontend corruption not classified as infra")
	}
	if isInfra(SnapshotEntry{Stderr: "panic: runtime error: index out of range [5] with length 3"}) {
		t.Fatal("program panic classified as infra")
	}
}

func TestSelectChecked_ContentAddressed(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a", "b"} {
		os.MkdirAll(filepath.Join(dir, n), 0o755)
		os.WriteFile(filepath.Join(dir, n, "main.go"), []byte("package main\nfunc main(){}\n// "+n), 0o644)
	}
	progs, err := harness.DiscoverCorpus(dir)
	if err != nil {
		t.Fatal(err)
	}
	snap := &SnapshotFile{}
	for _, p := range progs {
		h, _ := sourceHash(p)
		snap.Programs = append(snap.Programs, SnapshotEntry{Name: p.Name, SourceSHA256: h})
	}
	if live, err := selectChecked(snap, progs, nil, ""); err != nil || len(live) != 2 {
		t.Fatalf("consistent corpus rejected: %v", err)
	}
	if live, err := selectChecked(snap, progs, nil, "b"); err != nil || len(live) != 1 || live[0].Name != "b" {
		t.Fatalf("-programs slice wrong: %v %v", live, err)
	}
	if _, err := selectChecked(snap, progs, map[string]harness.ReviewedExclusion{"zz": {}}, ""); err == nil {
		t.Error("expected-divergence entry for a missing program accepted")
	}
	if _, err := selectChecked(snap, progs, nil, "zz"); err == nil {
		t.Error("unknown -programs name accepted")
	}
	snap2 := &SnapshotFile{Programs: append(append([]SnapshotEntry{}, snap.Programs...), SnapshotEntry{Name: "gone"})}
	if _, err := selectChecked(snap2, progs, nil, ""); err == nil {
		t.Error("recorded program missing from corpus accepted")
	}
	os.WriteFile(progs[0].Main, []byte("package main\nfunc main(){println()}\n"), 0o644)
	if _, err := selectChecked(snap, progs, nil, ""); err == nil || !strings.Contains(err.Error(), "source changed") {
		t.Errorf("edited source not detected: %v", err)
	}
}

func TestSnapshotRoundTripsInvalidUTF8(t *testing.T) {
	// encoding/json replaces invalid UTF-8 with U+FFFD: without the base64
	// fallback a gc snapshot of a program printing raw bytes would be
	// silently wrong (found by corpus 51_int_conversions).
	raw := "copy h\xff\xc3\xa9\xe2\x82\n"
	path := filepath.Join(t.TempDir(), "s.json")
	e := SnapshotEntry{Name: "p", Stdout: raw, Stderr: "ok"}
	e.encodeBytes()
	snap := SnapshotFile{Format: snapshotFormat, Normalizer: snapshotNormalizer, Programs: []SnapshotEntry{e}}
	if err := writeJSON(path, snap); err != nil {
		t.Fatal(err)
	}
	got, err := readSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Programs[0].Stdout != raw || got.Programs[0].Stderr != "ok" {
		t.Fatalf("output did not round-trip: %q / %q", got.Programs[0].Stdout, got.Programs[0].Stderr)
	}
}

func TestRetryBudget(t *testing.T) {
	if err := checkRetryBudget(nil, 0); err != nil {
		t.Fatalf("no retries: %v", err)
	}
	if err := checkRetryBudget([]string{"a"}, 0); err == nil {
		t.Fatal("a retried-away failure over budget 0 must fail (G6)")
	}
	if err := checkRetryBudget([]string{"a"}, 1); err != nil {
		t.Fatalf("within budget: %v", err)
	}
}

func TestSelectCheckedZeroCases(t *testing.T) {
	snap := &SnapshotFile{}
	if _, err := selectChecked(snap, nil, nil, ""); err == nil {
		t.Fatal("empty corpus must fail (G8)")
	}
	if _, err := selectChecked(snap, nil, nil, ","); err == nil {
		t.Fatal("-programs \",\" must fail (G8)")
	}
}

// G2 for srctest/typeparam: a listed file whose divergence no longer
// matches its pinned signature is DIVERGE, not EXPECTED.
func TestSrctestVerdictSignature(t *testing.T) {
	reasons := []string{"stdout differs at line 1"}
	pinned := harness.DiffSignature(strings.Join(reasons, "\n"))
	for _, c := range []struct {
		c    typeparam.Comparison
		want string
	}{
		{typeparam.Comparison{Same: false, Expected: "deferred-parity", ExpectedSig: pinned, Reasons: reasons}, "EXPECTED"},
		{typeparam.Comparison{Same: false, Expected: "deferred-parity", ExpectedSig: pinned, Reasons: []string{"exit code 2 != 0"}}, "DIVERGE"},
		{typeparam.Comparison{Same: false, Expected: "deferred-parity", Reasons: reasons}, "EXPECTED"},
		{typeparam.Comparison{Same: true, Expected: "deferred-parity"}, "STALE"},
		// A timing-dependent entry that matches gc passes; one that does not
		// is still judged by its signature, and an unlisted match is MATCH.
		{typeparam.Comparison{Same: true, Expected: "deferred-parity", Timing: true}, "TIMING"},
		{typeparam.Comparison{Same: false, Expected: "deferred-parity", Timing: true, ExpectedSig: pinned, Reasons: reasons}, "EXPECTED"},
		{typeparam.Comparison{Same: false, Expected: "deferred-parity", Timing: true, ExpectedSig: pinned, Reasons: []string{"exit code 2 != 0"}}, "DIVERGE"},
		{typeparam.Comparison{Same: true, Timing: true}, "MATCH"},
		{typeparam.Comparison{Infra: true}, "INFRA"},
	} {
		if got := srctestVerdict(c.c); got != c.want {
			t.Errorf("%+v: got %s want %s", c.c, got, c.want)
		}
	}
}

func TestMutantStatus(t *testing.T) {
	if s, ok := mutantStatus(false, true); ok || s != "STILLBORN" {
		t.Errorf("a mutant that fails to build must not count as detected (G5): %s %v", s, ok)
	}
	if _, ok := mutantStatus(true, false); ok {
		t.Error("missed mutant passed")
	}
	if s, ok := mutantStatus(true, true); !ok || s != "DETECTED" {
		t.Errorf("detected: %s %v", s, ok)
	}
}

// The corpus snapshot gate honors `timing: true` the same way: matching gc is
// TIMING (a pass) for a timing entry and STALE for any other.
func TestClassifyTimingEntry(t *testing.T) {
	exp := map[string]harness.ReviewedExclusion{
		"slow":  {File: "slow", Reason: "deferred-parity", Detail: "BUG-0001", Timing: true},
		"plain": {File: "plain", Reason: "deferred-parity", Detail: "BUG-0001"},
	}
	for _, c := range []struct{ name, diff, want string }{
		{"slow", "", "TIMING"},
		{"plain", "", "STALE"},
		{"slow", "stdout differs", "UNPINNED"},
		{"other", "", "MATCH"},
	} {
		if got := classify(c.name, c.diff, false, exp).kind; got != c.want {
			t.Errorf("classify(%s, %q): got %s want %s", c.name, c.diff, got, c.want)
		}
	}
}
