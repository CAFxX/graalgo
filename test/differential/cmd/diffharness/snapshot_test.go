package main

import (
	"strings"
	"testing"

	"diffharness/internal/harness"
)

func mkEntry(name string) SnapshotEntry {
	return SnapshotEntry{
		Name:     name,
		Args:     []string{},
		Stdin:    "",
		BuildOK:  true,
		BuildLog: "",
		ExitCode: 0,
		TimedOut: false,
		Stdout:   "hello\n",
		Stderr:   "",
	}
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
	if d == "" {
		t.Fatal("stdout difference not detected")
	}
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

func TestDiffEntries_BuildLogOnlyCompared(t *testing.T) {
	// A build-failure baseline must be sensitive to the failure text.
	a, b := mkEntry("p"), mkEntry("p")
	a.BuildOK, b.BuildOK = false, false
	a.ExitCode, b.ExitCode = -1, -1
	a.BuildLog = "build [{GRAALGO_RUN} --check {CORPUS}/x/main.go] failed: exit status 1\nold error"
	b.BuildLog = "build [{GRAALGO_RUN} --check {CORPUS}/x/main.go] failed: exit status 1\nnew error"
	if d := diffEntries(a, b); !strings.Contains(d, "buildLog") {
		t.Fatalf("build log drift not reported: %q", d)
	}
}

func TestScrubEnvPaths(t *testing.T) {
	in := "build [/home/u/checkout/phases/phase1/graalgo-run --check /home/u/checkout/tools/diffharness/corpus/13_x/main.go] failed"
	got := scrubEnvPaths(in, "/home/u/checkout/tools/diffharness/corpus")
	if strings.Contains(got, "/home/u/checkout") {
		t.Fatalf("machine paths not scrubbed: %q", got)
	}
	if !strings.Contains(got, "{GRAALGO_RUN}") || !strings.Contains(got, "{CORPUS}/13_x/main.go") {
		t.Fatalf("scrub tokens wrong: %q", got)
	}
}

func TestScrubEnvPaths_LeavesProgramOutputAlone(t *testing.T) {
	in := "hello, world\n"
	if got := scrubEnvPaths(in, "/home/u/checkout/tools/diffharness/corpus"); got != in {
		t.Fatalf("program output mangled: %q -> %q", in, got)
	}
}

func TestDiffExclusions_OrderInsensitive(t *testing.T) {
	a := []harness.Exclusion{{Program: "x", Reason: "flaky"}, {Program: "y", Reason: "other: z"}}
	b := []harness.Exclusion{{Program: "y", Reason: "other: z"}, {Program: "x", Reason: "flaky"}}
	if d := diffExclusions(a, b); d != "" {
		t.Fatalf("order-only difference reported as drift: %s", d)
	}
}

func TestDiffExclusions_DetectsDrift(t *testing.T) {
	a := []harness.Exclusion{{Program: "x", Reason: "flaky"}}
	if d := diffExclusions(a, nil); d == "" {
		t.Fatal("removed exclusion not detected")
	}
	if d := diffExclusions(nil, a); d == "" {
		t.Fatal("added exclusion not detected")
	}
	c := []harness.Exclusion{{Program: "x", Reason: "backend-specific"}}
	if d := diffExclusions(a, c); d == "" {
		t.Fatal("changed exclusion reason not detected")
	}
}

func TestDiffExclusions_DetectsOwnerAndRetriageDrift(t *testing.T) {
	// Ownership metadata is part of the baseline: silently dropping an
	// owner or changing a re-triage trigger must fail the check, and an
	// identical full entry must pass (positive + negative coverage).
	base := harness.Exclusion{Program: "x", Reason: "flaky", Owner: "backend/scheduler", Retriage: "re-probe on scheduler change"}
	if d := diffExclusions([]harness.Exclusion{base}, []harness.Exclusion{base}); d != "" {
		t.Fatalf("identical full exclusion reported as drift: %s", d)
	}
	noOwner := base
	noOwner.Owner = ""
	if d := diffExclusions([]harness.Exclusion{base}, []harness.Exclusion{noOwner}); d == "" {
		t.Fatal("dropped exclusion owner not detected")
	}
	newTrigger := base
	newTrigger.Retriage = "re-probe at next re-record"
	if d := diffExclusions([]harness.Exclusion{base}, []harness.Exclusion{newTrigger}); d == "" {
		t.Fatal("changed retriage trigger not detected")
	}
}
