package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scanCtx builds a temp tree with an (initially empty) requirements dir and a
// workCtx scanning the whole root.
func scanCtx(t *testing.T, files map[string]string) (string, *workCtx) {
	t.Helper()
	root, reqDir := stdLayout(t, files)
	return root, &workCtx{root: root, reqDir: reqDir, scanDir: root}
}

func refsByID(refs []Ref) map[string][]Ref {
	m := map[string][]Ref{}
	for _, r := range refs {
		m[r.ID] = append(m[r.ID], r)
	}
	return m
}

func TestScan_BasicRefAndVerifies(t *testing.T) {
	_, c := scanCtx(t, map[string]string{
		"code.go": "package x\n\n// GG-0001: implement the thing\nvar _ = 1\n",
		"code_test.go": "package x\n\nimport \"testing\"\n\n// verifies: GG-0001\n" +
			"func TestThing(t *testing.T) {}\n",
	})
	refs, fmts, err := scanAll(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(fmts) != 0 {
		t.Fatalf("unexpected format problems: %v", fmts)
	}
	byID := refsByID(refs)
	if len(byID["GG-0001"]) != 2 {
		t.Fatalf("expected 2 refs to GG-0001, got %v", byID["GG-0001"])
	}
	var kinds []string
	for _, r := range byID["GG-0001"] {
		kinds = append(kinds, r.Kind.String())
	}
	if !(kinds[0] == "ref" && kinds[1] == "verifies") {
		t.Fatalf("kinds = %v, want [ref verifies] (sorted by file, then line)", kinds)
	}
	r := byID["GG-0001"][1]
	if r.File != "code_test.go" || r.Line != 5 || !strings.Contains(r.Text, "verifies: GG-0001") {
		t.Fatalf("unexpected verifies ref: %+v", r)
	}
}

func TestScan_NamespacedIDs(t *testing.T) {
	_, c := scanCtx(t, map[string]string{
		"code.go":      "package x\n\n// GG-SCHED-0001: implement the thing\nvar _ = 1\n",
		"code_test.go": "package x\n\nimport \"testing\"\n\n// verifies: GG-SCHED-0001\nfunc TestThing(t *testing.T) {}\n",
		"sloppy.go":    "package x\n\n// GG-sched-0001 looks like an id but is not canonical\nvar _ = 2\n",
	})
	refs, fmts, err := scanAll(c)
	if err != nil {
		t.Fatal(err)
	}
	byID := refsByID(refs)
	if len(byID["GG-SCHED-0001"]) != 2 {
		t.Fatalf("expected 2 refs to GG-SCHED-0001, got %v", byID["GG-SCHED-0001"])
	}
	// The lowercase lookalike must be flagged as non-canonical, not silently dropped.
	found := false
	for _, f := range fmts {
		if strings.Contains(f, "GG-sched-0001") && strings.Contains(f, "non-canonical") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected non-canonical flag for GG-sched-0001, fmts = %v", fmts)
	}
}

func TestScan_VerifiesLineWithTwoIDs(t *testing.T) {
	_, c := scanCtx(t, map[string]string{
		"a_test.go": "package x\n// verifies: GG-0001 and GG-0002\nfunc T() {}\n",
	})
	refs, _, err := scanAll(c)
	if err != nil {
		t.Fatal(err)
	}
	byID := refsByID(refs)
	if len(byID["GG-0001"]) != 1 || byID["GG-0001"][0].Kind != KindVerifies {
		t.Fatalf("GG-0001 should be verifies kind: %v", byID["GG-0001"])
	}
	// NOTE(scan-contract): in *_test.go only `verifies:` annotations are
	// scanned. A second ID on the same line that is not itself introduced
	// by `verifies:` is a plain ref and is deliberately ignored in test
	// files, so fixture IDs in test sources never pollute `req lint`.
	if _, ok := byID["GG-0002"]; ok {
		t.Fatalf("GG-0002 plain ref in _test.go must be ignored: %v", byID["GG-0002"])
	}
}

func TestScan_VerifiesIsCaseSensitive(t *testing.T) {
	_, c := scanCtx(t, map[string]string{
		"a_test.go": "package x\n// Verifies: GG-0001\nfunc T() {}\n",
	})
	refs, _, err := scanAll(c)
	if err != nil {
		t.Fatal(err)
	}
	// NOTE(scan-contract): capitalized `Verifies:` is not a verifies
	// annotation, and plain refs in _test.go are not scanned at all.
	if len(refs) != 0 {
		t.Fatalf("capitalized Verifies: in _test.go must be fully ignored: %+v", refs)
	}
}

// The scanner deliberately does NOT enforce the AGENTS.md convention that
// `verifies:` sits directly above a test function: any `verifies: GG-XXXX`
// line is the authoritative mapping. This test pins that behavior; the
// convention/enforcement gap is recorded in TESTING.md.
func TestScan_VerifiesPositionNotEnforced(t *testing.T) {
	_, c := scanCtx(t, map[string]string{
		"a.go": "package x\n\n// verifies: GG-0001\nvar _ = 1\n",
	})
	refs, _, err := scanAll(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].Kind != KindVerifies {
		t.Fatalf("verifies annotation away from any test must still map: %+v", refs)
	}
}

func TestScan_MarkdownCodeExempt(t *testing.T) {
	_, c := scanCtx(t, map[string]string{
		"doc.md": "# Doc\n\n```go\n// GG-9999: illustrative only\n```\n\n" +
			"Inline `GG-9999` is also illustrative.\n\nBut GG-0001 here is real.\n",
	})
	refs, _, err := scanAll(c)
	if err != nil {
		t.Fatal(err)
	}
	byID := refsByID(refs)
	if _, ok := byID["GG-9999"]; ok {
		t.Fatalf("fenced/inline IDs in markdown must be exempt: %v", byID["GG-9999"])
	}
	if len(byID["GG-0001"]) != 1 {
		t.Fatalf("real ID outside code spans must be found: %v", byID)
	}
}

func TestScan_MarkdownSuffixCaseInsensitive(t *testing.T) {
	_, c := scanCtx(t, map[string]string{
		"README.MD": "```\nGG-9999\n```\n",
	})
	refs, _, err := scanAll(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 0 {
		t.Fatalf("uppercase .MD must be treated as markdown: %+v", refs)
	}
}

func TestScan_NonMarkdownFencesNotStripped(t *testing.T) {
	_, c := scanCtx(t, map[string]string{
		"notes.txt": "```\nGG-0001 inside fences in a .txt file is still a ref\n```\n",
	})
	refs, _, err := scanAll(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].ID != "GG-0001" {
		t.Fatalf("code fences are only special in markdown: %+v", refs)
	}
}

func TestScan_BinarySkipped(t *testing.T) {
	root, c := scanCtx(t, nil)
	writeFile(t, filepath.Join(root, "blob.bin"), "package x\n// GG-0001\x00more\n")
	refs, _, err := scanAll(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 0 {
		t.Fatalf("binary file must be skipped: %+v", refs)
	}
}

func TestScan_DanglingSymlinkSkipped(t *testing.T) {
	root, c := scanCtx(t, nil)
	if err := os.Symlink(filepath.Join(root, "nope.go"), filepath.Join(root, "link.go")); err != nil {
		t.Fatal(err)
	}
	refs, _, err := scanAll(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 0 {
		t.Fatalf("expected no refs, got %+v", refs)
	}
}

func TestScan_MissingScanDir(t *testing.T) {
	root, reqDir := stdLayout(t, nil)
	c := &workCtx{root: root, reqDir: reqDir, scanDir: filepath.Join(root, "nosuchdir")}
	refs, fmts, err := scanAll(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 0 || len(fmts) != 0 {
		t.Fatalf("missing scan dir must scan empty, got %v %v", refs, fmts)
	}
}

func TestScan_GitDirSkipped(t *testing.T) {
	_, c := scanCtx(t, map[string]string{
		".git/objects/x": "// GG-0001 in git internals\n",
		"code.go":        "// GG-0001 real\n",
	})
	refs, _, err := scanAll(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].File != "code.go" {
		t.Fatalf(".git must be skipped: %+v", refs)
	}
}

func TestScan_OwnIDLineSuppressed(t *testing.T) {
	_, c := scanCtx(t, map[string]string{
		"requirements/GG-0001.yaml": reqFile("GG-0001", "active", "GG-0002", ""),
		"requirements/GG-0002.yaml": reqFile("GG-0002", "superseded", "", "GG-0001"),
	})
	refs, _, err := scanAll(c)
	if err != nil {
		t.Fatal(err)
	}
	byID := refsByID(refs)
	for _, r := range byID["GG-0001"] {
		if r.File == "requirements/GG-0001.yaml" && r.Line == 1 {
			t.Fatalf("a record's own `id:` line must not be a reference: %+v", r)
		}
	}
	// ...but other mentions inside registry files are real references
	// (parent/superseded_by links), and they are historical, not exempt here.
	found := false
	for _, r := range byID["GG-0002"] {
		if r.File == "requirements/GG-0001.yaml" && strings.Contains(r.Text, "parent:") {
			found = true
		}
	}
	if !found {
		t.Fatalf("parent link inside registry YAML must be scanned: %v", byID["GG-0002"])
	}
}

func TestScan_SloppyIDs(t *testing.T) {
	_, c := scanCtx(t, map[string]string{
		"code.go": "package x\n// GG-42 sloppy\n// gg-0042 lowercase\n// GG-0001 canonical\nvar _ = 1\n",
	})
	_, fmts, err := scanAll(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(fmts) != 2 {
		t.Fatalf("expected 2 format problems, got %v", fmts)
	}
	joined := strings.Join(fmts, "\n")
	if !strings.Contains(joined, "non-canonical requirement id GG-42") ||
		!strings.Contains(joined, "non-canonical requirement id gg-0042") {
		t.Fatalf("unexpected format messages: %v", fmts)
	}
	for _, f := range fmts {
		if !strings.HasPrefix(f, "code.go:") {
			t.Fatalf("format message must carry file:line, got %q", f)
		}
	}
}

func TestScan_SloppyInMarkdownFenceExempt(t *testing.T) {
	_, c := scanCtx(t, map[string]string{
		"doc.md": "```\nGG-42 illustrative\n```\n",
	})
	_, fmts, err := scanAll(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(fmts) != 0 {
		t.Fatalf("sloppy IDs in markdown code spans must be exempt: %v", fmts)
	}
}

func TestScan_WordBoundaries(t *testing.T) {
	_, c := scanCtx(t, map[string]string{
		"code.go": "package x\n// XGG-0001 no\n// GG-0001x no\n// GG-00010 yes (5 digits, canonical)\n",
	})
	refs, _, err := scanAll(c)
	if err != nil {
		t.Fatal(err)
	}
	byID := refsByID(refs)
	if _, ok := byID["GG-0001"]; ok {
		t.Fatalf("XGG-0001 / GG-0001x must not match: %v", refs)
	}
	if len(byID["GG-00010"]) != 1 {
		t.Fatalf("GG-00010 must match as canonical: %v", refs)
	}
}

func TestScan_SortedByFileThenLine(t *testing.T) {
	_, c := scanCtx(t, map[string]string{
		"b.go": "// GG-0001\n// GG-0002\n",
		"a.go": "// GG-0003\n// GG-0004\n",
	})
	refs, _, err := scanAll(c)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range refs {
		got = append(got, r.File+":"+itoa(r.Line))
	}
	want := []string{"a.go:1", "a.go:2", "b.go:1", "b.go:2"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("refs order = %v, want %v", got, want)
	}
}
