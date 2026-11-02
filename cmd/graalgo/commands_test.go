package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/CAFxX/graalgo/internal/vet"
)

// chdir changes the working directory for the test, restoring it on
// cleanup. t.Chdir needs go1.24 but this module declares go1.21, so
// tests use this helper (same pattern as os.Chdir in driver tests).
func chdir(t *testing.T, dir string) {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
}

// TestParseBuildFlagsTags: R01 — -tags must be retained by the parser in
// both -tags value and -tags=value forms; a dropped value here is the
// exact regression that silently built the wrong variant.
func TestParseBuildFlagsTags(t *testing.T) {
	bf, err := parseBuildFlags([]string{"-tags", "foo bar", "pkg"})
	if err != nil {
		t.Fatal(err)
	}
	if bf.tags != "foo bar" {
		t.Errorf("-tags value form: tags = %q, want %q", bf.tags, "foo bar")
	}
	if !reflect.DeepEqual(bf.pkgs, []string{"pkg"}) {
		t.Errorf("pkgs = %v, want [pkg]", bf.pkgs)
	}

	bf, err = parseBuildFlags([]string{"-tags=foo", "-o", "out"})
	if err != nil {
		t.Fatal(err)
	}
	if bf.tags != "foo" || bf.output != "out" {
		t.Errorf("-tags= form: tags = %q output = %q, want foo/out", bf.tags, bf.output)
	}
}

func TestParseBuildFlagsErrors(t *testing.T) {
	if _, err := parseBuildFlags([]string{"-tags"}); err == nil {
		t.Error("-tags without a value: got nil error, want missing-argument error")
	}
	if _, err := parseBuildFlags([]string{"-bogus"}); err == nil {
		t.Error("unknown flag: got nil error, want error")
	}
	if _, err := parseBuildFlags([]string{"-o"}); err == nil {
		t.Error("-o without a value: got nil error, want error")
	}
}

func TestParseBuildFlagsIgnored(t *testing.T) {
	bf, err := parseBuildFlags([]string{"-a", "-x", "-work", "-n", "pkg"})
	if err != nil {
		t.Fatalf("accepted diagnostic flags rejected: %v", err)
	}
	if !reflect.DeepEqual(bf.pkgs, []string{"pkg"}) {
		t.Errorf("pkgs = %v, want [pkg]", bf.pkgs)
	}
}

// TestBackendCheckArgsForwardsTags: R01 — the backend invocation built
// for a tagged build must carry the tags (via GRAALGO_BUILD_TAGS env)
// and the wire/check argv. This is the test the original bug would
// have failed: tags parsed but never forwarded.
func TestBackendCheckArgsForwardsTags(t *testing.T) {
	args, env := backendCheckArgs("foo bar", "/tmp/w.json", []string{"a.go", "b.go"})
	wantArgs := []string{"--emit-wire=/tmp/w.json", "--check", "a.go", "b.go"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("backend argv = %v, want %v", args, wantArgs)
	}
	wantEnv := []string{"GRAALGO_BUILD_TAGS=foo bar"}
	if !reflect.DeepEqual(env, wantEnv) {
		t.Errorf("backend env = %v, want %v", env, wantEnv)
	}

	args, env = backendCheckArgs("", "/tmp/w.json", []string{"a.go"})
	if env != nil {
		t.Errorf("untagged build: backend env = %v, want nil (no tags to forward)", env)
	}
	if !reflect.DeepEqual(args, []string{"--emit-wire=/tmp/w.json", "--check", "a.go"}) {
		t.Errorf("untagged build: backend argv = %v", args)
	}
}

func TestTagsEnv(t *testing.T) {
	if got := tagsEnv(""); got != nil {
		t.Errorf("tagsEnv(\"\") = %v, want nil", got)
	}
	if got := tagsEnv("foo"); !reflect.DeepEqual(got, []string{"GRAALGO_BUILD_TAGS=foo"}) {
		t.Errorf("tagsEnv(\"foo\") = %v", got)
	}
}

// TestFilterFilesByBuildTags: R01 — files gated by a tag not in -tags
// are excluded; files gated by a tag in -tags and unconstrained files
// are kept; a tag set excluding everything is a loud error.
func TestFilterFilesByBuildTags(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	plain := write("plain.go", "package p\n")
	gated := write("gated.go", "//go:build mytag\n\npackage p\n")
	other := write("other.go", "//go:build othertag\n\npackage p\n")

	kept, err := filterFilesByBuildTags([]string{plain, gated, other}, "mytag")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(kept, []string{plain, gated}) {
		t.Errorf("tags=mytag: kept = %v, want [plain gated]", kept)
	}

	kept, err = filterFilesByBuildTags([]string{plain, gated, other}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(kept) != 3 {
		t.Errorf("no tags: kept = %v, want all files unchanged", kept)
	}

	if _, err := filterFilesByBuildTags([]string{gated, other}, "nomatch"); err == nil {
		t.Error("tags excluding all files: got nil error, want loud error")
	}
}

// TestFilterFilesByBuildTagsCommaSeparated: VETO finding 1 (P0) —
// `go build -tags` accepts comma-separated tags, so "mytag,other" must
// select a //go:build mytag file, not treat the whole string as one
// tag and drop it in favour of its !mytag alternative.
func TestFilterFilesByBuildTagsCommaSeparated(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	gated := write("gated.go", "//go:build mytag\n\npackage p\n")
	alt := write("alt.go", "//go:build !mytag\n\npackage p\n")

	kept, err := filterFilesByBuildTags([]string{gated, alt}, "mytag,other")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(kept, []string{gated}) {
		t.Errorf("tags=mytag,other: kept = %v, want [gated]", kept)
	}
	// Mixed separators and surrounding whitespace must not matter.
	kept, err = filterFilesByBuildTags([]string{gated, alt}, "other, mytag")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(kept, []string{gated}) {
		t.Errorf("tags='other, mytag': kept = %v, want [gated]", kept)
	}
}

// TestFilterFilesByBuildTagsPurego: VETO finding 3 (P2) — the filter
// seeds purego like the frontend disk-import path, so a purego file is
// kept under any -tags value and its !purego alternative is dropped.
// Tagged-vs-untagged GOOS/GOARCH behaviour: with tags, go/build
// evaluates the full constraint set against the host (a file for
// another GOOS is dropped); without tags the list passes through
// unchanged, preserving the driver's historical untagged file set.
func TestFilterFilesByBuildTagsPurego(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	pure := write("pure.go", "//go:build purego\n\npackage p\n")
	asm := write("asm.go", "//go:build !purego\n\npackage p\n")
	otherGOOS := "windows"
	if otherGOOS == runtime.GOOS {
		otherGOOS = "linux"
	}
	foreign := write("foreign.go", "//go:build "+otherGOOS+"\n\npackage p\n")

	kept, err := filterFilesByBuildTags([]string{pure, asm}, "mytag")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(kept, []string{pure}) {
		t.Errorf("tags=mytag: kept = %v, want [pure] (purego seeded)", kept)
	}

	kept, err = filterFilesByBuildTags([]string{pure, asm, foreign}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(kept) != 3 {
		t.Errorf("no tags: kept = %v, want all files unchanged", kept)
	}

	kept, err = filterFilesByBuildTags([]string{pure, foreign}, "mytag")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(kept, []string{pure}) {
		t.Errorf("tags=mytag: kept = %v, want [pure] (foreign-GOOS file dropped)", kept)
	}
}

// TestGroupFilesByPackage: R02 — a package and its external test
// package must form separate groups; internal test files in the same
// package stay with it.
func TestGroupFilesByPackage(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	a := write("a.go", "package foo\n")
	b := write("b.go", "package foo\n")
	ext := write("c_test.go", "package foo_test\n")
	in := write("d_test.go", "package foo\n")

	groups, err := groupFilesByPackage([]string{a, b, ext, in})
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2 (foo, foo_test)", len(groups))
	}
	if !reflect.DeepEqual(groups[0], []string{a, b, in}) {
		t.Errorf("group 0 = %v, want [a b d_test]", groups[0])
	}
	if !reflect.DeepEqual(groups[1], []string{ext}) {
		t.Errorf("group 1 = %v, want [c_test]", groups[1])
	}
}

// writeVetImportFixture creates the standard external-test layout: a
// package foo plus an external test package foo_test that imports foo
// by its module path. This is the layout the VETO finding 1 (P1) is
// about: checked alone with importer.Default(), the foo_test group
// cannot resolve the module-path import (no compiled export data for
// not-yet-compiled source) and fails type checking.
func writeVetImportFixture(t *testing.T) (dir, foo, ext string) {
	t.Helper()
	dir = t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/foomod\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	foo = write("foo.go", "package foo\n\n// Hello is imported by the external test package.\nfunc Hello() string { return \"hi\" }\n")
	ext = write("foo_test.go", "package foo_test\n\nimport \"example.com/foomod\"\n\nvar _ = foo.Hello\n")
	return dir, foo, ext
}

// TestRunVetGroupsImportingExternalTest: VETO finding 2 (P1) — a clean
// directory whose external tests import their own package must vet
// clean. The foo_test group alone with importer.Default() fails (that
// failure is the old behaviour, asserted here to document why the
// chained importer exists); runVetGroups, which checks foo first and
// resolves the import to the already-checked package, must be clean
// with no degraded diagnostics.
func TestRunVetGroupsImportingExternalTest(t *testing.T) {
	_, foo, ext := writeVetImportFixture(t)

	if _, err := vet.Run([]string{ext}); err == nil {
		t.Error("foo_test alone with importer.Default(): got nil error, want a type-checking failure (no export data for the module-path import)")
	}

	groups, err := groupFilesByPackage([]string{foo, ext})
	if err != nil {
		t.Fatal(err)
	}
	marked, degraded := runVetGroups(groups)
	if degraded {
		t.Error("runVetGroups: degraded = true, want false for a clean importing layout")
	}
	for _, m := range marked {
		if m.degraded {
			t.Errorf("runVetGroups: diagnostic %v marked degraded, want clean", m.d)
		}
	}
	if len(marked) != 0 {
		t.Errorf("runVetGroups: diagnostics = %v, want none", marked)
	}
}

// TestRunVetGroupsTestGroupFirst: the chained importer must not depend
// on group order — a test group listed before its package still
// resolves, because runVetGroups checks non-test groups first.
func TestRunVetGroupsTestGroupFirst(t *testing.T) {
	_, foo, ext := writeVetImportFixture(t)

	groups, err := groupFilesByPackage([]string{ext, foo})
	if err != nil {
		t.Fatal(err)
	}
	marked, degraded := runVetGroups(groups)
	if degraded || len(marked) != 0 {
		t.Errorf("runVetGroups (test group first): degraded = %v, diagnostics = %v, want false/none", degraded, marked)
	}
}

// TestRunVetGroupsDegradedMarked: VETO finding 6 (P2) — diagnostics
// from a group whose type checking failed are marked degraded, and the
// run reports degraded, so they are never presented as clean vet output.
func TestRunVetGroupsDegradedMarked(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.go")
	// A type error (undefined identifier) plus a printf call the
	// analyzer would normally flag: the point is the marking, not the
	// specific diagnostics.
	if err := os.WriteFile(bad, []byte("package p\n\nimport \"fmt\"\n\nfunc F() {\n\tfmt.Printf(\"%d\", undefinedThing)\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	groups, err := groupFilesByPackage([]string{bad})
	if err != nil {
		t.Fatal(err)
	}
	marked, degraded := runVetGroups(groups)
	if !degraded {
		t.Error("runVetGroups: degraded = false, want true for a type-checking failure")
	}
	for _, m := range marked {
		if !m.degraded {
			t.Errorf("runVetGroups: diagnostic %v unmarked, want degraded", m.d)
		}
	}
}

// fakeBackendRoot creates a graalgo installation root whose backend
// runner is the given shell script (the fake-backend seam): cmdVet /
// cmdBuild / cmdRun invoke root/internal/backend/graalgo-sync-run, so a
// test root with a script there exercises the driver wiring without a
// JVM backend.
func fakeBackendRoot(t *testing.T, script string) string {
	t.Helper()
	root := t.TempDir()
	runner := filepath.Join(root, "internal", "backend", "graalgo-sync-run")
	if err := os.MkdirAll(filepath.Dir(runner), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runner, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

// captureStdout runs f with os.Stdout redirected to a pipe and returns
// what f printed. cmdVet/cmdBuild print verdicts to stdout; tests
// assert on the verdict, not on test-log noise.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	f()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = old
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// TestGraalgoRunEnvDriverValueWins: VETO finding 4 (P2) — the env-dedup
// seam. graalgoRunEnv appends the driver value after the inherited
// environment, so with a user-exported GRAALGO_BUILD_TAGS present, the
// backend must still see the driver-supplied value (Go exec keeps the
// last duplicate key).
func TestGraalgoRunEnvDriverValueWins(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "tags.txt")
	root := fakeBackendRoot(t, fmt.Sprintf("#!/bin/sh\necho \"$GRAALGO_BUILD_TAGS\" > '%s'\n", marker))
	t.Setenv("GRAALGO_BUILD_TAGS", "user-value")
	if code := graalgoRunEnv(root, []string{"--check", "x.go"}, []string{"GRAALGO_BUILD_TAGS=driver-value"}); code != 0 {
		t.Fatalf("graalgoRunEnv: exit %d, want 0", code)
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(data)); got != "driver-value" {
		t.Errorf("backend saw GRAALGO_BUILD_TAGS=%q, want %q", got, "driver-value")
	}
}

// TestCmdVetFakeBackend: VETO finding 4 (P2) — pins the cmdVet wiring
// end to end (backend gate, then grouped analysis, then verdict).
// Degraded type information exits 1 and never prints a clean verdict;
// a clean directory (including the importing external-test layout)
// exits 0 with "no issues found".
func TestCmdVetFakeBackend(t *testing.T) {
	root := fakeBackendRoot(t, "#!/bin/sh\nexit 0\n")

	badDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(badDir, "bad.go"), []byte("package p\n\nfunc F() {\n\tvar _ = undefinedThing\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var code int
	out := captureStdout(t, func() { code = cmdVet(root, []string{badDir}) })
	if code != 1 {
		t.Errorf("cmdVet degraded: exit %d, want 1", code)
	}
	if strings.Contains(out, "no issues found") {
		t.Errorf("cmdVet degraded: printed a clean verdict: %q", out)
	}

	goodDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(goodDir, "good.go"), []byte("package p\n\nfunc F() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out = captureStdout(t, func() { code = cmdVet(root, []string{goodDir}) })
	if code != 0 {
		t.Errorf("cmdVet clean: exit %d, want 0", code)
	}
	if !strings.Contains(out, "vet: no issues found") {
		t.Errorf("cmdVet clean: stdout %q, want a clean verdict", out)
	}

	fixtureDir, _, _ := writeVetImportFixture(t)
	out = captureStdout(t, func() { code = cmdVet(root, []string{fixtureDir}) })
	if code != 0 {
		t.Errorf("cmdVet importing external-test layout: exit %d, want 0", code)
	}
	if !strings.Contains(out, "vet: no issues found") {
		t.Errorf("cmdVet importing external-test layout: stdout %q, want a clean verdict", out)
	}
}

// TestCmdBuildTaggedCarriesEnv: VETO finding 4 (P2) — a tagged cmdBuild
// invocation carries the driver tags to the backend even when the user
// exported a different GRAALGO_BUILD_TAGS, and the fake backend's wire
// output still flows through Emit to a real binary.
func TestCmdBuildTaggedCarriesEnv(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "tags.txt")
	// Minimal frontend/v1 wire document (the shape Emit validates).
	wireJSON := `{"frontend":"v1","generator":{"tool":"gofrontend","version":"0.7.0"},"package":{"name":"main","files":["main.go"]}}`
	script := fmt.Sprintf("#!/bin/sh\necho \"$GRAALGO_BUILD_TAGS\" > '%s'\nfor a in \"$@\"; do case \"$a\" in --emit-wire=*) p=${a#--emit-wire=};; esac; done\nprintf '%%s' '%s' > \"$p\"\n", marker, wireJSON)
	root := fakeBackendRoot(t, script)
	t.Setenv("GRAALGO_BUILD_TAGS", "user-value")

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "prog")
	if code := cmdBuild(root, []string{"-tags", "mytag,other", "-o", out, dir}); code != 0 {
		t.Fatalf("cmdBuild tagged: exit %d, want 0", code)
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(data)); got != "mytag,other" {
		t.Errorf("backend saw GRAALGO_BUILD_TAGS=%q, want %q", got, "mytag,other")
	}
	if fi, err := os.Stat(out); err != nil || fi.Mode()&0o111 == 0 {
		t.Errorf("cmdBuild tagged: output %q not an executable binary: %v", out, err)
	}
}

func TestGroupFilesByPackageParseError(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.go")
	if err := os.WriteFile(p, []byte("this is not go"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := groupFilesByPackage([]string{p}); err == nil {
		t.Error("unparsable file: got nil error, want loud error")
	}
}

func TestResolveFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "c.go"), []byte("package sub\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	chdir(t, dir)
	files, err := resolveFiles(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != "a.go" {
		t.Errorf("no-arg resolve = %v, want [a.go]", files)
	}
	files, err = resolveFiles([]string{"sub"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || !strings.HasSuffix(files[0], "c.go") {
		t.Errorf("dir resolve = %v, want [sub/c.go]", files)
	}
	files, err = resolveFiles([]string{"missing.go"})
	if err != nil || len(files) != 1 {
		t.Errorf("missing .go passthrough = %v, %v, want [missing.go] nil", files, err)
	}
	if _, err := resolveFiles([]string{"missing-dir"}); err == nil {
		t.Error("missing non-go path: got nil error, want error")
	}
	if _, err := resolveFiles([]string{"./..."}); err == nil {
		t.Error("./...: got nil error, want not-yet-supported error")
	}
	empty := t.TempDir()
	chdir(t, empty)
	if _, err := resolveFiles(nil); err == nil {
		t.Error("empty dir, no args: got nil error, want error")
	}
}

func TestFindGraalgoRoot(t *testing.T) {
	// Success via GRAALGO_HOME.
	root := t.TempDir()
	runner := filepath.Join(root, "internal", "backend", "graalgo-sync-run")
	if err := os.MkdirAll(filepath.Dir(runner), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runner, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRAALGO_HOME", root)
	got, err := findGraalgoRoot()
	if err != nil || got != root {
		t.Errorf("GRAALGO_HOME root: got %q, %v, want %q nil", got, err, root)
	}

	// GRAALGO_HOME pointing at a non-installation is a loud error.
	t.Setenv("GRAALGO_HOME", t.TempDir())
	if _, err := findGraalgoRoot(); err == nil {
		t.Error("GRAALGO_HOME non-root: got nil error, want error")
	}

	// No GRAALGO_HOME and no installation above cwd or the test
	// binary: root discovery must fail loudly, not return "".
	t.Setenv("GRAALGO_HOME", "")
	chdir(t, t.TempDir())
	if _, err := findGraalgoRoot(); err == nil {
		t.Error("no installation discoverable: got nil error, want error")
	}
}

// TestDispatchErrorPaths: R23 — build/run/vet/fmt must fail loudly on
// bad flags and unresolvable operands without ever reaching the
// backend (root is bogus on purpose: any backend call would fail
// differently, so these tests prove validation happens first).
func TestDispatchErrorPaths(t *testing.T) {
	const bogusRoot = "/nonexistent-graalgo-root"
	chdir(t, t.TempDir())

	cases := []struct {
		name string
		code int
	}{
		{"build unknown flag", cmdBuild(bogusRoot, []string{"-bogus"})},
		{"build -ldflags", cmdBuild(bogusRoot, []string{"-ldflags", "-X=x"})},
		{"build -o -", cmdBuild(bogusRoot, []string{"-o", "-"})},
		{"build missing dir", cmdBuild(bogusRoot, []string{"./no-such-dir"})},
		{"build -tags no files", cmdBuild(bogusRoot, []string{"-tags", "foo"})},
		{"run no files", cmdRun(bogusRoot, nil)},
		{"run unknown flag", cmdRun(bogusRoot, []string{"-bogus"})},
		{"run -ldflags", cmdRun(bogusRoot, []string{"-ldflags", "x"})},
		{"run -o", cmdRun(bogusRoot, []string{"-o", "out", "x.go"})},
		{"vet missing dir", cmdVet(bogusRoot, []string{"./no-such-dir"})},
		{"fmt missing dir", cmdFmt(bogusRoot, []string{"./no-such-dir"})},
	}
	for _, tc := range cases {
		if tc.code == 0 {
			t.Errorf("%s: exit 0, want non-zero", tc.name)
		}
	}
	// Usage errors are specifically exit 2 for build flag misuse.
	if code := cmdBuild(bogusRoot, []string{"-bogus"}); code != 2 {
		t.Errorf("build unknown flag: exit %d, want 2", code)
	}
	if code := cmdRun(bogusRoot, nil); code != 2 {
		t.Errorf("run no files: exit %d, want 2", code)
	}
}
