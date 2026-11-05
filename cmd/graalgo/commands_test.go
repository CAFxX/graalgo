package main

import (
	"go/build"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// chdir changes the working directory for the duration of the test
// (t.Chdir needs go1.24; this module is go1.21).
func chdir(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })
}

// writeTestFile writes content to dir/name, creating parent directories.
func writeTestFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// fakeGraalgoRoot creates a fake graalgo installation whose launcher is a
// shell script recording every invocation (argv and GRAALGO_BUILD_TAGS)
// to a log file and exiting 0. For --emit-wire=PATH it writes a minimal
// wire document so cmdBuild can proceed to artifact emission.
func fakeGraalgoRoot(t *testing.T) (root, logPath string) {
	t.Helper()
	root = t.TempDir()
	launcherDir := filepath.Join(root, "internal", "backend")
	if err := os.MkdirAll(launcherDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath = filepath.Join(root, "invocations.log")
	script := "#!/bin/sh\n" +
		"echo \"ARGS: $@\" >> \"$FAKE_LOG\"\n" +
		"echo \"TAGS: ${GRAALGO_BUILD_TAGS-<unset>}\" >> \"$FAKE_LOG\"\n" +
		"for a in \"$@\"; do case \"$a\" in --emit-wire=*) printf '{}' > \"${a#--emit-wire=}\";; esac; done\n" +
		"exit 0\n"
	if err := os.WriteFile(filepath.Join(launcherDir, "graalgo-sync-run"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_LOG", logPath)
	return root, logPath
}

// readLog returns the fake launcher's invocation log, one entry per line.
func readLog(t *testing.T, logPath string) []string {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}

// captureOutput runs fn with os.Stdout/os.Stderr redirected to buffers
// and returns what fn printed. The driver commands write diagnostics
// directly to os.Stdout/os.Stderr, so tests capture them this way.
func captureOutput(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = wOut, wErr
	defer func() {
		os.Stdout, os.Stderr = oldOut, oldErr
	}()
	fn()
	wOut.Close()
	wErr.Close()
	out, _ := io.ReadAll(rOut)
	errOut, _ := io.ReadAll(rErr)
	return string(out), string(errOut)
}

func TestParseBuildFlags(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		want   buildFlags
		wantOK bool
	}{
		{"o space", []string{"-o", "out"}, buildFlags{output: "out"}, true},
		{"o equals", []string{"-o=out"}, buildFlags{output: "out"}, true},
		{"tags space", []string{"-tags", "foo bar"}, buildFlags{tags: "foo bar"}, true},
		{"tags equals", []string{"-tags=foo,bar"}, buildFlags{tags: "foo,bar"}, true},
		{"ldflags", []string{"-ldflags", "-X=x"}, buildFlags{ldflags: "-X=x"}, true},
		{"bool flags", []string{"-a", "-x", "pkg"}, buildFlags{pkgs: []string{"pkg"}}, true},
		{"pkgs", []string{"a.go", "b.go"}, buildFlags{pkgs: []string{"a.go", "b.go"}}, true},
		{"unknown flag", []string{"-bogus"}, buildFlags{}, false},
		{"o missing arg", []string{"-o"}, buildFlags{}, false},
		{"tags missing arg", []string{"-tags"}, buildFlags{}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseBuildFlags(tc.args)
			if tc.wantOK && err != nil {
				t.Fatalf("parseBuildFlags(%q) error: %v", tc.args, err)
			}
			if !tc.wantOK && err == nil {
				t.Fatalf("parseBuildFlags(%q) succeeded, want error", tc.args)
			}
			if !tc.wantOK {
				return
			}
			if got.output != tc.want.output || got.ldflags != tc.want.ldflags || got.tags != tc.want.tags {
				t.Fatalf("parseBuildFlags(%q) = %+v, want %+v", tc.args, got, tc.want)
			}
			if len(got.pkgs) != len(tc.want.pkgs) {
				t.Fatalf("parseBuildFlags(%q) pkgs = %v, want %v", tc.args, got.pkgs, tc.want.pkgs)
			}
			for i := range got.pkgs {
				if got.pkgs[i] != tc.want.pkgs[i] {
					t.Fatalf("parseBuildFlags(%q) pkgs = %v, want %v", tc.args, got.pkgs, tc.want.pkgs)
				}
			}
		})
	}
}

func TestSplitTags(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"foo bar", []string{"foo", "bar"}},
		{"foo,bar", []string{"foo", "bar"}},
		{"foo, bar\tbaz", []string{"foo", "bar", "baz"}},
		{"foo", []string{"foo"}},
		{"", nil},
	}
	for _, tc := range cases {
		got := splitTags(tc.in)
		if len(got) != len(tc.want) {
			t.Fatalf("splitTags(%q) = %v, want %v", tc.in, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("splitTags(%q) = %v, want %v", tc.in, got, tc.want)
			}
		}
	}
}

func TestBuildContextTags(t *testing.T) {
	ctxt := buildContext("foo bar")
	found := map[string]bool{}
	for _, tag := range ctxt.BuildTags {
		found[tag] = true
	}
	if !found["foo"] || !found["bar"] {
		t.Fatalf("buildContext(%q).BuildTags = %v, want foo and bar", "foo bar", ctxt.BuildTags)
	}
	if ctxt.GOOS != build.Default.GOOS || ctxt.GOARCH != build.Default.GOARCH {
		t.Fatalf("buildContext changed GOOS/GOARCH: %s/%s", ctxt.GOOS, ctxt.GOARCH)
	}
}

func TestResolveFilesBuildConstraints(t *testing.T) {
	dir := t.TempDir()
	// Filename-suffix alternatives: only the linux file is buildable.
	writeTestFile(t, dir, "x_linux.go", "package p\n\nfunc Helper() int { return 1 }\n")
	writeTestFile(t, dir, "x_darwin.go", "package p\n\nfunc Helper() int { return 2 }\n")
	files, err := resolveFiles([]string{dir}, build.Default)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || filepath.Base(files[0]) != "x_linux.go" {
		t.Fatalf("resolveFiles(%q) = %v, want only x_linux.go", dir, files)
	}

	// //go:build alternatives: the tag selects the file.
	writeTestFile(t, dir, "a.go", "//go:build !foo\n\npackage p\n\nfunc A() int { return 1 }\n")
	writeTestFile(t, dir, "b.go", "//go:build foo\n\npackage p\n\nfunc B() int { return 2 }\n")
	files, err = resolveFiles([]string{dir}, build.Default)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, f := range files {
		names = append(names, filepath.Base(f))
	}
	// x_linux.go, a.go (x_darwin.go and b.go excluded; go/build sorts
	// GoFiles alphabetically).
	if len(names) != 2 || names[0] != "a.go" || names[1] != "x_linux.go" {
		t.Fatalf("resolveFiles without tags = %v, want [a.go x_linux.go]", names)
	}
	files, err = resolveFiles([]string{dir}, buildContext("foo"))
	if err != nil {
		t.Fatal(err)
	}
	names = nil
	for _, f := range files {
		names = append(names, filepath.Base(f))
	}
	if len(names) != 2 || names[0] != "b.go" || names[1] != "x_linux.go" {
		t.Fatalf("resolveFiles with -tags foo = %v, want [b.go x_linux.go]", names)
	}
}

func TestResolveFilesExplicitFilesPassThrough(t *testing.T) {
	// Explicit .go file lists are used exactly as given, like
	// `go run file.go`: constraints do not filter them, and a missing
	// file is passed through for the backend to report.
	dir := t.TempDir()
	p := writeTestFile(t, dir, "x_darwin.go", "package main\n\nfunc main() {}\n")
	files, err := resolveFiles([]string{p}, build.Default)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != p {
		t.Fatalf("resolveFiles(%q) = %v, want [%q]", p, files, p)
	}
	files, err = resolveFiles([]string{"missing.go"}, build.Default)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != "missing.go" {
		t.Fatalf("resolveFiles(missing.go) = %v, want [missing.go]", files)
	}
}

func TestResolveFilesImportPath(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "go.mod", "module example.com/m\n\ngo 1.21\n")
	sub := filepath.Join(root, "sub")
	writeTestFile(t, sub, "a.go", "package sub\n\nfunc A() {}\n")
	chdir(t, root)
	files, err := resolveFiles([]string{"example.com/m/sub"}, build.Default)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != filepath.Join(sub, "a.go") {
		t.Fatalf("resolveFiles(import path) = %v, want [%s]", files, filepath.Join(sub, "a.go"))
	}
	if _, err := resolveFiles([]string{"example.com/m/nope"}, build.Default); err == nil {
		t.Fatal("resolveFiles(unknown import path) succeeded, want error")
	}
	if _, err := resolveFiles([]string{"nosuchdir"}, build.Default); err == nil {
		t.Fatal("resolveFiles(unknown path) succeeded, want error")
	}
}

func TestResolveFilesNoArgs(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "x_linux.go", "package main\n\nfunc main() {}\n")
	writeTestFile(t, dir, "x_darwin.go", "package main\n\nfunc Helper() int { return 2 }\n")
	chdir(t, dir)
	files, err := resolveFiles(nil, build.Default)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || filepath.Base(files[0]) != "x_linux.go" {
		t.Fatalf("resolveFiles(nil) = %v, want only x_linux.go", files)
	}
}

func TestResolveFilesNoBuildableFiles(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "x_darwin.go", "package main\n\nfunc main() {}\n")
	if _, err := resolveFiles([]string{dir}, build.Default); err == nil {
		t.Fatal("resolveFiles(darwin-only dir on linux) succeeded, want error")
	}
}

func TestBuildableGoFiles(t *testing.T) {
	// Two packages in one directory: a hard error, not a fallback to
	// the unfiltered *.go list.
	dir := t.TempDir()
	writeTestFile(t, dir, "a.go", "package a\n\nfunc A() {}\n")
	writeTestFile(t, dir, "b.go", "package b\n\nfunc B() {}\n")
	if _, err := buildableGoFiles(dir); err == nil {
		t.Fatal("buildableGoFiles(two packages) succeeded, want error")
	}

	// No buildable files (every file excluded by constraints): empty,
	// not an error — the test runner reports [no test files].
	donly := t.TempDir()
	writeTestFile(t, donly, "x_darwin.go", "package p\n\nfunc D() {}\n")
	files, err := buildableGoFiles(donly)
	if err != nil {
		t.Fatalf("buildableGoFiles(darwin-only) error: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("buildableGoFiles(darwin-only) = %v, want empty", files)
	}

	// Normal package: GoFiles plus test files.
	pkg := t.TempDir()
	writeTestFile(t, pkg, "a.go", "package p\n\nfunc A() {}\n")
	writeTestFile(t, pkg, "a_test.go", "package p\n\nfunc TestA() {}\n")
	files, err = buildableGoFiles(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("buildableGoFiles = %v, want 2 files", files)
	}
}

func TestSplitVetFiles(t *testing.T) {
	dir := t.TempDir()
	a := writeTestFile(t, dir, "a.go", "package main\n\nfunc main() {}\n")
	b := writeTestFile(t, dir, "b.go", "package main\n\nfunc Helper() int { return 1 }\n")
	wt := writeTestFile(t, dir, "a_test.go", "package main\n\nfunc TestHelper() {}\n")
	xt := writeTestFile(t, dir, "b_test.go", "package main_test\n\nfunc TestExternal() {}\n")
	check, external := splitVetFiles([]string{a, b, wt, xt})
	if len(check) != 3 || check[0] != a || check[1] != b || check[2] != wt {
		t.Fatalf("splitVetFiles check = %v, want [a.go b.go a_test.go]", check)
	}
	if len(external) != 1 || external[0] != xt {
		t.Fatalf("splitVetFiles external = %v, want [b_test.go]", external)
	}
}

func TestPackageClause(t *testing.T) {
	dir := t.TempDir()
	p := writeTestFile(t, dir, "a.go", "package foo\n\nfunc A() {}\n")
	name, err := packageClause(p)
	if err != nil || name != "foo" {
		t.Fatalf("packageClause = %q, %v, want foo, nil", name, err)
	}
	if _, err := packageClause(filepath.Join(dir, "missing.go")); err == nil {
		t.Fatal("packageClause(missing) succeeded, want error")
	}
}

func TestCmdVetSingleCheck(t *testing.T) {
	root, logPath := fakeGraalgoRoot(t)
	dir := t.TempDir()
	// Multi-file package with a cross-file reference (C-P1-B): the
	// frontend must see both files in one invocation.
	a := writeTestFile(t, dir, "a.go", "package main\n\nfunc main() { println(helper()) }\n")
	b := writeTestFile(t, dir, "b.go", "package main\n\nfunc helper() int { return 42 }\n")
	wt := writeTestFile(t, dir, "a_test.go", "package main\n\nimport \"testing\"\n\nfunc TestHelper(t *testing.T) { if helper() != 42 { t.Fail() } }\n")
	writeTestFile(t, dir, "b_test.go", "package main_test\n\nimport \"testing\"\n\nfunc TestExternal(t *testing.T) {}\n")
	var code int
	out, _ := captureOutput(t, func() { code = cmdVet(root, []string{dir}) })
	if code != 0 {
		t.Fatalf("cmdVet = %d, want 0 (output %q)", code, out)
	}
	if !strings.Contains(out, "vet: no issues found") {
		t.Fatalf("cmdVet output %q, want 'vet: no issues found'", out)
	}
	lines := readLog(t, logPath)
	if len(lines) != 2 {
		t.Fatalf("launcher log = %v, want exactly one --check invocation (2 lines)", lines)
	}
	argsLine := lines[0]
	for _, want := range []string{"--check", a, b, wt} {
		if !strings.Contains(argsLine, want) {
			t.Fatalf("launcher args %q, want it to contain %q", argsLine, want)
		}
	}
	if strings.Contains(argsLine, "b_test.go") {
		t.Fatalf("launcher args %q, want external test file b_test.go excluded", argsLine)
	}
}

func TestCmdVetTagsForwarded(t *testing.T) {
	root, logPath := fakeGraalgoRoot(t)
	dir := t.TempDir()
	writeTestFile(t, dir, "a.go", "//go:build !foo\n\npackage main\n\nfunc main() { println(\"plain\") }\n")
	b := writeTestFile(t, dir, "b.go", "//go:build foo\n\npackage main\n\nfunc main() { println(\"foo\") }\n")
	var code int
	captureOutput(t, func() { code = cmdVet(root, []string{"-tags", "foo", dir}) })
	if code != 0 {
		t.Fatalf("cmdVet -tags foo = %d, want 0", code)
	}
	lines := readLog(t, logPath)
	if len(lines) != 2 {
		t.Fatalf("launcher log = %v, want exactly one invocation (2 lines)", lines)
	}
	if !strings.Contains(lines[0], b) || strings.Contains(lines[0], "a.go") {
		t.Fatalf("launcher args %q, want only b.go (tag foo selected)", lines[0])
	}
	if lines[1] != "TAGS: foo" {
		t.Fatalf("launcher env %q, want TAGS: foo", lines[1])
	}
}

func TestCmdBuildTagsForwarded(t *testing.T) {
	root, logPath := fakeGraalgoRoot(t)
	// The launcher compile inside driver.Emit needs a writable
	// TMPDIR; keep it inside the test temp dir. (GOCACHE is left
	// alone so the compile reuses the ambient cache.)
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	dir := t.TempDir()
	writeTestFile(t, dir, "a.go", "//go:build !foo\n\npackage main\n\nfunc main() { println(\"plain\") }\n")
	b := writeTestFile(t, dir, "b.go", "//go:build foo\n\npackage main\n\nfunc main() { println(\"foo\") }\n")
	out := filepath.Join(tmp, "myprog")
	var code int
	captureOutput(t, func() { code = cmdBuild(root, []string{"-tags", "foo", "-o", out, dir}) })
	if code != 0 {
		t.Fatalf("cmdBuild -tags foo = %d, want 0", code)
	}
	if fi, err := os.Stat(out); err != nil || fi.Mode()&0o111 == 0 {
		t.Fatalf("artifact %s missing or not executable: %v", out, err)
	}
	lines := readLog(t, logPath)
	if len(lines) != 2 {
		t.Fatalf("launcher log = %v, want exactly one invocation (2 lines)", lines)
	}
	if !strings.Contains(lines[0], "--emit-wire=") || !strings.Contains(lines[0], b) || strings.Contains(lines[0], "a.go") {
		t.Fatalf("launcher args %q, want --emit-wire with only b.go", lines[0])
	}
	if lines[1] != "TAGS: foo" {
		t.Fatalf("launcher env %q, want TAGS: foo", lines[1])
	}
}

func TestCmdRunTagsForwarded(t *testing.T) {
	root, logPath := fakeGraalgoRoot(t)
	dir := t.TempDir()
	a := writeTestFile(t, dir, "a.go", "package main\n\nfunc main() { println(\"hi\") }\n")
	var code int
	// Package form: a --check gate plus the program run, both tagged.
	captureOutput(t, func() { code = cmdRun(root, []string{"-tags", "foo", dir}) })
	if code != 0 {
		t.Fatalf("cmdRun -tags foo (package) = %d, want 0", code)
	}
	lines := readLog(t, logPath)
	if len(lines) != 4 {
		t.Fatalf("launcher log = %v, want two invocations (4 lines)", lines)
	}
	if !strings.Contains(lines[0], "--check") || !strings.Contains(lines[0], a) {
		t.Fatalf("first invocation %q, want --check with a.go", lines[0])
	}
	if lines[1] != "TAGS: foo" || lines[3] != "TAGS: foo" {
		t.Fatalf("launcher env lines = %q %q, want TAGS: foo on both", lines[1], lines[3])
	}

	// Explicit-file form: a single program run, still tagged.
	if err := os.WriteFile(logPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	captureOutput(t, func() { code = cmdRun(root, []string{"-tags", "foo", a}) })
	if code != 0 {
		t.Fatalf("cmdRun -tags foo (file) = %d, want 0", code)
	}
	lines = readLog(t, logPath)
	if len(lines) != 2 {
		t.Fatalf("launcher log = %v, want one invocation (2 lines)", lines)
	}
	if lines[1] != "TAGS: foo" {
		t.Fatalf("launcher env %q, want TAGS: foo", lines[1])
	}
}

func TestHelpFor(t *testing.T) {
	var code int
	captureOutput(t, func() { code = helpFor("build") })
	if code != 0 {
		t.Fatalf("helpFor(build) = %d, want 0", code)
	}
	captureOutput(t, func() { code = helpFor("bogus") })
	if code != 2 {
		t.Fatalf("helpFor(bogus) = %d, want 2", code)
	}
}

func TestParseFmtFlags(t *testing.T) {
	opts, rest, err := parseFmtFlags([]string{"-l", "-d", "pkg"})
	if err != nil || !opts.list || !opts.diff || len(rest) != 1 || rest[0] != "pkg" {
		t.Fatalf("parseFmtFlags(-l -d pkg) = %+v %v %v", opts, rest, err)
	}
	opts, _, err = parseFmtFlags([]string{"-l=false"})
	if err != nil || opts.list {
		t.Fatalf("parseFmtFlags(-l=false) = %+v %v", opts, err)
	}
	if _, _, err := parseFmtFlags([]string{"-bogus"}); err == nil {
		t.Fatal("parseFmtFlags(-bogus) succeeded, want error")
	}
	opts, rest, err = parseFmtFlags([]string{"-s", "-e", "-w"})
	if err != nil || !opts.simplify || !opts.showErrs || len(rest) != 0 {
		t.Fatalf("parseFmtFlags(-s -e -w) = %+v %v %v", opts, rest, err)
	}
}

func TestCmdFmt(t *testing.T) {
	if findGofmt() == "" {
		t.Skip("gofmt not found")
	}
	dir := t.TempDir()
	bad := "package main\n\nfunc main() { println( \"hi\" ) }\n"
	good := "package main\n\nfunc main() { println(\"hi\") }\n"
	p := writeTestFile(t, dir, "a.go", bad)

	// -l lists the file without rewriting it.
	out, _ := captureOutput(t, func() {
		if code := cmdFmt("", []string{"-l", dir}); code != 0 {
			t.Errorf("cmdFmt -l = %d, want 0", code)
		}
	})
	if !strings.Contains(out, "a.go") {
		t.Fatalf("cmdFmt -l output %q, want it to list a.go", out)
	}
	if got, _ := os.ReadFile(p); string(got) != bad {
		t.Fatal("cmdFmt -l rewrote the file")
	}

	// -d prints a diff without rewriting it.
	out, _ = captureOutput(t, func() {
		if code := cmdFmt("", []string{"-d", dir}); code != 0 {
			t.Errorf("cmdFmt -d = %d, want 0", code)
		}
	})
	if !strings.Contains(out, "println(\"hi\")") {
		t.Fatalf("cmdFmt -d output %q, want a diff", out)
	}
	if got, _ := os.ReadFile(p); string(got) != bad {
		t.Fatal("cmdFmt -d rewrote the file")
	}

	// Default: rewrite in place and print the changed file, like go fmt.
	out, _ = captureOutput(t, func() {
		if code := cmdFmt("", []string{dir}); code != 0 {
			t.Errorf("cmdFmt = %d, want 0", code)
		}
	})
	if !strings.Contains(out, "a.go") {
		t.Fatalf("cmdFmt output %q, want it to print a.go", out)
	}
	if got, _ := os.ReadFile(p); string(got) != good {
		t.Fatalf("cmdFmt left %q, want %q", string(got), good)
	}

	// Already formatted: silent success.
	out, _ = captureOutput(t, func() {
		if code := cmdFmt("", []string{dir}); code != 0 {
			t.Errorf("cmdFmt (formatted) = %d, want 0", code)
		}
	})
	if out != "" {
		t.Fatalf("cmdFmt (formatted) output %q, want empty", out)
	}

	// Unknown flag: exit 2.
	if code := cmdFmt("", []string{"-bogus", dir}); code != 2 {
		t.Fatalf("cmdFmt -bogus = %d, want 2", code)
	}
}

func TestCmdFmtDotDotDot(t *testing.T) {
	if findGofmt() == "" {
		t.Skip("gofmt not found")
	}
	root := t.TempDir()
	writeTestFile(t, root, "a.go", "package main\n\nfunc main() { println( \"a\" ) }\n")
	sub := filepath.Join(root, "sub")
	writeTestFile(t, sub, "b.go", "package sub\n\nfunc B() { println( \"b\" ) }\n")
	// A vendor tree must be skipped, like `go fmt ./...`.
	writeTestFile(t, root, "vendor/v/v.go", "package v\n\nfunc V() { println( \"v\" ) }\n")
	chdir(t, root)
	if code := cmdFmt("", []string{"./..."}); code != 0 {
		t.Fatalf("cmdFmt ./... = %d, want 0", code)
	}
	for _, p := range []string{filepath.Join(root, "a.go"), filepath.Join(sub, "b.go")} {
		if got, _ := os.ReadFile(p); strings.Contains(string(got), "println( \"") {
			t.Fatalf("cmdFmt ./... left %s unformatted", p)
		}
	}
	if got, _ := os.ReadFile(filepath.Join(root, "vendor/v/v.go")); !strings.Contains(string(got), "println( \"") {
		t.Fatal("cmdFmt ./... formatted vendor/ (must skip it)")
	}
}

func TestCmdFmtIgnoresBuildConstraints(t *testing.T) {
	if findGofmt() == "" {
		t.Skip("gofmt not found")
	}
	dir := t.TempDir()
	p := writeTestFile(t, dir, "x_darwin.go", "package main\n\nfunc D() { println( \"d\" ) }\n")
	if code := cmdFmt("", []string{dir}); code != 0 {
		t.Fatalf("cmdFmt = %d, want 0", code)
	}
	if got, _ := os.ReadFile(p); strings.Contains(string(got), "println( \"") {
		t.Fatal("cmdFmt skipped the darwin-only file (formatting ignores constraints)")
	}
}

func TestResolveTestDirsImportPath(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "go.mod", "module example.com/m\n\ngo 1.21\n")
	sub := filepath.Join(root, "sub")
	writeTestFile(t, sub, "a.go", "package sub\n\nfunc A() {}\n")
	chdir(t, root)
	dirs, err := resolveTestDirs([]string{"example.com/m/sub"})
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) != 1 || dirs[0] != sub {
		t.Fatalf("resolveTestDirs(import path) = %v, want [%s]", dirs, sub)
	}
	if _, err := resolveTestDirs([]string{"example.com/m/nope"}); err == nil {
		t.Fatal("resolveTestDirs(unknown import path) succeeded, want error")
	}
}
