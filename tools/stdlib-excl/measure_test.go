package main

// measure_test.go — red-green evidence for the coupled-package measurement.
//
// Fixture package dirs cover each coupled reason (asm, cgo, linkname,
// build-tags) plus the pure and not-linux-amd64 classes. Each test asserts
// the exact class + reason set, so removing the corresponding check in
// classifyDir turns the test red (verified by mutation runs documented in
// the test log below). The drift tests pin the 15% P3-D1 re-test trigger
// on both sides of the boundary.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeGoFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// fixtureTree builds a fake stdlib src tree covering every classification.
func fixtureTree(t *testing.T) string {
	t.Helper()
	src := t.TempDir()

	// pure: plain Go, no constraints.
	writeGoFile(t, filepath.Join(src, "pure"), "p.go", "package pure\n\nfunc F() int { return 1 }\n")

	// asm: .s file selected for the build.
	writeGoFile(t, filepath.Join(src, "asm"), "a.go", "package asm\n\nfunc F() int\n")
	writeGoFile(t, filepath.Join(src, "asm"), "a_amd64.s",
		"//go:build amd64\n\nTEXT ·F(SB),$0-8\n\tMOVQ $1, AX\n\tMOVQ AX, ret+0(FP)\n\tRET\n")

	// cgo: import "C".
	writeGoFile(t, filepath.Join(src, "cgo"), "c.go",
		"package cgo\n\nimport \"C\"\n\nfunc F() int { return 1 }\n")

	// linkname: //go:linkname directive.
	writeGoFile(t, filepath.Join(src, "linkname"), "l.go",
		"package linkname\n\nimport _ \"unsafe\"\n\n//go:linkname localname runtime.somefunc\nfunc localname()\n")

	// buildtags: linux file + windows-only file.
	writeGoFile(t, filepath.Join(src, "buildtags"), "b.go", "package buildtags\n\nfunc F() int { return 1 }\n")
	writeGoFile(t, filepath.Join(src, "buildtags"), "b_windows.go",
		"//go:build windows\n\npackage buildtags\n\nfunc G() int { return 2 }\n")

	// winonly: nothing selectable on linux/amd64.
	writeGoFile(t, filepath.Join(src, "winonly"), "w_windows.go",
		"//go:build windows\n\npackage winonly\n\nfunc F() int { return 1 }\n")

	// testlink: linkname only in the _test.go file (still coupled: the
	// test binary binds the symbol; see measure.go scope notes).
	writeGoFile(t, filepath.Join(src, "testlink"), "t.go", "package testlink\n\nfunc F() int { return 1 }\n")
	writeGoFile(t, filepath.Join(src, "testlink"), "t_test.go",
		"package testlink\n\nimport _ \"unsafe\"\n\n//go:linkname tlocal runtime.somefunc\nfunc tlocal()\n")

	// multireason: asm + build-tags together.
	writeGoFile(t, filepath.Join(src, "multireason"), "m.go", "package multireason\n\nfunc F() int\n")
	writeGoFile(t, filepath.Join(src, "multireason"), "m_amd64.s",
		"//go:build amd64\n\nTEXT ·F(SB),$0-8\n\tMOVQ $1, AX\n\tMOVQ AX, ret+0(FP)\n\tRET\n")
	writeGoFile(t, filepath.Join(src, "multireason"), "m_windows.go",
		"//go:build windows\n\npackage multireason\n\nfunc G() int { return 2 }\n")

	return src
}

func runMeasureJSON(t *testing.T, src string) MeasureReport {
	t.Helper()
	out := filepath.Join(t.TempDir(), "report.json")
	var stdout, stderr strings.Builder
	err := runMeasure([]string{"--src", src, "--out", out, "--go-version", "go9.99-test"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("measure failed: %v\nstderr: %s", err, stderr.String())
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var rep MeasureReport
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatal(err)
	}
	return rep
}

func dirByPath(t *testing.T, rep MeasureReport, path string) DirResult {
	t.Helper()
	for _, d := range rep.Dirs {
		if d.Path == path {
			return d
		}
	}
	t.Fatalf("dir %q not in report", path)
	return DirResult{}
}

func assertReasons(t *testing.T, d DirResult, class DirClass, wantReasons ...string) {
	t.Helper()
	if d.Class != class {
		t.Fatalf("%s: class = %q, want %q (reasons %v)", d.Path, d.Class, class, d.Reasons)
	}
	got := map[string]bool{}
	for _, r := range d.Reasons {
		got[r] = true
	}
	if len(got) != len(wantReasons) {
		t.Fatalf("%s: reasons = %v, want %v", d.Path, d.Reasons, wantReasons)
	}
	for _, w := range wantReasons {
		if !got[w] {
			t.Fatalf("%s: reasons = %v, want %v", d.Path, d.Reasons, wantReasons)
		}
	}
}

func TestMeasureClassifiesPure(t *testing.T) {
	rep := runMeasureJSON(t, fixtureTree(t))
	assertReasons(t, dirByPath(t, rep, "pure"), ClassPure)
}

func TestMeasureClassifiesAsm(t *testing.T) {
	rep := runMeasureJSON(t, fixtureTree(t))
	assertReasons(t, dirByPath(t, rep, "asm"), ClassCoupled, ReasonASM)
}

func TestMeasureClassifiesCgo(t *testing.T) {
	rep := runMeasureJSON(t, fixtureTree(t))
	assertReasons(t, dirByPath(t, rep, "cgo"), ClassCoupled, ReasonCGO)
}

func TestMeasureClassifiesLinkname(t *testing.T) {
	rep := runMeasureJSON(t, fixtureTree(t))
	assertReasons(t, dirByPath(t, rep, "linkname"), ClassCoupled, ReasonLinkname)
}

func TestMeasureClassifiesBuildTags(t *testing.T) {
	rep := runMeasureJSON(t, fixtureTree(t))
	assertReasons(t, dirByPath(t, rep, "buildtags"), ClassCoupled, ReasonBuildTags)
}

func TestMeasureClassifiesNotLinuxAmd64(t *testing.T) {
	rep := runMeasureJSON(t, fixtureTree(t))
	d := dirByPath(t, rep, "winonly")
	if d.Class != ClassNotLinuxAmd64 {
		t.Fatalf("winonly: class = %q, want not-linux-amd64", d.Class)
	}
}

func TestMeasureLinknameInTestFileStillCoupled(t *testing.T) {
	rep := runMeasureJSON(t, fixtureTree(t))
	d := dirByPath(t, rep, "testlink")
	assertReasons(t, d, ClassCoupled, ReasonLinkname)
	if len(d.NonTestReasons) != 0 {
		t.Fatalf("testlink: non-test reasons = %v, want empty", d.NonTestReasons)
	}
}

func TestMeasureMultipleReasons(t *testing.T) {
	rep := runMeasureJSON(t, fixtureTree(t))
	assertReasons(t, dirByPath(t, rep, "multireason"), ClassCoupled, ReasonASM, ReasonBuildTags)
}

func TestMeasureCounts(t *testing.T) {
	rep := runMeasureJSON(t, fixtureTree(t))
	if rep.Population != 8 {
		t.Fatalf("population = %d, want 8", rep.Population)
	}
	if rep.Counts["pure"] != 1 || rep.Counts["coupled"] != 6 || rep.Counts["not-linux-amd64"] != 1 {
		t.Fatalf("counts = %v, want pure=1 coupled=6 not-linux-amd64=1", rep.Counts)
	}
	if rep.GoVersion != "go9.99-test" {
		t.Fatalf("go_version = %q", rep.GoVersion)
	}
	if rep.Version != 1 || rep.Target != "linux/amd64" {
		t.Fatalf("version/target = %d/%q", rep.Version, rep.Target)
	}
}

// Drift tests: synthetic counts on both sides of the 15% P3-D1 trigger.
// SPEC baseline is 78 coupled packages: 78*1.15 = 89.7, so 89 must NOT
// trigger and 90 must; symmetric below: 66 triggers, 67 does not.
func TestDriftBoundary(t *testing.T) {
	cases := []struct {
		spec, measured int
		wantTrigger    bool
	}{
		{78, 78, false}, // exact: no drift
		{78, 89, false}, // +14.1%: under the trigger
		{78, 90, true},  // +15.4%: over the trigger
		{78, 67, false}, // -14.1%: under the trigger
		{78, 66, true},  // -15.4%: over the trigger
		{78, 100, true}, // large drift
		{78, 0, true},   // degenerate: total loss
	}
	for _, c := range cases {
		if got := DriftReopensBoundary(c.spec, c.measured); got != c.wantTrigger {
			t.Fatalf("DriftReopensBoundary(%d,%d) = %v, want %v (drift %.2f%%)",
				c.spec, c.measured, got, c.wantTrigger, DriftPct(c.spec, c.measured))
		}
	}
}

func TestDriftPctValues(t *testing.T) {
	if p := DriftPct(78, 90); p < 15.38 || p > 15.39 {
		t.Fatalf("DriftPct(78,90) = %v, want ~15.385", p)
	}
	if p := DriftPct(78, 78); p != 0 {
		t.Fatalf("DriftPct(78,78) = %v, want 0", p)
	}
}
