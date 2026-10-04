package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// supFixture: GG-0001 active, GG-0002 draft, code.go + code_test.go hit GG-0001.
func supFixture(t *testing.T) (root, reqDir string) {
	t.Helper()
	return stdLayout(t, map[string]string{
		"requirements/GG-0001.yaml": reqFile("GG-0001", "active", "", ""),
		"requirements/GG-0002.yaml": reqFile("GG-0002", "draft", "", ""),
		"code.go":                   "package x\n\n// GG-0001: use it\nvar _ = 1\n",
		"code_test.go":              "package x\n\nimport \"testing\"\n\n// verifies: GG-0001\nfunc TestUse(t *testing.T) {}\n",
	})
}

func supArgs(root, reqDir string, extra ...string) []string {
	return append(ctxFlags(root, reqDir, root), extra...)
}

func mkHits() []Ref {
	return []Ref{
		{ID: "GG-0001", File: "code.go", Line: 3, Text: "// GG-0001: use it", Kind: KindRef},
		{ID: "GG-0001", File: "code_test.go", Line: 5, Text: "// verifies: GG-0001", Kind: KindVerifies},
	}
}

// ---------- promptDispositions ----------

func TestPromptDispositions_NonInteractiveRefusal(t *testing.T) {
	stubTerminal(t, false)
	_, err := promptDispositions(mkHits(), "GG-0001", "GG-0003", false)
	requireErrContains(t, err, "refusing non-interactive supersede/retire with 2 un-dispositioned hit(s)")
}

func TestPromptDispositions_Update(t *testing.T) {
	stubTerminal(t, true)
	withStdin(t, "u\n")
	var ds []disposition
	out, _ := captureOutput(t, func() {
		var err error
		ds, err = promptDispositions(mkHits()[:1], "GG-0001", "GG-0003", false)
		if err != nil {
			t.Fatal(err)
		}
	})
	if len(ds) != 1 || ds[0].action != 'u' || ds[0].ref.Line != 3 {
		t.Fatalf("unexpected dispositions: %+v", ds)
	}
	if !strings.Contains(out, "[u]pdate to GG-0003") {
		t.Fatalf("missing update prompt: %q", out)
	}
}

func TestPromptDispositions_VerifiesNote(t *testing.T) {
	stubTerminal(t, true)
	withStdin(t, "u\n")
	out, _ := captureOutput(t, func() {
		if _, err := promptDispositions(mkHits()[1:], "GG-0001", "GG-0003", false); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "re-pointing a `verifies:` annotation asserts this test verifies the new requirement") {
		t.Fatalf("missing verifies repoint note: %q", out)
	}
}

func TestPromptDispositions_RetireUpdateNotApplicable(t *testing.T) {
	stubTerminal(t, true)
	withStdin(t, "u\nd\n")
	var ds []disposition
	out, _ := captureOutput(t, func() {
		var err error
		ds, err = promptDispositions(mkHits()[:1], "GG-0001", "", true)
		if err != nil {
			t.Fatal(err)
		}
	})
	if len(ds) != 1 || ds[0].action != 'd' {
		t.Fatalf("expected delete after update refused, got %+v", ds)
	}
	if !strings.Contains(out, "update not applicable on retire") {
		t.Fatalf("missing not-applicable message: %q", out)
	}
	if !strings.Contains(out, "[d]elete line / [a]bort") {
		t.Fatalf("missing retire prompt: %q", out)
	}
}

func TestPromptDispositions_Abort(t *testing.T) {
	stubTerminal(t, true)
	for _, input := range []string{"a\n", "\n"} {
		withStdin(t, input)
		_, err := promptDispositions(mkHits()[:1], "GG-0001", "GG-0003", false)
		requireErrContains(t, err, "aborted by user")
	}
}

func TestPromptDispositions_UnknownThenDelete(t *testing.T) {
	stubTerminal(t, true)
	withStdin(t, "bogus\nd\n")
	var ds []disposition
	out, _ := captureOutput(t, func() {
		var err error
		ds, err = promptDispositions(mkHits()[:1], "GG-0001", "GG-0003", false)
		if err != nil {
			t.Fatal(err)
		}
	})
	if len(ds) != 1 || ds[0].action != 'd' {
		t.Fatalf("expected delete after unknown choice, got %+v", ds)
	}
	if !strings.Contains(out, "unknown choice") {
		t.Fatalf("missing unknown-choice message: %q", out)
	}
}

func TestPromptDispositions_EOF(t *testing.T) {
	stubTerminal(t, true)
	withStdin(t, "") // immediate EOF
	_, err := promptDispositions(mkHits()[:1], "GG-0001", "GG-0003", false)
	requireErrContains(t, err, "aborted:")
}

// ---------- codeHits ----------

func TestAbsRefPath(t *testing.T) {
	c := &workCtx{root: "/r", reqDir: "/r/requirements", scanDir: "/r"}
	if got := absRefPath(c, "/abs/x.go"); got != "/abs/x.go" {
		t.Fatalf("absolute path must pass through, got %q", got)
	}
	if got := absRefPath(c, "sub/x.go"); got != "/r/sub/x.go" {
		t.Fatalf("relative path must anchor at root, got %q", got)
	}
}

func TestCodeHits_FiltersHistorical(t *testing.T) {
	root := t.TempDir()
	reqDir := filepath.Join(root, "requirements")
	c := &workCtx{root: root, reqDir: reqDir, scanDir: root}
	refs := []Ref{
		{ID: "GG-0001", File: "requirements/GG-0001.yaml", Line: 2},
		{ID: "GG-0001", File: "code.go", Line: 3},
		{ID: "GG-0002", File: "code.go", Line: 4},
	}
	hits := codeHits(c, refs, "GG-0001")
	if len(hits) != 1 || hits[0].File != "code.go" || hits[0].Line != 3 {
		t.Fatalf("unexpected hits: %+v", hits)
	}
}

// ---------- planFileEdits ----------

func planCtx(t *testing.T, files map[string]string) *workCtx {
	t.Helper()
	root, reqDir := stdLayout(t, files)
	return &workCtx{root: root, reqDir: reqDir, scanDir: root}
}

func TestPlanFileEdits_UpdateAndDelete(t *testing.T) {
	c := planCtx(t, map[string]string{
		"code.go": "line1\n// GG-0001: update me\nline3\n// GG-0001: delete me\nline5\n",
	})
	ds := []disposition{
		{ref: Ref{ID: "GG-0001", File: "code.go", Line: 2}, action: 'u'},
		{ref: Ref{ID: "GG-0001", File: "code.go", Line: 4}, action: 'd'},
	}
	before := readFile(t, filepath.Join(c.root, "code.go"))
	ws, err := planFileEdits(c, ds, "GG-0001", "GG-0003")
	if err != nil {
		t.Fatal(err)
	}
	if len(ws) != 1 {
		t.Fatalf("expected 1 staged write, got %d", len(ws))
	}
	w := ws[0]
	if !filepath.IsAbs(w.path) {
		t.Fatalf("staged path must be absolute, got %q", w.path)
	}
	if !w.existed || string(w.orig) != before {
		t.Fatal("staged write must carry the original content")
	}
	want := "line1\n// GG-0003: update me\nline3\nline5\n"
	if string(w.data) != want {
		t.Fatalf("staged content:\n%s\nwant:\n%s", w.data, want)
	}
	// Planning writes nothing.
	if got := readFile(t, filepath.Join(c.root, "code.go")); got != before {
		t.Fatal("planFileEdits must not write to disk")
	}
}

func TestPlanFileEdits_LineOutOfRange(t *testing.T) {
	c := planCtx(t, map[string]string{"code.go": "a\nb\n"})
	ds := []disposition{{ref: Ref{ID: "GG-0001", File: "code.go", Line: 99}, action: 'u'}}
	before := readFile(t, filepath.Join(c.root, "code.go"))
	ws, err := planFileEdits(c, ds, "GG-0001", "GG-0003")
	requireErrContains(t, err, "line out of range")
	if ws != nil {
		t.Fatalf("failed plan must stage nothing, got %v", ws)
	}
	if got := readFile(t, filepath.Join(c.root, "code.go")); got != before {
		t.Fatal("failed plan must not touch the file")
	}
}

func TestPlanFileEdits_ReadError(t *testing.T) {
	c := planCtx(t, nil)
	ds := []disposition{{ref: Ref{ID: "GG-0001", File: "nosuch.go", Line: 1}, action: 'u'}}
	_, err := planFileEdits(c, ds, "GG-0001", "GG-0003")
	if err == nil {
		t.Fatal("expected read error, got nil")
	}
}

func TestPlanFileEdits_UnknownActionKeepsLine(t *testing.T) {
	c := planCtx(t, map[string]string{"code.go": "// GG-0001: x\n"})
	ds := []disposition{{ref: Ref{ID: "GG-0001", File: "code.go", Line: 1}, action: 'x'}}
	ws, err := planFileEdits(c, ds, "GG-0001", "GG-0003")
	if err != nil {
		t.Fatal(err)
	}
	if string(ws[0].data) != "// GG-0001: x\n" {
		t.Fatalf("unknown action must leave the line alone: %q", ws[0].data)
	}
}

// Regression test: hit paths are repo-relative, but the mutation must resolve
// against --root, not the process working directory (the test process runs in
// the package dir, far from the temp root).
func TestPlanFileEdits_RootAnchored(t *testing.T) {
	c := planCtx(t, map[string]string{"code.go": "// GG-0001: x\n"})
	wd, _ := os.Getwd()
	if c.root == wd {
		t.Skip("test root unexpectedly equals the working directory")
	}
	ds := []disposition{{ref: Ref{ID: "GG-0001", File: "code.go", Line: 1}, action: 'u'}}
	ws, err := planFileEdits(c, ds, "GG-0001", "GG-0003")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ws[0].path, c.root) {
		t.Fatalf("staged path %q not anchored at root %q", ws[0].path, c.root)
	}
}

// ---------- commitWrites ----------

func TestCommitWrites_Happy(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	writeFile(t, a, "old")
	b := filepath.Join(dir, "b.txt")
	err := commitWrites([]pendingWrite{
		{path: a, data: []byte("new"), existed: true, orig: []byte("old")},
		{path: b, data: []byte("created")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if readFile(t, a) != "new" || readFile(t, b) != "created" {
		t.Fatal("commit did not apply writes")
	}
}

func TestCommitWrites_RollbackRestoresOriginals(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	writeFile(t, a, "original-a")
	badDir := filepath.Join(dir, "bad")
	if err := os.MkdirAll(badDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Writing to a directory path always fails, even for root: forces the
	// mid-commit failure after a.txt was already rewritten.
	err := commitWrites([]pendingWrite{
		{path: a, data: []byte("changed"), existed: true, orig: []byte("original-a")},
		{path: badDir, data: []byte("x"), existed: true, orig: []byte{}},
	})
	if err == nil {
		t.Fatal("expected commit error, got nil")
	}
	if readFile(t, a) != "original-a" {
		t.Fatalf("rollback did not restore a.txt: %q", readFile(t, a))
	}
	if st, serr := os.Stat(badDir); serr != nil || !st.IsDir() {
		t.Fatal("rollback must not disturb the failing path")
	}
}

func TestCommitWrites_RollbackRemovesNewFiles(t *testing.T) {
	dir := t.TempDir()
	fresh := filepath.Join(dir, "fresh.txt")
	badDir := filepath.Join(dir, "bad")
	if err := os.MkdirAll(badDir, 0755); err != nil {
		t.Fatal(err)
	}
	err := commitWrites([]pendingWrite{
		{path: fresh, data: []byte("new")}, // existed=false
		{path: badDir, data: []byte("x"), existed: true},
	})
	if err == nil {
		t.Fatal("expected commit error, got nil")
	}
	if _, serr := os.Stat(fresh); !os.IsNotExist(serr) {
		t.Fatal("rollback must remove brand-new files")
	}
}

func TestCommitWrites_Empty(t *testing.T) {
	if err := commitWrites(nil); err != nil {
		t.Fatal(err)
	}
}

// ---------- supersede / retire ----------

func TestSupersede_Usage(t *testing.T) {
	root, reqDir := supFixture(t)
	err := cmdSupersede(supArgs(root, reqDir))
	requireErrContains(t, err, "usage: req supersede GG-XXXX")
}

func TestSupersede_CtxError(t *testing.T) {
	root := t.TempDir()
	err := cmdSupersede(append(ctxFlags(root, filepath.Join(root, "missing"), root), "GG-0001"))
	requireErrContains(t, err, "requirements dir not found")
}

func TestSupersede_LoadError(t *testing.T) {
	root, reqDir := stdLayout(t, nil)
	writeFile(t, filepath.Join(reqDir, "GG-0001.yaml"), "id: [broken\n")
	err := cmdSupersede(supArgs(root, reqDir, "GG-0001", "--title", "T", "--statement", "S"))
	if err == nil {
		t.Fatal("expected registry load error, got nil")
	}
}

func TestRetire_Usage(t *testing.T) {
	root, reqDir := supFixture(t)
	err := cmdRetire(supArgs(root, reqDir))
	requireErrContains(t, err, "usage: req retire GG-XXXX")
}

func TestSupersede_UnknownID(t *testing.T) {
	root, reqDir := supFixture(t)
	err := cmdSupersede(supArgs(root, reqDir, "GG-0099", "--title", "T", "--statement", "S"))
	requireErrContains(t, err, "unknown requirement id GG-0099")
}

func TestSupersede_AlreadyDone(t *testing.T) {
	root, reqDir := stdLayout(t, map[string]string{
		"requirements/GG-0001.yaml": reqFile("GG-0001", "superseded", "", "GG-0002"),
		"requirements/GG-0002.yaml": reqFile("GG-0002", "retired", "", ""),
	})
	err := cmdSupersede(supArgs(root, reqDir, "GG-0001", "--title", "T", "--statement", "S"))
	requireErrContains(t, err, "GG-0001 is already superseded")
	err = cmdRetire(supArgs(root, reqDir, "GG-0002"))
	requireErrContains(t, err, "GG-0002 is already retired")
}

func TestSupersede_DraftRefused(t *testing.T) {
	root, reqDir := supFixture(t) // GG-0002 is draft
	err := cmdSupersede(supArgs(root, reqDir, "GG-0002", "--title", "T", "--statement", "S"))
	requireErrContains(t, err, "still draft")
	err = cmdRetire(supArgs(root, reqDir, "GG-0002"))
	requireErrContains(t, err, "still draft")
}

func TestSupersede_MissingTitleStatement(t *testing.T) {
	root, reqDir := supFixture(t)
	err := cmdSupersede(supArgs(root, reqDir, "GG-0001", "--title", "T"))
	requireErrContains(t, err, "requires --title and --statement")
}

func TestSupersede_BadNewParent(t *testing.T) {
	root, reqDir := supFixture(t)
	err := cmdSupersede(supArgs(root, reqDir, "GG-0001",
		"--title", "T", "--statement", "S", "--parent", "GG-0099"))
	requireErrContains(t, err, "parent GG-0099 not in registry")
}

// Zero hits: no prompt, no terminal needed. GG-0002's superseded_by link to
// GG-0001 is a historical reference and is kept as-is.
func TestSupersede_ZeroHits(t *testing.T) {
	root, reqDir := stdLayout(t, map[string]string{
		"requirements/GG-0001.yaml": reqFile("GG-0001", "active", "", ""),
		"requirements/GG-0002.yaml": reqFile("GG-0002", "superseded", "", "GG-0001"),
	})
	out, _ := captureOutput(t, func() {
		if err := cmdSupersede(supArgs(root, reqDir, "GG-0001",
			"--title", "New wording", "--statement", "New statement.")); err != nil {
			t.Fatal(err)
		}
	})
	for _, want := range []string{
		// Note: "%sing" with op="supersede" renders "supersedeing".
		"supersedeing GG-0001: 0 codebase hit(s), 5 historical reference(s) kept as-is",
		"kept (historical): requirements/GG-0002.yaml",
		"supersede complete",
		`req(GG-0001): superseded by GG-0003`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n---\n%s", want, out)
		}
	}
	reg, err := loadRegistry(reqDir)
	if err != nil {
		t.Fatal(err)
	}
	old := reg["GG-0001"]
	if old.Status != "superseded" || old.SupersededBy != "GG-0003" {
		t.Fatalf("old record not superseded: %+v", old)
	}
	if !strings.Contains(strings.Join(old.History, "\n"), "superseded by GG-0003") {
		t.Fatalf("old history missing: %v", old.History)
	}
	nw := reg["GG-0003"]
	if nw == nil || nw.Status != "draft" || nw.Title != "New wording" {
		t.Fatalf("new record wrong: %+v", nw)
	}
	if !strings.Contains(strings.Join(nw.History, "\n"), "supersedes GG-0001") {
		t.Fatalf("new history missing: %v", nw.History)
	}
}

func TestSupersede_WithHits_InteractiveUpdate(t *testing.T) {
	root, reqDir := supFixture(t)
	stubTerminal(t, true)
	withStdin(t, "u\nu\n")
	var out string
	out, _ = captureOutput(t, func() {
		if err := cmdSupersede(supArgs(root, reqDir, "GG-0001",
			"--title", "T", "--statement", "S")); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "asserts this test verifies the new requirement") {
		t.Fatalf("missing verifies repoint note:\n%s", out)
	}
	if got := readFile(t, filepath.Join(root, "code.go")); !strings.Contains(got, "// GG-0003: use it") {
		t.Fatalf("code.go not repointed:\n%s", got)
	}
	if got := readFile(t, filepath.Join(root, "code_test.go")); !strings.Contains(got, "// verifies: GG-0003") {
		t.Fatalf("code_test.go not repointed:\n%s", got)
	}
	reg, err := loadRegistry(reqDir)
	if err != nil {
		t.Fatal(err)
	}
	if reg["GG-0001"].SupersededBy != "GG-0003" || reg["GG-0003"] == nil {
		t.Fatal("registry not updated")
	}
}

func TestSupersede_WithHits_NonInteractiveRefusal(t *testing.T) {
	root, reqDir := supFixture(t)
	stubTerminal(t, false)
	snap := snapshotTree(t, root)
	err := cmdSupersede(supArgs(root, reqDir, "GG-0001", "--title", "T", "--statement", "S"))
	requireErrContains(t, err, "refusing non-interactive")
	assertTreeIdentical(t, root, snap)
}

// Atomicity: the minted GG-0003.yaml path is blocked by a directory, so the
// commit fails AFTER the code-file writes. Everything must roll back.
func TestSupersede_MidCommitFailure_Rollback(t *testing.T) {
	root, reqDir := supFixture(t)
	if err := os.MkdirAll(filepath.Join(reqDir, "GG-0003.yaml"), 0755); err != nil {
		t.Fatal(err)
	}
	stubTerminal(t, true)
	withStdin(t, "u\nu\n")
	snap := snapshotTree(t, root)
	err := cmdSupersede(supArgs(root, reqDir, "GG-0001", "--title", "T", "--statement", "S"))
	requireErrContains(t, err, "rolled back")
	assertTreeIdentical(t, root, snap)
	if st, serr := os.Stat(filepath.Join(reqDir, "GG-0003.yaml")); serr != nil || !st.IsDir() {
		t.Fatal("the blocking GG-0003.yaml directory must be untouched")
	}
	// The registry must be byte-identical: GG-0001.yaml still active.
	reg, lerr := loadRegistry(reqDir)
	if lerr != nil {
		t.Fatal(lerr)
	}
	if reg["GG-0001"].Status != "active" || reg["GG-0001"].SupersededBy != "" {
		t.Fatalf("registry was mutated despite rollback: %+v", reg["GG-0001"])
	}
	if reg["GG-0003"] != nil {
		t.Fatal("GG-0003 must not exist after rollback")
	}
}

func TestSupersede_AbortLeavesUntouched(t *testing.T) {
	root, reqDir := supFixture(t)
	stubTerminal(t, true)
	withStdin(t, "a\n")
	snap := snapshotTree(t, root)
	err := cmdSupersede(supArgs(root, reqDir, "GG-0001", "--title", "T", "--statement", "S"))
	requireErrContains(t, err, "aborted by user")
	assertTreeIdentical(t, root, snap)
}

func TestRetire_ZeroHits(t *testing.T) {
	root, reqDir := stdLayout(t, map[string]string{
		"requirements/GG-0001.yaml": reqFile("GG-0001", "active", "", ""),
	})
	out, _ := captureOutput(t, func() {
		if err := cmdRetire(supArgs(root, reqDir, "GG-0001", "--reason", "obsolete")); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "retire complete") || !strings.Contains(out, "req(GG-0001): retired") {
		t.Fatalf("unexpected retire output:\n%s", out)
	}
	reg, err := loadRegistry(reqDir)
	if err != nil {
		t.Fatal(err)
	}
	if reg["GG-0001"].Status != "retired" {
		t.Fatalf("not retired: %+v", reg["GG-0001"])
	}
	if !strings.Contains(strings.Join(reg["GG-0001"].History, "\n"), "retired (obsolete)") {
		t.Fatalf("history missing reason: %v", reg["GG-0001"].History)
	}
}

func TestRetire_DefaultReason(t *testing.T) {
	root, reqDir := stdLayout(t, map[string]string{
		"requirements/GG-0001.yaml": reqFile("GG-0001", "active", "", ""),
	})
	captureOutput(t, func() {
		if err := cmdRetire(supArgs(root, reqDir, "GG-0001")); err != nil {
			t.Fatal(err)
		}
	})
	reg, err := loadRegistry(reqDir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(reg["GG-0001"].History, "\n"), "retired (retired)") {
		t.Fatalf("default reason missing: %v", reg["GG-0001"].History)
	}
}

func TestRetire_WithHits_InteractiveDelete(t *testing.T) {
	root, reqDir := supFixture(t)
	stubTerminal(t, true)
	withStdin(t, "d\nd\n")
	captureOutput(t, func() {
		if err := cmdRetire(supArgs(root, reqDir, "GG-0001", "--reason", "gone")); err != nil {
			t.Fatal(err)
		}
	})
	if got := readFile(t, filepath.Join(root, "code.go")); strings.Contains(got, "GG-0001") {
		t.Fatalf("hit line not deleted:\n%s", got)
	}
	if got := readFile(t, filepath.Join(root, "code_test.go")); strings.Contains(got, "verifies: GG-0001") {
		t.Fatalf("verifies line not deleted:\n%s", got)
	}
	reg, err := loadRegistry(reqDir)
	if err != nil {
		t.Fatal(err)
	}
	if reg["GG-0001"].Status != "retired" {
		t.Fatalf("not retired: %+v", reg["GG-0001"])
	}
}

func TestRetire_WithHits_NonInteractiveRefusal(t *testing.T) {
	root, reqDir := supFixture(t)
	stubTerminal(t, false)
	snap := snapshotTree(t, root)
	err := cmdRetire(supArgs(root, reqDir, "GG-0001"))
	requireErrContains(t, err, "refusing non-interactive")
	assertTreeIdentical(t, root, snap)
}
