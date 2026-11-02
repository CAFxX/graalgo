package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseBuildFlagsRejectsDryRun(t *testing.T) {
	for _, args := range [][]string{{"-n"}, {"-n", "x.go"}, {"-o", "out", "-n"}} {
		if _, err := parseBuildFlags(args); err == nil || !strings.Contains(err.Error(), "dry-run") {
			t.Errorf("parseBuildFlags(%v) err = %v, want a dry-run error", args, err)
		}
	}
	// -a/-x/-work stay accepted-and-ignored (no regression).
	if _, err := parseBuildFlags([]string{"-a", "-x", "-work", "x.go"}); err != nil {
		t.Errorf("parseBuildFlags(-a/-x/-work) err = %v, want nil", err)
	}
}

func TestParseBuildFlagsTerminator(t *testing.T) {
	bf, err := parseBuildFlags([]string{"--", "./pkg"})
	if err != nil {
		t.Fatalf("parseBuildFlags(-- ./pkg) err = %v", err)
	}
	if len(bf.pkgs) != 1 || bf.pkgs[0] != "./pkg" {
		t.Errorf("pkgs = %v, want [./pkg]", bf.pkgs)
	}
	bf, err = parseBuildFlags([]string{"-o", "out", "--", "-x"})
	if err != nil {
		t.Fatalf("parseBuildFlags(-o out -- -x) err = %v", err)
	}
	if bf.output != "out" || len(bf.pkgs) != 1 || bf.pkgs[0] != "-x" {
		t.Errorf("output = %q, pkgs = %v, want out and [-x]", bf.output, bf.pkgs)
	}
}

func chdirTemp(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	return tmp
}

func writeGoFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestResolveFilesDedup(t *testing.T) {
	tmp := chdirTemp(t)
	writeGoFile(t, tmp, "a.go", "package main\nfunc main() {}\n")
	writeGoFile(t, tmp, "b.go", "package main\n")
	files, err := resolveFiles([]string{".", "a.go"})
	if err != nil {
		t.Fatalf("resolveFiles(., a.go) err = %v", err)
	}
	count := 0
	for _, f := range files {
		if filepath.Clean(f) == "a.go" {
			count++
		}
	}
	if count != 1 || len(files) != 2 {
		t.Errorf("resolveFiles(., a.go) = %v, want a.go exactly once plus b.go", files)
	}
	files, err = resolveFiles([]string{"a.go", "./a.go"})
	if err != nil {
		t.Fatalf("resolveFiles(a.go, ./a.go) err = %v", err)
	}
	if len(files) != 1 {
		t.Errorf("resolveFiles(a.go, ./a.go) = %v, want one file", files)
	}
}

func TestResolveFilesImportPath(t *testing.T) {
	chdirTemp(t)
	for _, tc := range []struct {
		arg  string
		want string
	}{
		{"example.com/foo", "import paths"},
		{"std", "import paths"},
		{"./pkg/...", "patterns"},
		{"./...", "not yet supported"},
		{"./nonexistent", "no such file"},
	} {
		_, err := resolveFiles([]string{tc.arg})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("resolveFiles(%q) err = %v, want error containing %q", tc.arg, err, tc.want)
		}
	}
}

func TestSplitCommandArgs(t *testing.T) {
	if _, err := splitCommandArgs("vet", []string{"-l", "."}); err == nil || !strings.Contains(err.Error(), "does not accept flags") {
		t.Errorf("splitCommandArgs(vet, -l .) err = %v, want flag error", err)
	}
	ops, err := splitCommandArgs("fmt", []string{"--", "-l.go"})
	if err != nil || len(ops) != 1 || ops[0] != "-l.go" {
		t.Errorf("splitCommandArgs(fmt, -- -l.go) = %v, %v, want [-l.go], nil", ops, err)
	}
	ops, err = splitCommandArgs("vet", []string{".", "--", "x.go"})
	if err != nil || len(ops) != 2 {
		t.Errorf("splitCommandArgs(vet, . -- x.go) = %v, %v, want 2 operands", ops, err)
	}
}

func captureStderr(t *testing.T, f func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	f()
	_ = w.Close()
	os.Stderr = old
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestHelpForUnknown(t *testing.T) {
	var code int
	out := captureStderr(t, func() { code = helpFor("bogus") })
	if code != 2 {
		t.Errorf("helpFor(bogus) = %d, want 2", code)
	}
	if !strings.Contains(out, "unknown command") {
		t.Errorf("helpFor(bogus) stderr = %q, want unknown-command message", out)
	}
	if code := helpFor("build"); code != 0 {
		t.Errorf("helpFor(build) = %d, want 0", code)
	}
}

func TestResolveDirectoryOutput(t *testing.T) {
	dir := t.TempDir()
	got, err := resolveDirectoryOutput(dir, []string{"hello.go"})
	if err != nil {
		t.Fatalf("resolveDirectoryOutput(dir, hello.go) err = %v", err)
	}
	if want := filepath.Join(dir, "hello"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	plain := filepath.Join(dir, "prog")
	got, err = resolveDirectoryOutput(plain, nil)
	if err != nil || got != plain {
		t.Errorf("nonexistent path: got %q, %v, want %q, nil", got, err, plain)
	}
	got, err = resolveDirectoryOutput("", nil)
	if err != nil || got != "" {
		t.Errorf("empty output: got %q, %v, want \"\", nil", got, err)
	}
}

func TestRequireMainPackage(t *testing.T) {
	tmp := t.TempDir()
	mainFile := writeGoFile(t, tmp, "main.go", "package main\nfunc main() {}\n")
	libFile := writeGoFile(t, tmp, "lib.go", "package foo\nfunc F() {}\n")
	noFuncFile := writeGoFile(t, tmp, "nofunc.go", "package main\nfunc other() {}\n")
	if err := requireMainPackage([]string{mainFile}); err != nil {
		t.Errorf("main package: err = %v, want nil", err)
	}
	if err := requireMainPackage([]string{libFile}); err == nil || !strings.Contains(err.Error(), "not a main package") {
		t.Errorf("library package: err = %v, want not-a-main-package error", err)
	}
	if err := requireMainPackage([]string{noFuncFile}); err == nil || !strings.Contains(err.Error(), "no func main") {
		t.Errorf("main without func main: err = %v, want no-func-main error", err)
	}
}
