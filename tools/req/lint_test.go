package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// lintCleanFixture builds a fully lint-clean tree:
//   - GG-0001 active, parent GG-0002, referenced by code.go, verified by code_test.go
//   - GG-0002 superseded by GG-0001 (referenced only from historical records)
//   - ESCAPES.md mentions GG-0002 (historical record)
func lintCleanFixture(t *testing.T) (root, reqDir string) {
	t.Helper()
	root, reqDir = stdLayout(t, map[string]string{
		"requirements/GG-0001.yaml": reqFile("GG-0001", "active", "GG-0002", ""),
		"requirements/GG-0002.yaml": reqFile("GG-0002", "superseded", "", "GG-0001"),
		"code.go":                   "package x\n\n// GG-0001: implement the thing\nvar _ = 1\n",
		"code_test.go":              "package x\n\nimport \"testing\"\n\n// verifies: GG-0001\nfunc TestThing(t *testing.T) {}\n",
		"ESCAPES.md":                "# Escapes\n\nGG-0002 was superseded long ago; this log is history.\n",
	})
	return root, reqDir
}

func runLint(t *testing.T, root, reqDir string) (stdout string, err error) {
	t.Helper()
	var out string
	out, _ = captureOutput(t, func() {
		err = cmdLint(ctxFlags(root, reqDir, root))
	})
	return out, err
}

// requireLintProblem asserts the lint run failed and reported the given
// rule marker in its LINT: output lines (the error itself only carries the
// problem count; details go to stdout).
func requireLintProblem(t *testing.T, out string, err error, rule string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected lint failure for %s, got nil\n%s", rule, out)
	}
	if !strings.Contains(out, rule) {
		t.Fatalf("lint output missing %q\n%s", rule, out)
	}
}

func TestLint_Clean(t *testing.T) {
	root, reqDir := lintCleanFixture(t)
	out, err := runLint(t, root, reqDir)
	if err != nil {
		t.Fatalf("clean tree should lint: %v\n%s", err, out)
	}
	// Pin the exact reference count: 5 (GG-0001.yaml) + 5 (GG-0002.yaml) +
	// 1 (code.go) + 1 (code_test.go verifies) + 1 (ESCAPES.md) = 13.
	if !strings.Contains(out, "lint ok: 2 requirement(s), 13 reference(s)") {
		t.Fatalf("unexpected lint output: %q", out)
	}
}

func TestLint_RuleA_UnknownID(t *testing.T) {
	root, reqDir := lintCleanFixture(t)
	writeFile(t, filepath.Join(root, "code2.go"), "package x\n// GG-0099: unknown\n")
	out, err := runLint(t, root, reqDir)
	requireLintProblem(t, out, err, "rule (a)")
	requireErrContains(t, err, "1 lint problem(s)")
	if !strings.Contains(out, "LINT: code2.go:2: rule (a): unknown requirement id GG-0099") {
		t.Fatalf("missing rule (a) report: %q", out)
	}
}

func TestLint_RuleA_MarkdownExempt(t *testing.T) {
	root, reqDir := lintCleanFixture(t)
	writeFile(t, filepath.Join(root, "doc.md"), "# Example\n\n```\n// GG-0099: illustrative\n```\n")
	out, err := runLint(t, root, reqDir)
	if err != nil {
		t.Fatalf("fenced unknown IDs in markdown are exempt: %v\n%s", err, out)
	}
}

func TestLint_RuleB_RetiredRef(t *testing.T) {
	root, reqDir := lintCleanFixture(t)
	writeFile(t, filepath.Join(reqDir, "GG-0003.yaml"), reqFile("GG-0003", "retired", "", ""))
	writeFile(t, filepath.Join(root, "code2.go"), "package x\n// GG-0003: stale\n")
	out, err := runLint(t, root, reqDir)
	requireLintProblem(t, out, err, "rule (b)")
	if !strings.Contains(out, "reference to retired GG-0003 outside historical record") {
		t.Fatalf("missing rule (b) report: %q", out)
	}
}

func TestLint_RuleB_SupersededRef(t *testing.T) {
	root, reqDir := lintCleanFixture(t)
	writeFile(t, filepath.Join(root, "code2.go"), "package x\n// GG-0002: stale\n")
	out, err := runLint(t, root, reqDir)
	requireLintProblem(t, out, err, "rule (b)")
	if !strings.Contains(out, "reference to superseded GG-0002 outside historical record") {
		t.Fatalf("missing rule (b) report: %q", out)
	}
}

func TestLint_RuleB_VerifiesToRetiredAlsoFlagged(t *testing.T) {
	root, reqDir := lintCleanFixture(t)
	writeFile(t, filepath.Join(reqDir, "GG-0003.yaml"), reqFile("GG-0003", "retired", "", ""))
	writeFile(t, filepath.Join(root, "code_test.go"),
		"package x\n\nimport \"testing\"\n\n// verifies: GG-0001\nfunc TestThing(t *testing.T) {}\n\n// verifies: GG-0003\nfunc TestStale(t *testing.T) {}\n")
	out, err := runLint(t, root, reqDir)
	requireLintProblem(t, out, err, "rule (b)")
	if !strings.Contains(out, "reference to retired GG-0003 outside historical record") {
		t.Fatalf("verifies: to a retired ID must be flagged: %q", out)
	}
}

func TestLint_RuleB_HistoricalRecordsExempt(t *testing.T) {
	root, reqDir := lintCleanFixture(t)
	// A history entry inside a registry file may keep naming the old ID.
	yaml := readFile(t, filepath.Join(reqDir, "GG-0001.yaml"))
	yaml += "  - \"2026-10-02: still thinking about GG-0002\"\n"
	writeFile(t, filepath.Join(reqDir, "GG-0001.yaml"), yaml)
	out, err := runLint(t, root, reqDir)
	if err != nil {
		t.Fatalf("historical records must be exempt from rule (b): %v\n%s", err, out)
	}
	// The clean fixture itself exercises the exemption: GG-0001.yaml's
	// parent link and ESCAPES.md both name superseded GG-0002.
}

func TestLint_RuleC_ActiveNeedsVerifies(t *testing.T) {
	root, reqDir := stdLayout(t, map[string]string{
		"requirements/GG-0004.yaml": reqFile("GG-0004", "active", "", ""),
	})
	out, err := runLint(t, root, reqDir)
	requireLintProblem(t, out, err, "rule (c)")
	requireErrContains(t, err, "1 lint problem(s)")
	if !strings.Contains(out, "LINT: rule (c): active requirement GG-0004 has no `verifies:` annotation") {
		t.Fatalf("missing rule (c) report: %q", out)
	}
}

func TestLint_RuleC_NonActiveExempt(t *testing.T) {
	root, reqDir := lintCleanFixture(t)
	writeFile(t, filepath.Join(reqDir, "GG-0004.yaml"), reqFile("GG-0004", "draft", "", ""))
	writeFile(t, filepath.Join(reqDir, "GG-0005.yaml"), reqFile("GG-0005", "retired", "", ""))
	writeFile(t, filepath.Join(reqDir, "GG-0006.yaml"), reqFile("GG-0006", "superseded", "", "GG-0001"))
	out, err := runLint(t, root, reqDir)
	if err != nil {
		t.Fatalf("draft/retired/superseded need no verifies:: %v\n%s", err, out)
	}
}

func TestLint_RuleD_SloppyID(t *testing.T) {
	root, reqDir := lintCleanFixture(t)
	writeFile(t, filepath.Join(root, "code2.go"), "package x\n// GG-42 sloppy\n")
	out, err := runLint(t, root, reqDir)
	requireLintProblem(t, out, err, "rule (d)")
	if !strings.Contains(out, "LINT: rule (d): code2.go:2: non-canonical requirement id GG-42") {
		t.Fatalf("missing rule (d) report: %q", out)
	}
}

func TestLint_ParentDangles(t *testing.T) {
	root, reqDir := lintCleanFixture(t)
	writeFile(t, filepath.Join(reqDir, "GG-0004.yaml"), reqFile("GG-0004", "draft", "GG-0099", ""))
	out, err := runLint(t, root, reqDir)
	requireLintProblem(t, out, err, "parent GG-0099 not in registry")
}

func TestLint_SupersededEmptySuccessor(t *testing.T) {
	root, reqDir := lintCleanFixture(t)
	writeFile(t, filepath.Join(reqDir, "GG-0004.yaml"), reqFile("GG-0004", "superseded", "", ""))
	out, err := runLint(t, root, reqDir)
	requireLintProblem(t, out, err, "superseded with empty superseded_by")
}

func TestLint_ChainDangles(t *testing.T) {
	root, reqDir := lintCleanFixture(t)
	writeFile(t, filepath.Join(reqDir, "GG-0004.yaml"), reqFile("GG-0004", "superseded", "", "GG-0099"))
	out, err := runLint(t, root, reqDir)
	requireLintProblem(t, out, err, "superseded_by chain dangles at GG-0099")
}

func TestLint_ChainCycle(t *testing.T) {
	root, reqDir := lintCleanFixture(t)
	writeFile(t, filepath.Join(reqDir, "GG-0004.yaml"), reqFile("GG-0004", "superseded", "", "GG-0005"))
	writeFile(t, filepath.Join(reqDir, "GG-0005.yaml"), reqFile("GG-0005", "superseded", "", "GG-0004"))
	out, err := runLint(t, root, reqDir)
	requireLintProblem(t, out, err, "superseded_by chain cycles at")
}

func TestLint_ChainReachesDeadSuperseded(t *testing.T) {
	root, reqDir := lintCleanFixture(t)
	writeFile(t, filepath.Join(reqDir, "GG-0004.yaml"), reqFile("GG-0004", "superseded", "", "GG-0005"))
	writeFile(t, filepath.Join(reqDir, "GG-0005.yaml"), reqFile("GG-0005", "superseded", "", ""))
	out, err := runLint(t, root, reqDir)
	requireLintProblem(t, out, err, "superseded with no successor")
}

func TestLint_ChainIntermediateSupersededOK(t *testing.T) {
	root, reqDir := lintCleanFixture(t)
	writeFile(t, filepath.Join(reqDir, "GG-0004.yaml"), reqFile("GG-0004", "superseded", "", "GG-0005"))
	writeFile(t, filepath.Join(reqDir, "GG-0005.yaml"), reqFile("GG-0005", "superseded", "", "GG-0006"))
	writeFile(t, filepath.Join(reqDir, "GG-0006.yaml"), reqFile("GG-0006", "active", "", ""))
	writeFile(t, filepath.Join(root, "v_test.go"),
		"package x\nimport \"testing\"\n// verifies: GG-0006\nfunc TestV(t *testing.T) {}\n")
	out, err := runLint(t, root, reqDir)
	if err != nil {
		t.Fatalf("A -> B -> C chains with live terminus must pass: %v\n%s", err, out)
	}
}

func TestLint_ProblemsSorted(t *testing.T) {
	root, reqDir := lintCleanFixture(t)
	writeFile(t, filepath.Join(root, "code2.go"), "package x\n// GG-0099 unknown\n// GG-7 sloppy\n")
	out, err := runLint(t, root, reqDir)
	requireErrContains(t, err, "2 lint problem(s)")
	ia := strings.Index(out, "rule (a)")
	id := strings.Index(out, "rule (d)")
	if ia < 0 || id < 0 || ia > id {
		t.Fatalf("problems must be printed sorted, got:\n%s", out)
	}
}

func TestLint_CtxError(t *testing.T) {
	root := t.TempDir()
	err := cmdLint(ctxFlags(root, filepath.Join(root, "missing"), root))
	requireErrContains(t, err, "requirements dir not found")
}

func TestLint_LoadError(t *testing.T) {
	root, reqDir := stdLayout(t, nil)
	writeFile(t, filepath.Join(reqDir, "GG-0001.yaml"), "id: [broken\n")
	err := cmdLint(ctxFlags(root, reqDir, root))
	if err == nil {
		t.Fatal("expected load error, got nil")
	}
}

func TestHistoricalRef(t *testing.T) {
	root := t.TempDir()
	reqDir := filepath.Join(root, "requirements")
	c := &workCtx{root: root, reqDir: reqDir, scanDir: root}
	cases := []struct {
		rel  string
		want bool
	}{
		{"ESCAPES.md", true},
		{"requirements/GG-0001.yaml", true},
		{"requirements/sub/nested.yaml", true},
		{"code.go", false},
		{"requirements2/GG-0001.yaml", false}, // prefix trap, not inside reqDir
		{"docs/requirements/GG-0001.yaml", false},
		{"escapes.md", false}, // case-sensitive
	}
	for _, tc := range cases {
		if got := c.historicalRef(tc.rel); got != tc.want {
			t.Errorf("historicalRef(%q) = %v, want %v", tc.rel, got, tc.want)
		}
	}
}

func TestLint_RelativeFlagsKeepHistoricalExemption(t *testing.T) {
	root, _ := lintCleanFixture(t)
	// Mixed absolute --root with relative --req-dir (cwd = fixture
	// parent): the rule-(b) historical exemption (inside the registry dir
	// and in ESCAPES.md) must still apply. Regression: a relative
	// --req-dir made filepath.Rel fail on the mixed pair, silently
	// disabling the exemption and flagging history as violations.
	withCwd(t, filepath.Dir(root))
	base := filepath.Base(root)
	var out string
	var err error
	out, _ = captureOutput(t, func() {
		err = cmdLint(ctxFlags(root, filepath.Join(base, "requirements"), root))
	})
	if err != nil {
		t.Fatalf("historical records must stay exempt with relative --req-dir: %v\n%s", err, out)
	}
}
