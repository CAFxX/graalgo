package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// hasRequirementsAbove reports whether dir or any parent up to / contains a
// "requirements" directory. Used to keep the findRoot failure test hermetic.
func hasRequirementsAbove(dir string) bool {
	for {
		if st, err := os.Stat(filepath.Join(dir, "requirements")); err == nil && st.IsDir() {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

func TestFindRoot_Success(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "requirements"), 0755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	withCwd(t, sub)
	got, err := findRoot()
	if err != nil {
		t.Fatal(err)
	}
	if got != root {
		t.Fatalf("findRoot() = %q, want %q", got, root)
	}
}

func TestFindRoot_NotFound(t *testing.T) {
	dir := t.TempDir()
	if hasRequirementsAbove(dir) {
		t.Skip("a requirements/ dir exists above the temp dir on this machine; cannot test the not-found path hermetically")
	}
	withCwd(t, dir)
	_, err := findRoot()
	requireErrContains(t, err, "no requirements/ directory found")
}

func TestResolveCtx_RootFlag(t *testing.T) {
	root, reqDir := stdLayout(t, nil)
	c, err := resolveCtx(root, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if c.root != root {
		t.Fatalf("root = %q, want %q", c.root, root)
	}
	if c.reqDir != reqDir {
		t.Fatalf("reqDir = %q, want %q", c.reqDir, reqDir)
	}
	if c.scanDir != root {
		t.Fatalf("scanDir = %q, want root %q", c.scanDir, root)
	}
}

func TestResolveCtx_ReqDirMissing(t *testing.T) {
	root := t.TempDir()
	_, err := resolveCtx(root, "", "")
	requireErrContains(t, err, "requirements dir not found")
}

func TestResolveCtx_ReqDirIsFile(t *testing.T) {
	root := t.TempDir()
	f := filepath.Join(root, "requirements")
	if err := os.WriteFile(f, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := resolveCtx(root, "", "")
	requireErrContains(t, err, "requirements dir not found")
}

func TestResolveCtx_ScanDirFlagSetsRoot(t *testing.T) {
	root := t.TempDir()
	scan := filepath.Join(root, "src")
	if err := os.MkdirAll(scan, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(scan, "requirements"), 0755); err != nil {
		t.Fatal(err)
	}
	c, err := resolveCtx("", "", scan)
	if err != nil {
		t.Fatal(err)
	}
	abs, _ := filepath.Abs(scan)
	if c.root != abs {
		t.Fatalf("root = %q, want %q", c.root, abs)
	}
	if c.scanDir != scan {
		t.Fatalf("scanDir = %q, want %q", c.scanDir, scan)
	}
}

func TestResolveCtx_ReqDirFlagSetsRoot(t *testing.T) {
	root := t.TempDir()
	reqDir := filepath.Join(root, "myreqs")
	if err := os.MkdirAll(reqDir, 0755); err != nil {
		t.Fatal(err)
	}
	c, err := resolveCtx("", reqDir, "")
	if err != nil {
		t.Fatal(err)
	}
	abs, _ := filepath.Abs(reqDir)
	if c.root != abs {
		t.Fatalf("root = %q, want %q", c.root, abs)
	}
	if c.reqDir != reqDir {
		t.Fatalf("reqDir = %q, want %q (flag value kept as given)", c.reqDir, reqDir)
	}
}

func TestResolveCtx_DefaultFindRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "requirements"), 0755); err != nil {
		t.Fatal(err)
	}
	withCwd(t, filepath.Join(root))
	c, err := resolveCtx("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if c.root != root {
		t.Fatalf("root = %q, want %q", c.root, root)
	}
}

func TestResolveCtx_FindRootError(t *testing.T) {
	dir := t.TempDir()
	if hasRequirementsAbove(dir) {
		t.Skip("a requirements/ dir exists above the temp dir on this machine; cannot test the not-found path hermetically")
	}
	withCwd(t, dir)
	_, err := resolveCtx("", "", "")
	requireErrContains(t, err, "no requirements/ directory found")
}

func TestLoadRegistry_Happy(t *testing.T) {
	_, reqDir := stdLayout(t, map[string]string{
		"requirements/GG-0001.yaml": reqFile("GG-0001", "active", "", ""),
		"requirements/GG-0002.yaml": reqFile("GG-0002", "draft", "GG-0001", ""),
		"requirements/notes.txt":    "not a requirement",
		"requirements/GG-1.yaml":    reqFile("GG-1", "draft", "", ""), // name doesn't match GG-XXXX.yaml: ignored
	})
	// subdirectories are not scanned (non-recursive)
	if err := os.MkdirAll(filepath.Join(reqDir, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(reqDir, "sub", "GG-0003.yaml"), reqFile("GG-0003", "draft", "", ""))

	reg, err := loadRegistry(reqDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(reg) != 2 {
		t.Fatalf("loaded %d requirements, want 2", len(reg))
	}
	r := reg["GG-0002"]
	if r.Title != "Title GG-0002" || r.Parent != "GG-0001" || r.Status != "draft" {
		t.Fatalf("unexpected record: %+v", r)
	}
	if len(r.History) != 1 || !strings.Contains(r.History[0], "created (draft)") {
		t.Fatalf("unexpected history: %v", r.History)
	}
}

func TestLoadRegistry_NamespacedIDs(t *testing.T) {
	_, reqDir := stdLayout(t, map[string]string{
		"requirements/GG-0001.yaml":       reqFile("GG-0001", "active", "", ""),
		"requirements/GG-SCHED-0001.yaml": reqFile("GG-SCHED-0001", "draft", "GG-0001", ""),
		"requirements/GG-WAKE-0001.yaml":  reqFile("GG-WAKE-0001", "draft", "", ""),
	})
	reg, err := loadRegistry(reqDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(reg) != 3 {
		t.Fatalf("loaded %d requirements, want 3", len(reg))
	}
	if reg["GG-SCHED-0001"].Parent != "GG-0001" {
		t.Fatalf("unexpected parent: %+v", reg["GG-SCHED-0001"])
	}
}

func TestLoadRegistry_EmptyDir(t *testing.T) {
	_, reqDir := stdLayout(t, nil)
	reg, err := loadRegistry(reqDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(reg) != 0 {
		t.Fatalf("expected empty registry, got %d", len(reg))
	}
}

func TestLoadRegistry_ReadDirError(t *testing.T) {
	_, err := loadRegistry(filepath.Join(t.TempDir(), "nosuchdir"))
	if err == nil {
		t.Fatal("expected ReadDir error, got nil")
	}
}

func TestLoadRegistry_MalformedYAML(t *testing.T) {
	_, reqDir := stdLayout(t, nil)
	writeFile(t, filepath.Join(reqDir, "GG-0001.yaml"), "id: [unclosed\n  bad: : :\n")
	_, err := loadRegistry(reqDir)
	requireErrContains(t, err, "GG-0001.yaml")
}

func TestLoadRegistry_ReadError(t *testing.T) {
	_, reqDir := stdLayout(t, nil)
	// A dangling symlink matches GG-XXXX.yaml but cannot be read.
	if err := os.Symlink(filepath.Join(reqDir, "nope.yaml"), filepath.Join(reqDir, "GG-0001.yaml")); err != nil {
		t.Fatal(err)
	}
	_, err := loadRegistry(reqDir)
	if err == nil {
		t.Fatal("expected read error for dangling symlink, got nil")
	}
}

func TestLoadRegistry_FilenameRecordMismatch(t *testing.T) {
	_, reqDir := stdLayout(t, nil)
	writeFile(t, filepath.Join(reqDir, "GG-0001.yaml"), reqFile("GG-0002", "draft", "", ""))
	_, err := loadRegistry(reqDir)
	requireErrContains(t, err, "filename ID GG-0001 != record id")
}

func TestLoadRegistry_InvalidStatus(t *testing.T) {
	_, reqDir := stdLayout(t, nil)
	writeFile(t, filepath.Join(reqDir, "GG-0001.yaml"), reqFile("GG-0001", "bogus", "", ""))
	_, err := loadRegistry(reqDir)
	requireErrContains(t, err, `invalid status "bogus"`)
}

func TestSave_RoundTrip(t *testing.T) {
	_, reqDir := stdLayout(t, nil)
	path := filepath.Join(reqDir, "GG-0007.yaml")
	r := &Requirement{
		ID: "GG-0007", Title: "T", Statement: "S", Rationale: "R",
		Parent: "GG-0001", Status: "active", VerificationPlan: "V",
		History: []string{"2026-10-01: created (draft)"},
		file:    path,
	}
	if err := r.save(); err != nil {
		t.Fatal(err)
	}
	reg, err := loadRegistry(reqDir)
	if err != nil {
		t.Fatal(err)
	}
	got := reg["GG-0007"]
	if got.Title != "T" || got.Statement != "S" || got.Rationale != "R" ||
		got.Parent != "GG-0001" || got.Status != "active" ||
		got.VerificationPlan != "V" || len(got.History) != 1 {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	// file must not serialize the internal path field
	if strings.Contains(readFile(t, path), "file:") {
		t.Fatal("saved YAML leaks the internal file field")
	}
}

func TestSave_WriteError(t *testing.T) {
	_, reqDir := stdLayout(t, nil)
	// Writing to a directory path always fails, even for root.
	r := &Requirement{ID: "GG-0001", file: reqDir}
	requireErrContains(t, r.save(), "is a directory")
}

func TestMintID_Empty(t *testing.T) {
	got, err := mintID(map[string]*Requirement{}, "")
	if err != nil {
		t.Fatalf("mintID(empty) err = %v", err)
	}
	if got != "GG-0001" {
		t.Fatalf("mintID(empty) = %q, want GG-0001", got)
	}
}

func TestMintID_GapNotFilled(t *testing.T) {
	reg := map[string]*Requirement{
		"GG-0001": {ID: "GG-0001"},
		"GG-0003": {ID: "GG-0003"},
	}
	// Monotonic: max+1, never backfills a gap (gaps stay empty forever,
	// which is what makes "never reuse" trivially auditable).
	got, err := mintID(reg, "")
	if err != nil {
		t.Fatalf("mintID err = %v", err)
	}
	if got != "GG-0004" {
		t.Fatalf("mintID = %q, want GG-0004", got)
	}
}

func TestMintID_NeverReusesRetired(t *testing.T) {
	// mintID scans records of every status: retired/superseded IDs still
	// occupy their numbers, so a retired ID can never be re-minted.
	reg := map[string]*Requirement{
		"GG-0001": {ID: "GG-0001", Status: "retired"},
		"GG-0002": {ID: "GG-0002", Status: "superseded", SupersededBy: "GG-0003"},
		"GG-0003": {ID: "GG-0003", Status: "active"},
	}
	got, err := mintID(reg, "")
	if err != nil {
		t.Fatalf("mintID err = %v", err)
	}
	if got != "GG-0004" {
		t.Fatalf("mintID = %q, want GG-0004", got)
	}
}

func TestMintID_Namespaced(t *testing.T) {
	reg := map[string]*Requirement{
		"GG-0001":       {ID: "GG-0001"},
		"GG-SCHED-0001": {ID: "GG-SCHED-0001"},
		"GG-SCHED-0003": {ID: "GG-SCHED-0003", Status: "retired"},
	}
	// Fresh namespace starts at 0001.
	got, err := mintID(reg, "WAKE")
	if err != nil {
		t.Fatalf("mintID(WAKE) err = %v", err)
	}
	if got != "GG-WAKE-0001" {
		t.Fatalf("mintID(WAKE) = %q, want GG-WAKE-0001", got)
	}
	// Existing namespace: max+1, gaps not filled, retired not reused.
	got, err = mintID(reg, "SCHED")
	if err != nil {
		t.Fatalf("mintID(SCHED) err = %v", err)
	}
	if got != "GG-SCHED-0004" {
		t.Fatalf("mintID(SCHED) = %q, want GG-SCHED-0004", got)
	}
	// Numeric series is unaffected by namespaced IDs.
	got, err = mintID(reg, "")
	if err != nil {
		t.Fatalf("mintID(\"\") err = %v", err)
	}
	if got != "GG-0002" {
		t.Fatalf("mintID(\"\") = %q, want GG-0002", got)
	}
}

func TestMintID_BadNamespace(t *testing.T) {
	for _, ns := range []string{"sched", "SCHED1", "SCH-ED", "Sched"} {
		if _, err := mintID(map[string]*Requirement{}, ns); err == nil {
			t.Fatalf("mintID(%q) accepted invalid namespace", ns)
		}
	}
}

func TestSplitID(t *testing.T) {
	cases := []struct {
		id  string
		ns  string
		seq int
		ok  bool
	}{
		{"GG-0001", "", 1, true},
		{"GG-0060", "", 60, true},
		{"GG-SCHED-0001", "SCHED", 1, true},
		{"GG-WAKE-0042", "WAKE", 42, true},
		{"GG-XYZ", "", 0, false},
		{"nope", "", 0, false},
	}
	for _, c := range cases {
		ns, seq, ok := splitID(c.id)
		if ns != c.ns || seq != c.seq || ok != c.ok {
			t.Fatalf("splitID(%q) = (%q,%d,%v), want (%q,%d,%v)",
				c.id, ns, seq, ok, c.ns, c.seq, c.ok)
		}
	}
}

func TestSortedIDs(t *testing.T) {
	reg := map[string]*Requirement{
		"GG-0010": {}, "GG-0002": {}, "GG-0001": {},
	}
	got := sortedIDs(reg)
	want := []string{"GG-0001", "GG-0002", "GG-0010"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("sortedIDs = %v, want %v", got, want)
	}
}
