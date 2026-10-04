package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------- parseFlags ----------

func TestParseFlags_Forms(t *testing.T) {
	f := parseFlags([]string{"pos1", "--title=x", "--statement", "y", "--flag"})
	if f.str("title") != "x" {
		t.Fatalf("title = %q", f.str("title"))
	}
	if f.str("statement") != "y" {
		t.Fatalf("statement = %q", f.str("statement"))
	}
	if f.str("flag") != "true" {
		t.Fatalf("bare --flag = %q, want \"true\"", f.str("flag"))
	}
	if strings.Join(f.pos, ",") != "pos1" {
		t.Fatalf("pos = %v", f.pos)
	}
	if f.str("missing") != "" {
		t.Fatalf("missing key = %q, want \"\"", f.str("missing"))
	}
	// A bare flag followed by a non-flag token consumes it as its value.
	g := parseFlags([]string{"--flag", "pos2"})
	if g.str("flag") != "pos2" || len(g.pos) != 0 {
		t.Fatalf("value consumption: flag=%q pos=%v", g.str("flag"), g.pos)
	}
}

func TestParseFlags_LastWins(t *testing.T) {
	f := parseFlags([]string{"--k", "1", "--k=2"})
	if f.str("k") != "2" {
		t.Fatalf("k = %q, want last-wins \"2\"", f.str("k"))
	}
}

func TestParseFlags_ValueLookingLikeFlag(t *testing.T) {
	// A value starting with "--" is not consumed as the value; the key
	// becomes a boolean flag.
	f := parseFlags([]string{"--a", "--b"})
	if f.str("a") != "true" || f.str("b") != "true" {
		t.Fatalf("flags = %v", f.kv)
	}
}

// ---------- req add ----------

func addArgs(root, reqDir string, extra ...string) []string {
	return append(ctxFlags(root, reqDir, root), extra...)
}

func TestCmdAdd_Happy(t *testing.T) {
	root, reqDir := stdLayout(t, map[string]string{
		"requirements/GG-0001.yaml": reqFile("GG-0001", "draft", "", ""),
	})
	out, _ := captureOutput(t, func() {
		if err := cmdAdd(addArgs(root, reqDir,
			"--title", "New thing", "--statement", "It SHALL work.",
			"--rationale", "Because.", "--parent", "GG-0001",
			"--verification-plan", "Test it.")); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "minted GG-0002 -> ") {
		t.Fatalf("unexpected add output: %q", out)
	}
	reg, err := loadRegistry(reqDir)
	if err != nil {
		t.Fatal(err)
	}
	r := reg["GG-0002"]
	if r == nil {
		t.Fatal("GG-0002.yaml was not created")
	}
	if r.Title != "New thing" || r.Statement != "It SHALL work." ||
		r.Rationale != "Because." || r.Parent != "GG-0001" ||
		r.Status != "draft" || r.VerificationPlan != "Test it." {
		t.Fatalf("unexpected record: %+v", r)
	}
	if len(r.History) != 1 || !strings.Contains(r.History[0], "created (draft)") {
		t.Fatalf("unexpected history: %v", r.History)
	}
}

func TestCmdAdd_Monotonic(t *testing.T) {
	root, reqDir := stdLayout(t, nil)
	for i := 0; i < 3; i++ {
		captureOutput(t, func() {
			if err := cmdAdd(addArgs(root, reqDir, "--title", "T", "--statement", "S")); err != nil {
				t.Fatal(err)
			}
		})
	}
	reg, err := loadRegistry(reqDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"GG-0001", "GG-0002", "GG-0003"} {
		if reg[id] == nil {
			t.Fatalf("%s was not minted", id)
		}
	}
}

func TestCmdAdd_MissingTitleOrStatement(t *testing.T) {
	root, reqDir := stdLayout(t, nil)
	err := cmdAdd(addArgs(root, reqDir, "--title", "T"))
	requireErrContains(t, err, "requires --title and --statement")
	err = cmdAdd(addArgs(root, reqDir, "--statement", "S"))
	requireErrContains(t, err, "requires --title and --statement")
}

func TestCmdAdd_BadParent(t *testing.T) {
	root, reqDir := stdLayout(t, nil)
	err := cmdAdd(addArgs(root, reqDir, "--title", "T", "--statement", "S", "--parent", "GG-0099"))
	requireErrContains(t, err, "parent GG-0099 not in registry")
}

func TestCmdAdd_SaveError(t *testing.T) {
	root, reqDir := stdLayout(t, nil)
	// A directory at the minted path makes the write fail (works as root).
	if err := os.MkdirAll(filepath.Join(reqDir, "GG-0001.yaml"), 0755); err != nil {
		t.Fatal(err)
	}
	err := cmdAdd(addArgs(root, reqDir, "--title", "T", "--statement", "S"))
	if err == nil {
		t.Fatal("expected save error, got nil")
	}
}

func TestCmdAdd_CtxError(t *testing.T) {
	root := t.TempDir()
	err := cmdAdd(ctxFlags(root, filepath.Join(root, "missing"), root))
	requireErrContains(t, err, "requirements dir not found")
}

func TestCmdAdd_LoadError(t *testing.T) {
	root, reqDir := stdLayout(t, nil)
	writeFile(t, filepath.Join(reqDir, "GG-0001.yaml"), "id: [broken\n")
	err := cmdAdd(addArgs(root, reqDir, "--title", "T", "--statement", "S"))
	if err == nil {
		t.Fatal("expected registry load error, got nil")
	}
}

// ---------- req trace ----------

func traceFixture(t *testing.T) (root, reqDir string) {
	t.Helper()
	return stdLayout(t, map[string]string{
		"requirements/GG-0001.yaml": reqFile("GG-0001", "active", "GG-0002", "",
			"2026-10-02: locked"),
		"requirements/GG-0002.yaml": reqFile("GG-0002", "superseded", "", "GG-0001"),
		"code.go":                   "package x\n\n// GG-0002: old usage\nvar _ = 1\n",
		"code_test.go":              "package x\n\nimport \"testing\"\n\n// verifies: GG-0002\nfunc TestOld(t *testing.T) {}\n",
		"ESCAPES.md":                "# Escapes\n\nGG-0002 mentioned in the historical log.\n",
	})
}

func TestCmdTrace_Happy(t *testing.T) {
	root, reqDir := traceFixture(t)
	out, _ := captureOutput(t, func() {
		if err := cmdTrace(append(ctxFlags(root, reqDir, root), "GG-0002")); err != nil {
			t.Fatal(err)
		}
	})
	for _, want := range []string{
		"GG-0002 [superseded]",
		"title: Title GG-0002",
		"superseded_by: GG-0001",
		"statement: Statement GG-0002.",
		"rationale: Rationale GG-0002.",
		"verification_plan: Plan GG-0002.",
		"history: 2026-10-01: created (draft)",
		"code sites:",
		"code.go:3: // GG-0002: old usage",
		"tests:",
		"code_test.go:5: // verifies: GG-0002",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("trace output missing %q\n---\n%s", want, out)
		}
	}
	if strings.Contains(out, "ESCAPES.md") {
		t.Errorf("historical ESCAPES.md ref must not appear in code sites:\n%s", out)
	}
}

func TestCmdTrace_Supersedes(t *testing.T) {
	root, reqDir := traceFixture(t)
	out, _ := captureOutput(t, func() {
		if err := cmdTrace(append(ctxFlags(root, reqDir, root), "GG-0001")); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "supersedes: GG-0002") {
		t.Fatalf("trace should list predecessors:\n%s", out)
	}
	if !strings.Contains(out, "parent: GG-0002") {
		t.Fatalf("trace should show the parent link:\n%s", out)
	}
}

func TestCmdTrace_UnknownID(t *testing.T) {
	root, reqDir := traceFixture(t)
	err := cmdTrace(append(ctxFlags(root, reqDir, root), "GG-0099"))
	requireErrContains(t, err, "unknown requirement id GG-0099")
}

func TestCmdTrace_Usage(t *testing.T) {
	root, reqDir := traceFixture(t)
	err := cmdTrace(ctxFlags(root, reqDir, root))
	requireErrContains(t, err, "usage: req trace GG-XXXX")
}

func TestCmdTrace_CtxError(t *testing.T) {
	root := t.TempDir()
	err := cmdTrace(append(ctxFlags(root, filepath.Join(root, "missing"), root), "GG-0001"))
	requireErrContains(t, err, "requirements dir not found")
}

func TestCmdTrace_LoadError(t *testing.T) {
	root, reqDir := stdLayout(t, nil)
	writeFile(t, filepath.Join(reqDir, "GG-0001.yaml"), "id: [broken\n")
	err := cmdTrace(append(ctxFlags(root, reqDir, root), "GG-0001"))
	if err == nil {
		t.Fatal("expected registry load error, got nil")
	}
}

func TestCmdTrace_NoSites(t *testing.T) {
	root, reqDir := stdLayout(t, map[string]string{
		"requirements/GG-0001.yaml": reqFile("GG-0001", "draft", "", ""),
	})
	out, _ := captureOutput(t, func() {
		if err := cmdTrace(append(ctxFlags(root, reqDir, root), "GG-0001")); err != nil {
			t.Fatal(err)
		}
	})
	// The record's own title/statement mention GG-0001, but those live in the
	// registry (historical) — wait: trace only excludes historical refs from
	// "code sites". Registry self-mentions are historical, so code sites is empty.
	if strings.Count(out, "(none)") != 2 {
		t.Fatalf("expected (none) for code sites and tests, got:\n%s", out)
	}
}

// ---------- req tree ----------

func TestCmdTree_Hierarchy(t *testing.T) {
	root, reqDir := stdLayout(t, map[string]string{
		"requirements/GG-0001.yaml": reqFile("GG-0001", "active", "", ""),
		"requirements/GG-0002.yaml": reqFile("GG-0002", "draft", "GG-0001", ""),
		"requirements/GG-0003.yaml": reqFile("GG-0003", "draft", "GG-0002", ""),
		"requirements/GG-0004.yaml": reqFile("GG-0004", "draft", "", ""),
	})
	out, _ := captureOutput(t, func() {
		if err := cmdTree(ctxFlags(root, reqDir, root)); err != nil {
			t.Fatal(err)
		}
	})
	want := "GG-0001 [active] Title GG-0001\n" +
		"  GG-0002 [draft] Title GG-0002\n" +
		"    GG-0003 [draft] Title GG-0003\n" +
		"GG-0004 [draft] Title GG-0004\n"
	if out != want {
		t.Fatalf("tree output:\n%s\nwant:\n%s", out, want)
	}
}

func TestCmdTree_SupersededArrow(t *testing.T) {
	root, reqDir := stdLayout(t, map[string]string{
		"requirements/GG-0001.yaml": reqFile("GG-0001", "superseded", "", "GG-0002"),
		"requirements/GG-0002.yaml": reqFile("GG-0002", "draft", "", ""),
	})
	out, _ := captureOutput(t, func() {
		if err := cmdTree(ctxFlags(root, reqDir, root)); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "GG-0001 [superseded -> GG-0002]") {
		t.Fatalf("missing supersession arrow:\n%s", out)
	}
}

func TestCmdTree_DanglingParent(t *testing.T) {
	root, reqDir := stdLayout(t, map[string]string{
		"requirements/GG-0001.yaml": reqFile("GG-0001", "draft", "GG-0099", ""),
	})
	out, _ := captureOutput(t, func() {
		if err := cmdTree(ctxFlags(root, reqDir, root)); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "warning: GG-0001 has dangling parent GG-0099") {
		t.Fatalf("missing dangling-parent warning:\n%s", out)
	}
	if !strings.Contains(out, "GG-0001 [draft]") {
		t.Fatalf("dangling node must still render as a root:\n%s", out)
	}
}

func TestCmdTree_CtxError(t *testing.T) {
	root := t.TempDir()
	err := cmdTree(ctxFlags(root, filepath.Join(root, "missing"), root))
	requireErrContains(t, err, "requirements dir not found")
}

func TestCmdTree_LoadError(t *testing.T) {
	root, reqDir := stdLayout(t, nil)
	writeFile(t, filepath.Join(reqDir, "GG-0001.yaml"), "id: [broken\n")
	err := cmdTree(ctxFlags(root, reqDir, root))
	if err == nil {
		t.Fatal("expected registry load error, got nil")
	}
}

// ---------- run() dispatch ----------

func TestRun_NoArgs(t *testing.T) {
	out, _ := captureOutput(t, func() {
		if got := run([]string{}); got != 2 {
			t.Fatalf("run([]) = %d, want 2", got)
		}
	})
	if !strings.Contains(out, "Usage:") {
		t.Fatalf("no-args must print usage, got %q", out)
	}
}

func TestRun_UnknownCommand(t *testing.T) {
	var code int
	_, serr := captureOutput(t, func() { code = run([]string{"bogus"}) })
	if code != 2 {
		t.Fatalf("run(bogus) = %d, want 2", code)
	}
	if !strings.Contains(serr, `unknown command "bogus"`) {
		t.Fatalf("stderr = %q", serr)
	}
}

func TestRun_Help(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"-h"}, {"--help"}} {
		var code int
		out, _ := captureOutput(t, func() { code = run(args) })
		if code != 0 {
			t.Fatalf("run(%v) = %d, want 0", args, code)
		}
		if !strings.Contains(out, "Usage:") {
			t.Fatalf("run(%v) must print usage, got %q", args, out)
		}
	}
}

func TestRun_CommandError(t *testing.T) {
	var code int
	_, serr := captureOutput(t, func() { code = run([]string{"trace"}) })
	if code != 1 {
		t.Fatalf("run(trace) = %d, want 1", code)
	}
	if !strings.Contains(serr, "req trace:") {
		t.Fatalf("stderr = %q", serr)
	}
}

func TestRun_LintOK(t *testing.T) {
	root, reqDir := lintCleanFixture(t)
	var code int
	out, _ := captureOutput(t, func() { code = run(append([]string{"lint"}, ctxFlags(root, reqDir, root)...)) })
	if code != 0 {
		t.Fatalf("run(lint) = %d, want 0; output %q", code, out)
	}
}

func TestRun_AddError(t *testing.T) {
	root, reqDir := stdLayout(t, nil)
	var code int
	_, serr := captureOutput(t, func() { code = run(append([]string{"add"}, ctxFlags(root, reqDir, root)...)) })
	if code != 1 {
		t.Fatalf("run(add) without title = %d, want 1", code)
	}
	if !strings.Contains(serr, "req add:") {
		t.Fatalf("stderr = %q", serr)
	}
}

func TestRun_TreeSupersedeRetireDispatch(t *testing.T) {
	// tree
	root, reqDir := stdLayout(t, map[string]string{
		"requirements/GG-0001.yaml": reqFile("GG-0001", "active", "", ""),
	})
	var code int
	captureOutput(t, func() { code = run(append([]string{"tree"}, ctxFlags(root, reqDir, root)...)) })
	if code != 0 {
		t.Fatalf("run(tree) = %d, want 0", code)
	}
	// supersede (zero hits: no prompt, no terminal needed)
	captureOutput(t, func() {
		code = run(append([]string{"supersede"}, append(ctxFlags(root, reqDir, root),
			"GG-0001", "--title", "T", "--statement", "S")...))
	})
	if code != 0 {
		t.Fatalf("run(supersede) = %d, want 0", code)
	}
	reg, err := loadRegistry(reqDir)
	if err != nil {
		t.Fatal(err)
	}
	if reg["GG-0001"].Status != "superseded" || reg["GG-0002"] == nil {
		t.Fatal("supersede dispatch did not update the registry")
	}
	// retire on an independent active requirement
	root2, reqDir2 := stdLayout(t, map[string]string{
		"requirements/GG-0001.yaml": reqFile("GG-0001", "active", "", ""),
	})
	captureOutput(t, func() {
		code = run(append([]string{"retire"}, append(ctxFlags(root2, reqDir2, root2), "GG-0001")...))
	})
	if code != 0 {
		t.Fatalf("run(retire) = %d, want 0", code)
	}
	reg2, err := loadRegistry(reqDir2)
	if err != nil {
		t.Fatal(err)
	}
	if reg2["GG-0001"].Status != "retired" {
		t.Fatalf("retire dispatch left status %q", reg2["GG-0001"].Status)
	}
}

// ---------- isTerminal (real implementation) ----------

func withStdinFile(t *testing.T, f *os.File) {
	t.Helper()
	old := os.Stdin
	os.Stdin = f
	t.Cleanup(func() { os.Stdin = old })
}

func TestIsTerminal_DevNull(t *testing.T) {
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	withStdinFile(t, f)
	if !isTerminal() {
		t.Fatal("isTerminal() on /dev/null must be true (character device)")
	}
}

func TestIsTerminal_Pipe(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	withStdinFile(t, r)
	if isTerminal() {
		t.Fatal("isTerminal() on a pipe must be false")
	}
}

func TestIsTerminal_StatError(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	_ = r.Close() // closed file: Stat fails
	withStdinFile(t, r)
	if isTerminal() {
		t.Fatal("isTerminal() must be false when stdin stat fails")
	}
}
