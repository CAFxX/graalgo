package main

// Tests for -tags handling in `graalgo build` / `graalgo run`
// (REVIEW-2026-11-03-10 P1-1 remediation):
//
//   - parseBuildFlags captures -tags (and still rejects unknown flags)
//   - splitTags follows go's comma/whitespace splitting
//   - buildableGoFilesWithTags / filterBuildableFiles select files by
//     go/build constraints, in both tag directions
//   - tagsEnv forwards the flag value to the frontend child process
//     (GRAALGO_BUILD_TAGS, space-joined, user value preserved)
//   - graalgoRunEnv actually delivers that environment to the child
//
// The two-file foo/!foo package tests are differential against gc:
// the file set graalgo's filter selects must be the file set whose
// program output `go run [-tags foo]` prints. All tests are pure
// driver logic: no JVM backend is invoked.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseBuildFlagsTags(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    string
		wantErr bool
	}{
		{"separate value", []string{"-tags", "netgo"}, "netgo", false},
		{"equals form", []string{"-tags=netgo,osusergo"}, "netgo,osusergo", false},
		{"empty value", []string{"-tags="}, "", false},
		{"missing value", []string{"-tags"}, "", true},
		{"unknown flag", []string{"-frobnicate"}, "", true},
		{"tags among packages", []string{"-a", "-tags", "foo", "./pkg"}, "foo", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bf, err := parseBuildFlags(tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseBuildFlags(%q) = no error, want error", tc.args)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseBuildFlags(%q) = error %v, want tags %q", tc.args, err, tc.want)
			}
			if bf.tags != tc.want {
				t.Errorf("parseBuildFlags(%q).tags = %q, want %q", tc.args, bf.tags, tc.want)
			}
		})
	}
}

func TestSplitTags(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"foo", []string{"foo"}},
		{"foo,bar", []string{"foo", "bar"}},
		{"foo bar", []string{"foo", "bar"}},
		{"foo, bar  baz", []string{"foo", "bar", "baz"}},
		{",foo,,", []string{"foo"}},
	}
	for _, tc := range tests {
		got := splitTags(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("splitTags(%q) = %q, want %q", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("splitTags(%q) = %q, want %q", tc.in, got, tc.want)
				break
			}
		}
	}
}

func TestTagsEnv(t *testing.T) {
	if got := tagsEnv(""); got != nil {
		t.Errorf("tagsEnv(\"\") = %q, want nil (untagged builds must not touch the child env)", got)
	}
	if got := tagsEnv("foo,bar baz"); len(got) != 1 || got[0] != "GRAALGO_BUILD_TAGS=foo bar baz" {
		t.Errorf("tagsEnv(\"foo,bar baz\") = %q, want [GRAALGO_BUILD_TAGS=foo bar baz] (comma-joined would arrive as one bogus tag in diskimport)", got)
	}
	t.Setenv("GRAALGO_BUILD_TAGS", "preexisting")
	if got := tagsEnv("foo"); len(got) != 1 || got[0] != "GRAALGO_BUILD_TAGS=preexisting foo" {
		t.Errorf("tagsEnv(\"foo\") with user env = %q, want [GRAALGO_BUILD_TAGS=preexisting foo]", got)
	}
}

// writeTagPackage creates a two-file package with foo/!foo variants
// plus an untagged main that prints the selected variant. It is the
// fixture for both the filter tests and the gc differential below.
func writeTagPackage(t *testing.T, dir string) (fooFile, nofooFile, mainFile string) {
	t.Helper()
	writeFile(t, dir, "go.mod", "module example.com/tagtest\n\ngo 1.24\n")
	writeFile(t, dir, "main.go", "package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(variant) }\n")
	writeFile(t, dir, "foo.go", "//go:build foo\n\npackage main\n\nconst variant = \"foo\"\n")
	writeFile(t, dir, "nofoo.go", "//go:build !foo\n\npackage main\n\nconst variant = \"nofoo\"\n")
	return filepath.Join(dir, "foo.go"), filepath.Join(dir, "nofoo.go"), filepath.Join(dir, "main.go")
}

func TestBuildableGoFilesWithTagsDirectionality(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "//go:build mytag\n\npackage p\n\nconst A = 1\n")
	writeFile(t, dir, "b.go", "//go:build !mytag\n\npackage p\n\nconst B = 1\n")
	with := buildableGoFilesWithTags(dir, []string{"mytag"})
	without := buildableGoFilesWithTags(dir, nil)
	has := func(files []string, base string) bool {
		for _, f := range files {
			if filepath.Base(f) == base {
				return true
			}
		}
		return false
	}
	if !has(with, "a.go") || has(with, "b.go") {
		t.Errorf("buildableGoFilesWithTags(mytag) = %v, want a.go only", with)
	}
	if has(without, "a.go") || !has(without, "b.go") {
		t.Errorf("buildableGoFilesWithTags(nil) = %v, want b.go only", without)
	}
	// The untagged entry point must keep its old behavior exactly.
	plain := buildableGoFiles(dir)
	if len(plain) != len(without) {
		t.Errorf("buildableGoFiles() = %v, want same as nil-tags %v", plain, without)
	}
	// A directory ImportDir cannot read at all (conflicting package
	// names) still falls back to the full glob, so the backend —
	// not the filter — reports the real error. Only the NoGoError
	// "nothing is buildable" case yields empty.
	confDir := t.TempDir()
	writeFile(t, confDir, "a.go", "package p\n\nconst A = 1\n")
	writeFile(t, confDir, "b.go", "package q\n\nconst B = 1\n")
	if got := buildableGoFilesWithTags(confDir, []string{"other"}); len(got) != 2 {
		t.Errorf("buildableGoFilesWithTags(conflicting pkgs) = %v, want both files (fallback)", got)
	}
}

func TestFilterBuildableFilesTags(t *testing.T) {
	dir := t.TempDir()
	fooFile, nofooFile, mainFile := writeTagPackage(t, dir)
	files := []string{fooFile, nofooFile, mainFile}

	// Empty tags are NOT the identity: platform-default constraints
	// still apply, so the foo-gated file drops out even with no -tags
	// (this is the no-tags half of the original defect: a bare glob
	// compiled foo.go and nofoo.go together).
	if got := filterBuildableFiles(files, ""); len(got) != 2 || got[0] != nofooFile || got[1] != mainFile {
		t.Errorf("filterBuildableFiles(empty) = %v, want [%v %v]", got, nofooFile, mainFile)
	}
	// -tags foo selects the foo variant; the untagged main always stays.
	if got := filterBuildableFiles(files, "foo"); len(got) != 2 || got[0] != fooFile || got[1] != mainFile {
		t.Errorf("filterBuildableFiles(foo) = %v, want [%v %v]", got, fooFile, mainFile)
	}
	// No tags selects the !foo variant.
	if got := filterBuildableFiles(files, "other"); len(got) != 2 || got[0] != nofooFile || got[1] != mainFile {
		t.Errorf("filterBuildableFiles(other) = %v, want [%v %v]", got, nofooFile, mainFile)
	}
	// Comma+space splitting: "foo, bar" still selects the foo variant.
	if got := filterBuildableFiles(files, "foo, bar"); len(got) != 2 || got[0] != fooFile {
		t.Errorf("filterBuildableFiles(\"foo, bar\") = %v, want foo variant", got)
	}
	// A file that does not exist yet is kept so the backend reports
	// the real error, exactly as the no-tags path does.
	missing := filepath.Join(dir, "typo.go")
	if got := filterBuildableFiles([]string{missing}, "foo"); len(got) != 1 || got[0] != missing {
		t.Errorf("filterBuildableFiles(missing) = %v, want [%v]", got, missing)
	}
}

// TestResolveFilesWithTags checks the shared expansion helper:
// directories filter by constraints, explicitly named files (even
// constraint-excluded ones) pass through like gc's file-list builds,
// and the all-excluded case reports empty-with-nil-error so callers
// can print the tags-aware diagnostic.
func TestResolveFilesWithTags(t *testing.T) {
	dir := t.TempDir()
	fooFile, nofooFile, mainFile := writeTagPackage(t, dir)

	// Directory expansion under -tags foo.
	got, err := resolveFilesWithTags([]string{dir}, "foo")
	if err != nil {
		t.Fatalf("resolveFilesWithTags(dir, foo) = error %v", err)
	}
	if len(got) != 2 || got[0] != fooFile || got[1] != mainFile {
		t.Errorf("resolveFilesWithTags(dir, foo) = %v, want [%v %v]", got, fooFile, mainFile)
	}
	// Directory expansion with no tags: platform defaults only, in
	// the input (glob-lexical) order.
	got, err = resolveFilesWithTags([]string{dir}, "")
	if err != nil {
		t.Fatalf("resolveFilesWithTags(dir, \"\") = error %v", err)
	}
	if len(got) != 2 || got[0] != mainFile || got[1] != nofooFile {
		t.Errorf("resolveFilesWithTags(dir, \"\") = %v, want [%v %v]", got, mainFile, nofooFile)
	}
	// Explicitly named files pass through unfiltered, like gc:
	// nofoo.go is constraint-excluded under -tags foo, but naming
	// it explicitly still builds it.
	got, err = resolveFilesWithTags([]string{nofooFile}, "foo")
	if err != nil {
		t.Fatalf("resolveFilesWithTags(file, foo) = error %v", err)
	}
	if len(got) != 1 || got[0] != nofooFile {
		t.Errorf("resolveFilesWithTags(file, foo) = %v, want [%v] (explicit files are not filtered)", got, nofooFile)
	}
	// A directory whose files are ALL excluded reports empty + nil.
	onlyDir := t.TempDir()
	writeFile(t, onlyDir, "only.go", "//go:build onlythis\n\npackage main\n\nfunc main() {}\n")
	got, err = resolveFilesWithTags([]string{onlyDir}, "other")
	if err != nil {
		t.Fatalf("resolveFilesWithTags(all-excluded) = error %v, want empty+nil", err)
	}
	if len(got) != 0 {
		t.Errorf("resolveFilesWithTags(all-excluded) = %v, want empty", got)
	}
	// A truly empty directory still errors, as resolveFiles does.
	emptyDir := t.TempDir()
	if _, err := resolveFilesWithTags([]string{emptyDir}, "foo"); err == nil {
		t.Errorf("resolveFilesWithTags(empty dir) = no error, want \"no Go files matched\"")
	}
	// No-argument case expands the current directory with filtering.
	// (t.Chdir needs go1.24; this module is go1.21, so chdir manually.)
	t.Run("no-arg", func(t *testing.T) {
		old, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chdir(dir); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := os.Chdir(old); err != nil {
				t.Fatal(err)
			}
		}()
		got, err := resolveFilesWithTags(nil, "foo")
		if err != nil {
			t.Fatalf("resolveFilesWithTags(nil, foo) = error %v", err)
		}
		if len(got) != 2 {
			t.Errorf("resolveFilesWithTags(nil, foo) = %v, want 2 files", got)
		}
	})
}

// TestFilterMatchesGcOutput is the differential test: the variant file
// graalgo's filter selects under each -tags setting must be the one
// whose program output gc prints for the same setting. If the filter
// ever disagreed with gc, the two sides of this test would diverge.
func TestFilterMatchesGcOutput(t *testing.T) {
	goBin := findGoForTest(t)
	dir := t.TempDir()
	fooFile, nofooFile, mainFile := writeTagPackage(t, dir)
	files := []string{fooFile, nofooFile, mainFile}
	for _, tc := range []struct {
		tags     string
		wantOut  string
		wantFile string
	}{
		{"foo", "foo", fooFile},
		{"", "nofoo", nofooFile},
	} {
		args := []string{"run"}
		if tc.tags != "" {
			args = append(args, "-tags", tc.tags)
		}
		args = append(args, ".")
		cmd := exec.Command(goBin, args...)
		cmd.Dir = dir
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("gc go %v: %v", args, err)
		}
		if strings.TrimSpace(string(out)) != tc.wantOut {
			t.Fatalf("gc go %v printed %q, want %q (fixture broken)", args, out, tc.wantOut)
		}
		got := filterBuildableFiles(files, tc.tags)
		found := false
		for _, f := range got {
			if f == tc.wantFile {
				found = true
			}
			if f == fooFile || f == nofooFile {
				if f != tc.wantFile {
					t.Errorf("tags=%q: filter selected %v, but gc printed %q (want %v)", tc.tags, got, tc.wantOut, tc.wantFile)
				}
			}
		}
		if !found {
			t.Errorf("tags=%q: filter selected %v, missing gc's variant file %v", tc.tags, got, tc.wantFile)
		}
	}
}

// TestCmdBuildRunTagsEndToEnd drives the real cmdBuild/cmdRun entry
// points against a fake backend runner (no JVM): the fake
// graalgo-sync-run logs every invocation's argv and GRAALGO_BUILD_TAGS
// and satisfies --emit-wire=PATH with dummy JSON, so cmdBuild can run
// to artifact emission. It proves the wiring the unit tests cannot:
// that -tags actually changes the file list build/run hand to the
// backend, and that the frontend child sees the tags.
func TestCmdBuildRunTagsEndToEnd(t *testing.T) {
	findGoForTest(t) // cmdBuild's artifact emission compiles a launcher with gc
	work := t.TempDir()
	pkgDir := filepath.Join(work, "pkg")
	fooFile, nofooFile, mainFile := writeTagPackage(t, pkgDir)
	// NB: assertions below compare exact paths, not substrings:
	// "nofoo.go" contains "foo.go" as a substring.

	root := filepath.Join(work, "root")
	scriptDir := filepath.Join(root, "internal", "backend")
	logPath := filepath.Join(work, "invocations.log")
	script := "#!/bin/sh\n" +
		"echo \"ARGS: $@\" >> " + logPath + "\n" +
		"echo \"TAGS: $GRAALGO_BUILD_TAGS\" >> " + logPath + "\n" +
		"for a in \"$@\"; do case \"$a\" in --emit-wire=*) echo '{\"fake\":true}' > \"${a#--emit-wire=}\";; esac; done\n" +
		"exit 0\n"
	writeFile(t, scriptDir, "graalgo-sync-run", script)
	if err := os.Chmod(filepath.Join(scriptDir, "graalgo-sync-run"), 0o755); err != nil {
		t.Fatal(err)
	}

	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(work); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(old); err != nil {
			t.Fatal(err)
		}
	}()

	readLog := func() string {
		b, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	// cmdBuild -tags foo: the emit invocation must name foo.go and
	// main.go but not nofoo.go, and the child must see TAGS=foo.
	outPath := filepath.Join(work, "prog")
	if code := cmdBuild(root, []string{"-tags", "foo", "-o", outPath, pkgDir}); code != 0 {
		t.Fatalf("cmdBuild -tags foo = %d, want 0", code)
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("cmdBuild produced no artifact at %s: %v", outPath, err)
	}
	log := readLog()
	if !strings.Contains(log, fooFile) || !strings.Contains(log, mainFile) {
		t.Errorf("cmdBuild log %q: want %s and %s passed to backend", log, fooFile, mainFile)
	}
	if strings.Contains(log, nofooFile) {
		t.Errorf("cmdBuild log %q: %s must not reach the backend under -tags foo", log, nofooFile)
	}
	if !strings.Contains(log, "TAGS: foo") {
		t.Errorf("cmdBuild log %q: want child env TAGS: foo", log)
	}

	// cmdRun -tags foo: both the --check gate and the program run
	// must see the filtered list and the tags.
	if err := os.Remove(logPath); err != nil {
		t.Fatal(err)
	}
	if code := cmdRun(root, []string{"-tags", "foo", pkgDir}); code != 0 {
		t.Fatalf("cmdRun -tags foo = %d, want 0", code)
	}
	log = readLog()
	if strings.Contains(log, nofooFile) {
		t.Errorf("cmdRun log %q: %s must not reach the backend under -tags foo", log, nofooFile)
	}
	if strings.Count(log, "TAGS: foo") != 2 {
		t.Errorf("cmdRun log %q: want exactly 2 invocations (check + run) with TAGS: foo", log)
	}

	// cmdRun with no tags: the !foo variant is selected instead.
	if err := os.Remove(logPath); err != nil {
		t.Fatal(err)
	}
	if code := cmdRun(root, []string{pkgDir}); code != 0 {
		t.Fatalf("cmdRun (no tags) = %d, want 0", code)
	}
	log = readLog()
	if strings.Contains(log, fooFile) {
		t.Errorf("cmdRun (no tags) log %q: %s must not reach the backend without -tags foo", log, fooFile)
	}
	if !strings.Contains(log, nofooFile) {
		t.Errorf("cmdRun (no tags) log %q: want %s passed to backend", log, nofooFile)
	}
}

// findGoForTest locates a gc toolchain for the differential test.
func findGoForTest(t *testing.T) string {
	t.Helper()
	if p, err := exec.LookPath("go"); err == nil {
		return p
	}
	for _, p := range []string{filepath.Join(os.Getenv("HOME"), "tools", "go", "bin", "go")} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Skip("no go toolchain on PATH for gc differential")
	return ""
}

// TestGraalgoRunEnvForwardsTags proves the child process actually
// receives the tagsEnv value: a fake graalgo-sync-run script echoes
// its environment, and the test reads it back through graalgoRunEnv's
// own stdout wiring. This covers the driver→child hop; the JVM
// launchers pass their environment to gofrontend unchanged (only
// GODEBUG is scrubbed), so the value reaches diskimport.go.
func TestGraalgoRunEnvForwardsTags(t *testing.T) {
	root := t.TempDir()
	scriptDir := filepath.Join(root, "internal", "backend")
	if err := os.MkdirAll(scriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(scriptDir, "graalgo-sync-run")
	writeFile(t, scriptDir, "graalgo-sync-run",
		"#!/bin/sh\n echo \"TAGS=[$GRAALGO_BUILD_TAGS] ARGS=[$*]\"\n")
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}

	// Capture graalgoRunEnv's stdout (it wires the child to os.Stdout).
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	code := graalgoRunEnv(root, []string{"--check", "x.go"}, tagsEnv("foo,bar"))
	w.Close()
	os.Stdout = old
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	r.Close()
	out := string(buf[:n])
	if code != 0 {
		t.Fatalf("graalgoRunEnv = %d, want 0 (output %q)", code, out)
	}
	if !strings.Contains(out, "TAGS=[foo bar]") {
		t.Errorf("child saw %q, want TAGS=[foo bar] (comma value must arrive space-joined)", out)
	}
	if !strings.Contains(out, "ARGS=[--check x.go]") {
		t.Errorf("child saw %q, want args passed through", out)
	}

	// With no extra env, the inherited environment passes through.
	t.Setenv("GRAALGO_BUILD_TAGS", "usertags")
	r2, w2, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w2
	code = graalgoRunEnv(root, nil, nil)
	w2.Close()
	os.Stdout = old
	buf = make([]byte, 4096)
	n, _ = r2.Read(buf)
	r2.Close()
	out = string(buf[:n])
	if code != 0 || !strings.Contains(out, "TAGS=[usertags]") {
		t.Errorf("graalgoRunEnv(nil env) = %d, output %q, want inherited TAGS=[usertags]", code, out)
	}
}
