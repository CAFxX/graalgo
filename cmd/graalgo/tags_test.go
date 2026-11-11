// Tests for A-P1-2 (REVIEW-2026-11-11-03): `-tags` was accepted by
// `graalgo run`/`build`/`test`, parsed, then silently dropped — the
// backend honors tags only via GRAALGO_BUILD_TAGS, which nothing ever
// set, and the driver's own file selection ignored constraints
// entirely. These tests pin both halves of the fix: the flag value
// reaches the backend child's environment (overriding any ambient
// value), and tag-gated files are selected the way `go` selects them.
package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// withEnv must replace a pre-existing entry, not append a second one:
// Go's Getenv returns the FIRST occurrence, so an appended duplicate
// would leave the old value in effect (A-P2-6's shape).
func TestWithEnvOverridesExistingKey(t *testing.T) {
	env := []string{"A=1", "GRAALGO_BUILD_TAGS=old", "B=2", "GRAALGO_BUILD_TAGS=older"}
	got := withEnv(env, "GRAALGO_BUILD_TAGS", "new")
	count := 0
	for _, e := range got {
		if e == "GRAALGO_BUILD_TAGS=new" {
			count++
		}
		if strings.HasPrefix(e, "GRAALGO_BUILD_TAGS=") && e != "GRAALGO_BUILD_TAGS=new" {
			t.Errorf("withEnv left stale entry %q in %v", e, got)
		}
	}
	if count != 1 {
		t.Errorf("withEnv produced %d GRAALGO_BUILD_TAGS=new entries, want 1: %v", count, got)
	}
	for _, want := range []string{"A=1", "B=2"} {
		found := false
		for _, e := range got {
			if e == want {
				found = true
			}
		}
		if !found {
			t.Errorf("withEnv dropped unrelated entry %q: %v", want, got)
		}
	}
}

func TestBuildTagsEnv(t *testing.T) {
	if got := buildTagsEnv(nil); got != nil {
		t.Errorf("buildTagsEnv(nil) = %v, want nil (no flag, no override)", got)
	}
	if got := buildTagsEnv([]string{}); got != nil {
		t.Errorf("buildTagsEnv(empty) = %v, want nil (no flag, no override)", got)
	}
	got := buildTagsEnv([]string{"foo", "bar"})
	if len(got) != 1 || got[0] != "GRAALGO_BUILD_TAGS=foo bar" {
		t.Errorf("buildTagsEnv([foo bar]) = %v, want [GRAALGO_BUILD_TAGS=foo bar]", got)
	}
}

// parseBuildFlags must keep the -tags value in both forms; the driver
// used to parse it and then never read it.
func TestParseBuildFlagsTags(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"separate-arg", []string{"-tags", "foo bar"}},
		{"equals-form", []string{"-tags=foo bar"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bf, err := parseBuildFlags(tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if bf.tags != "foo bar" {
				t.Errorf("bf.tags = %q, want %q", bf.tags, "foo bar")
			}
		})
	}
}

// writeTagGatedPkg creates a package with two alternative
// implementations of Who, gated by the "footag" build tag, plus a
// test asserting Who() == "foo".
func writeTagGatedPkg(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/tagged\n\ngo 1.21\n")
	writeFile(t, dir, "who_foo.go", "//go:build footag\n\npackage tagged\n\nfunc Who() string { return \"foo\" }\n")
	writeFile(t, dir, "who_plain.go", "//go:build !footag\n\npackage tagged\n\nfunc Who() string { return \"plain\" }\n")
	writeFile(t, dir, "who_test.go", "package tagged\n\nimport \"testing\"\n\nfunc TestWho(t *testing.T) {\n\tif Who() != \"foo\" {\n\t\tt.Errorf(\"Who() = %q, want %q\", Who(), \"foo\")\n\t}\n}\n")
	return dir
}

func baseNames(files []string) []string {
	var out []string
	for _, f := range files {
		out = append(out, filepath.Base(f))
	}
	return out
}

func containsAll(t *testing.T, got []string, want ...string) {
	t.Helper()
	set := map[string]bool{}
	for _, g := range got {
		set[g] = true
	}
	for _, w := range want {
		if !set[w] {
			t.Errorf("got %v, missing %q", got, w)
		}
	}
}

// buildableGoFiles must honor build tags: without the tag the !footag
// file wins; with it the footag file wins. Before the fix the function
// took no tags at all, so both alternatives were always returned.
func TestBuildableGoFilesTags(t *testing.T) {
	dir := writeTagGatedPkg(t)
	plain := baseNames(buildableGoFiles(dir, nil))
	containsAll(t, plain, "who_plain.go", "who_test.go")
	for _, f := range plain {
		if f == "who_foo.go" {
			t.Errorf("buildableGoFiles(no tags) = %v, must not contain who_foo.go", plain)
		}
	}
	tagged := baseNames(buildableGoFiles(dir, []string{"footag"}))
	containsAll(t, tagged, "who_foo.go", "who_test.go")
	for _, f := range tagged {
		if f == "who_plain.go" {
			t.Errorf("buildableGoFiles([footag]) = %v, must not contain who_plain.go", tagged)
		}
	}
}

// With explicit tags, a directory whose files are all excluded by the
// tags selects nothing — it must not fall back to all files (the
// fallback exists only for the no-tags "not importable" case).
func TestBuildableGoFilesTagsAllExcluded(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/onlytagged\n\ngo 1.21\n")
	writeFile(t, dir, "only_foo.go", "//go:build footag\n\npackage onlytagged\n\nfunc Who() string { return \"foo\" }\n")
	if got := buildableGoFiles(dir, []string{"othertag"}); len(got) != 0 {
		t.Errorf("buildableGoFiles([othertag]) = %v, want empty (all files excluded)", got)
	}
}

// fakeTagsBackendRoot is fakeBackendRoot with a script that records the
// backend child's environment (GRAALGO_BUILD_TAGS) and argv to $TAGS_LOG,
// and answers --emit-wire=FILE with an empty JSON object so `build`'s
// frontend gate succeeds.
func fakeTagsBackendRoot(t *testing.T) (root, logPath string) {
	t.Helper()
	logPath = filepath.Join(t.TempDir(), "tags.log")
	t.Setenv("TAGS_LOG", logPath)
	root = fakeBackendRoot(t, `echo "tags=${GRAALGO_BUILD_TAGS-unset}" >> "$TAGS_LOG"
echo "args: $*" >> "$TAGS_LOG"
for a in "$@"; do case "$a" in --emit-wire=*) echo '{}' > "${a#--emit-wire=}";; esac; done
exit 0`)
	return root, logPath
}

func readTagsLog(t *testing.T, logPath string) string {
	t.Helper()
	b, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading tags log: %v", err)
	}
	return string(b)
}

// `graalgo run -tags=footag` must forward the tags to the backend
// child's environment (single-file form) — before the fix the backend
// saw no GRAALGO_BUILD_TAGS at all.
func TestCmdRunForwardsTagsToBackend(t *testing.T) {
	root, logPath := fakeTagsBackendRoot(t)
	dir := writeTagGatedPkg(t)
	// The package dir also doubles as a main package for the run test:
	// give it a main file so the single-file form is exercised.
	writeFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")
	prog := filepath.Join(dir, "main.go")
	var code int
	captureOutput(t, func() { code = cmdRun(root, []string{"-tags=footag", prog}) })
	if code != 0 {
		t.Fatalf("cmdRun exit = %d, want 0", code)
	}
	mustContain(t, readTagsLog(t, logPath), "tags=footag")
}

// The package form of `run` must also forward tags, and must pass only
// the tag-selected files to the backend: with -tags=footag the backend
// is invoked (--check, then run) with who_foo.go and never who_plain.go.
func TestCmdRunPackageFormForwardsTagsAndFiltersFiles(t *testing.T) {
	root, logPath := fakeTagsBackendRoot(t)
	// A main package whose alternatives are tag-gated.
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/tagrun\n\ngo 1.21\n")
	writeFile(t, dir, "main_foo.go", "//go:build footag\n\npackage main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(\"foo\") }\n")
	writeFile(t, dir, "main_plain.go", "//go:build !footag\n\npackage main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(\"plain\") }\n")
	var code int
	captureOutput(t, func() { code = cmdRun(root, []string{"-tags=footag", dir}) })
	if code != 0 {
		t.Fatalf("cmdRun exit = %d, want 0", code)
	}
	log := readTagsLog(t, logPath)
	mustContain(t, log, "tags=footag")
	mustContain(t, log, "main_foo.go")
	mustNotContain(t, log, "main_plain.go")
}

// An explicit -tags must override an ambient GRAALGO_BUILD_TAGS for
// the backend child: Go's Getenv is first-wins, so appending would
// silently keep the ambient value.
func TestCmdRunTagsOverrideAmbient(t *testing.T) {
	root, logPath := fakeTagsBackendRoot(t)
	t.Setenv("GRAALGO_BUILD_TAGS", "ambient")
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")
	var code int
	captureOutput(t, func() { code = cmdRun(root, []string{"-tags=footag", filepath.Join(dir, "main.go")}) })
	if code != 0 {
		t.Fatalf("cmdRun exit = %d, want 0", code)
	}
	log := readTagsLog(t, logPath)
	mustContain(t, log, "tags=footag")
	mustNotContain(t, log, "tags=ambient")
}

// `graalgo build -tags=footag` must forward the tags to the frontend
// gate and pass only the tag-selected files.
func TestCmdBuildForwardsTagsToBackend(t *testing.T) {
	root, logPath := fakeTagsBackendRoot(t)
	// Emit compiles the launcher with the toolchain that built this
	// test binary; point it there explicitly.
	t.Setenv("GOROOT", runtime.GOROOT())
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/tagbuild\n\ngo 1.21\n")
	writeFile(t, dir, "main_foo.go", "//go:build footag\n\npackage main\n\nfunc main() {}\n")
	writeFile(t, dir, "main_plain.go", "//go:build !footag\n\npackage main\n\nfunc main() {}\n")
	out := filepath.Join(t.TempDir(), "tagbuild")
	var code int
	captureOutput(t, func() { code = cmdBuild(root, []string{"-tags=footag", "-o", out, dir}) })
	if code != 0 {
		t.Fatalf("cmdBuild exit = %d, want 0", code)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("cmdBuild produced no artifact at %s: %v", out, err)
	}
	log := readTagsLog(t, logPath)
	mustContain(t, log, "tags=footag")
	mustContain(t, log, "main_foo.go")
	mustNotContain(t, log, "main_plain.go")
}

// `graalgo test -tags=footag` must accept the flag (it used to reject
// it outright, F-P2-5), forward it to the backend, and select the
// tag-gated files for the synthesized driver.
func TestCmdTestAcceptsAndForwardsTags(t *testing.T) {
	root, logPath := fakeTagsBackendRoot(t)
	dir := writeTagGatedPkg(t)
	var code int
	out := captureOutput(t, func() { code = cmdTest(root, []string{"-tags=footag", dir}) })
	if code != 0 {
		t.Fatalf("cmdTest exit = %d, want 0:\n%s", code, out)
	}
	mustContain(t, readTagsLog(t, logPath), "tags=footag")
}

// The synthesized driver must inline the tag-selected implementation:
// with -tags=footag the driver contains Who() == "foo" and not the
// plain alternative; without tags it is the other way round. This is
// the "right one wins" assertion for the test path.
func TestSynthDriverTagsSelectRightFile(t *testing.T) {
	dir := writeTagGatedPkg(t)
	testFiles, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil || len(testFiles) == 0 {
		t.Fatal("no test files")
	}
	var ftList []fileTests
	for _, tf := range testFiles {
		tests, err := discoverTests(tf)
		if err != nil {
			t.Fatal(err)
		}
		if len(tests) > 0 {
			ftList = append(ftList, fileTests{file: tf, tests: tests})
		}
	}
	synth := func(tags []string) string {
		t.Helper()
		srcFiles := buildableGoFiles(dir, tags)
		var nonTest []string
		for _, f := range srcFiles {
			if !strings.HasSuffix(f, "_test.go") {
				nonTest = append(nonTest, f)
			}
		}
		driver, err := synthesizeTestDriver(dir, ftList, nonTest, false, tags)
		if err != nil {
			t.Fatalf("synthesizeTestDriver(%v): %v", tags, err)
		}
		defer os.Remove(driver)
		b, err := os.ReadFile(driver)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	tagged := synth([]string{"footag"})
	mustContain(t, tagged, `return "foo"`)
	mustNotContain(t, tagged, `return "plain"`)
	plain := synth(nil)
	mustContain(t, plain, `return "plain"`)
	mustNotContain(t, plain, `return "foo"`)
}

// End to end at the driver level: the synthesized driver for the
// tag-gated package, run under gc, passes when the tag selects the
// implementation the test expects — proving the selected file is the
// one that actually runs, not just the one named in the driver.
// Verbose mode, so the driver prints the per-test PASS lines (in
// non-verbose mode it prints nothing on success, like `go test`).
func TestSynthDriverTagsRunsSelectedFile(t *testing.T) {
	dir := writeTagGatedPkg(t)
	testFiles, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	var ftList []fileTests
	for _, tf := range testFiles {
		tests, err := discoverTests(tf)
		if err != nil {
			t.Fatal(err)
		}
		if len(tests) > 0 {
			ftList = append(ftList, fileTests{file: tf, tests: tests})
		}
	}
	srcFiles := buildableGoFiles(dir, []string{"footag"})
	var nonTest []string
	for _, f := range srcFiles {
		if !strings.HasSuffix(f, "_test.go") {
			nonTest = append(nonTest, f)
		}
	}
	importPkgNameCache = map[string]string{}
	driver, err := synthesizeTestDriver(dir, ftList, nonTest, true, []string{"footag"})
	if err != nil {
		t.Fatalf("synthesizeTestDriver: %v", err)
	}
	defer os.Remove(driver)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, goBinary(t), "run", driver)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run driver: %v\noutput:\n%s", err, out)
	}
	mustContain(t, string(out), "=== RUN   TestWho")
	mustContain(t, string(out), "--- PASS: TestWho")
}
