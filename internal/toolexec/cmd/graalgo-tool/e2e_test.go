package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"graalgo-toolexec/archive"
	"graalgo-toolexec/bundle"
	"graalgo-toolexec/ir"
)

// env holds a built wrapper + stub and an isolated build cache.
type env struct {
	t         *testing.T
	tool      string
	cache     string
	toolLog   string
	fakeRun   string
	moduleDir string
}

func goCmd(t *testing.T, dir string, extraEnv []string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), extraEnv...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func setup(t *testing.T) *env {
	t.Helper()
	if testing.Short() {
		t.Skip("builds the wrapper and runs go build; skipped in -short mode")
	}
	bin := t.TempDir()
	e := &env{
		t:       t,
		tool:    filepath.Join(bin, "graalgo-tool"),
		cache:   t.TempDir(),
		toolLog: filepath.Join(bin, "tool.log"),
		fakeRun: filepath.Join(bin, "fake-runner"),
	}
	if out, err := goCmd(t, ".", nil, "build", "-o", e.tool, "."); err != nil {
		t.Fatalf("build graalgo-tool: %v\n%s", err, out)
	}
	if out, err := goCmd(t, ".", nil, "build", "-ldflags=-buildid="+bundle.BuildIDPlaceholder,
		"-o", filepath.Join(bin, "graalgo-stub"), "../graalgo-stub"); err != nil {
		t.Fatalf("build graalgo-stub: %v\n%s", err, out)
	}
	// A stand-in runner: proves the stub's hand-off (argv, exit status)
	// without needing the JVM backend.
	script := "#!/bin/sh\necho \"runner exe=$1 args=$2,$3\"\nexit 7\n"
	if err := os.WriteFile(e.fakeRun, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	e.moduleDir = t.TempDir()
	write := func(name, content string) {
		p := filepath.Join(e.moduleDir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/skel\n\ngo 1.27\n")
	write("main.go", "package main\n\nimport \"example.com/skel/lib\"\n\nfunc main() {\n\tprintln(lib.Answer())\n}\n")
	write("lib/lib.go", "package lib\n\nfunc Answer() int {\n\treturn 42\n}\n")
	return e
}

func (e *env) build(out string, extra ...string) (string, error) {
	e.t.Helper()
	os.Remove(e.toolLog)
	args := append([]string{"build", "-toolexec", e.tool, "-o", out}, extra...)
	args = append(args, ".")
	return goCmd(e.t, e.moduleDir, []string{
		"GOCACHE=" + e.cache, "GOFLAGS=", "CGO_ENABLED=0",
		"GRAALGO_TOOL_LOG=" + e.toolLog, "GRAALGO_RUNNER=" + e.fakeRun,
	}, args...)
}

// toolRuns returns the logged tool invocations that are not -V=full probes.
func (e *env) toolRuns() []string {
	data, err := os.ReadFile(e.toolLog)
	if err != nil {
		return nil
	}
	var runs []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line != "" && !strings.HasSuffix(line, " -V=full") {
			runs = append(runs, line)
		}
	}
	return runs
}

func TestBuildThroughToolexec(t *testing.T) {
	e := setup(t)
	exe := filepath.Join(t.TempDir(), "skel")
	if out, err := e.build(exe); err != nil {
		t.Fatalf("go build -toolexec: %v\n%s", err, out)
	}

	// The executable carries the IR of both user packages and nothing
	// from the (unforked) standard library.
	b, err := bundle.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, p := range b.Packages {
		paths = append(paths, p.ImportPath)
		f, err := ir.DecodeValidate(p.IR)
		if err != nil {
			t.Errorf("%s: IR is not valid GIR: %v", p.ImportPath, err)
			continue
		}
		if f.Pkg.Path != p.ImportPath && !(f.Pkg.Path == "main" && p.ImportPath == "example.com/skel") {
			t.Errorf("GIR of %s says package %s", p.ImportPath, f.Pkg.Path)
		}
		// Positions are gc's: absolute file names with -trimpath applied.
		for _, name := range f.Pkg.Files {
			if !strings.HasSuffix(name, ".go") || strings.Contains(name, "go-build") {
				t.Errorf("%s: recorded file name %q", p.ImportPath, name)
			}
		}
	}
	if got := strings.Join(paths, ","); got != "example.com/skel,example.com/skel/lib" {
		t.Errorf("bundle packages = %s", got)
	}
	if b.Main != "example.com/skel" {
		t.Errorf("bundle main = %q", b.Main)
	}

	// cmd/go can read the build ID from the stub's ELF note, and it is the
	// rewritten content-based ID, not the placeholder.
	id, err := goCmd(t, e.moduleDir, nil, "tool", "buildid", exe)
	if err != nil {
		t.Fatalf("go tool buildid: %v\n%s", err, id)
	}
	id = strings.TrimSpace(id)
	if !regexp.MustCompile(`^[\w-]{20}/[\w-]{20}/[\w-]{20}/[\w-]{20}$`).MatchString(id) || strings.Contains(id, "GRAALGO") {
		t.Errorf("build ID = %q", id)
	}

	// Running the program hands off to the runner with the executable and
	// the program's arguments, and the runner's exit status is the
	// program's.
	cmd := exec.Command(exe, "a", "b")
	cmd.Env = append(os.Environ(), "GRAALGO_RUNNER=")
	out, err := cmd.CombinedOutput()
	ee, ok := err.(*exec.ExitError)
	if !ok || ee.ExitCode() != 7 {
		t.Errorf("exit = %v, want status 7", err)
	}
	if want := "runner exe=" + exe + " args=a,b\n"; string(out) != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

// GG-0028: a second build with a warm cache runs no tool at all, and the
// graalgo cache entries are disjoint from gc's in the same GOCACHE.
func TestCacheBehavior(t *testing.T) {
	e := setup(t)
	dir := t.TempDir()
	exe := filepath.Join(dir, "skel")
	if out, err := e.build(exe); err != nil {
		t.Fatalf("first build: %v\n%s", err, out)
	}
	cold := e.toolRuns()
	var compiles, links int
	for _, r := range cold {
		switch strings.Fields(r)[0] {
		case "compile":
			compiles++
		case "link":
			links++
		}
	}
	if compiles < 3 || links != 1 {
		t.Fatalf("cold build ran %d compiles and %d links; want >= 3 (std + 2 user packages) and 1", compiles, links)
	}

	if out, err := e.build(exe); err != nil {
		t.Fatalf("second build: %v\n%s", err, out)
	}
	if warm := e.toolRuns(); len(warm) != 0 {
		t.Errorf("warm build ran tools:\n%s", strings.Join(warm, "\n"))
	}

	// Plain gc build in the same cache: must not reuse graalgo archives
	// (it would fail or, worse, link a stub) and must produce a working
	// native binary.
	gcExe := filepath.Join(dir, "skel-gc")
	if out, err := goCmd(t, e.moduleDir, []string{"GOCACHE=" + e.cache, "GOFLAGS=", "CGO_ENABLED=0"}, "build", "-o", gcExe, "."); err != nil {
		t.Fatalf("gc build in shared cache: %v\n%s", err, out)
	}
	out, err := exec.Command(gcExe).CombinedOutput()
	if err != nil || string(out) != "42\n" {
		t.Errorf("gc binary: %q, %v", out, err)
	}
	if _, _, err := bundle.Locate(gcExe); err == nil {
		t.Error("gc build in a shared cache produced a graalgo bundle")
	}

	// The two backends' archives of the same package are different cache
	// entries: only graalgo's carries IR.
	shared := []string{"GOCACHE=" + e.cache, "GOFLAGS=", "CGO_ENABLED=0", "GRAALGO_TOOL_LOG=" + e.toolLog}
	gcArchive, err := goCmd(t, e.moduleDir, shared, "list", "-export", "-f", "{{.Export}}", "./lib")
	if err != nil {
		t.Fatalf("go list -export (gc): %v\n%s", err, gcArchive)
	}
	ggArchive, err := goCmd(t, e.moduleDir, shared, "list", "-toolexec", e.tool, "-export", "-f", "{{.Export}}", "./lib")
	if err != nil {
		t.Fatalf("go list -export (graalgo): %v\n%s", err, ggArchive)
	}
	gcArchive, ggArchive = strings.TrimSpace(gcArchive), strings.TrimSpace(ggArchive)
	if gcArchive == ggArchive {
		t.Errorf("gc and graalgo share the cache entry %s", gcArchive)
	}
	if _, err := archive.Read(gcArchive, archive.IRMember); !errors.Is(err, archive.ErrNoMember) {
		t.Errorf("gc archive: IR member lookup = %v, want ErrNoMember", err)
	}
	if _, err := archive.Read(ggArchive, archive.IRMember); err != nil {
		t.Errorf("graalgo archive: %v", err)
	}

	// And back: the graalgo build is still fully cached.
	if out, err := e.build(exe); err != nil {
		t.Fatalf("third build: %v\n%s", err, out)
	}
	if again := e.toolRuns(); len(again) != 0 {
		t.Errorf("graalgo rebuild after a gc build ran tools:\n%s", strings.Join(again, "\n"))
	}
}

// Compile errors are gc's own, byte for byte, with gc's exit path.
func TestCompileErrorIsStock(t *testing.T) {
	e := setup(t)
	bad := "package lib\n\nfunc Answer() int {\n\treturn \"no\"\n}\n"
	if err := os.WriteFile(filepath.Join(e.moduleDir, "lib", "lib.go"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(t.TempDir(), "skel")
	wrapped, err := e.build(exe)
	if err == nil {
		t.Fatal("build of an ill-typed package succeeded")
	}
	stock, err := goCmd(t, e.moduleDir, []string{"GOCACHE=" + t.TempDir(), "GOFLAGS=", "CGO_ENABLED=0"}, "build", "-o", exe, ".")
	if err == nil {
		t.Fatal("stock build of an ill-typed package succeeded")
	}
	if wrapped != stock {
		t.Errorf("diagnostics differ\nwrapped:\n%s\nstock:\n%s", wrapped, stock)
	}
}

// //go:embed reaches the IR: the -embedcfg cmd/go passes is read and the
// file contents land in DATA behind a Data initializer.
func TestEmbedThroughToolexec(t *testing.T) {
	e := setup(t)
	src := "package lib\n\nimport _ \"embed\"\n\n//go:embed data.txt\nvar data string\n\nfunc Answer() int {\n\treturn len(data)\n}\n"
	if err := os.WriteFile(filepath.Join(e.moduleDir, "lib", "lib.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.moduleDir, "lib", "data.txt"), []byte("embedded!"), 0o644); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(t.TempDir(), "skel")
	if out, err := e.build(exe); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	b, err := bundle.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range b.Packages {
		if p.ImportPath != "example.com/skel/lib" {
			continue
		}
		f, err := ir.DecodeValidate(p.IR)
		if err != nil {
			t.Fatal(err)
		}
		if string(f.Data) != "embedded!" {
			t.Errorf("DATA = %q", f.Data)
		}
		return
	}
	t.Fatal("no IR for lib")
}

// Input the IR cannot represent (nesting deeper than the decoders accept,
// a legal program for gc) must fail the build with a message naming the
// package, never produce a binary or a cached archive that fails later.
func TestUnsupportedConstructFailsLoudly(t *testing.T) {
	e := setup(t)
	src := "package lib\n\nfunc Answer() int {\n\tx := 1\n\treturn " + strings.Repeat("x+", 1100) + "x\n}\n"
	if err := os.WriteFile(filepath.Join(e.moduleDir, "lib", "lib.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(t.TempDir(), "skel")
	out, err := e.build(exe)
	if err == nil {
		t.Fatal("build succeeded")
	}
	if !strings.Contains(out, "graalgo-tool: example.com/skel/lib:") || !strings.Contains(out, "nesting deeper than") {
		t.Errorf("unexpected diagnostics:\n%s", out)
	}
	if _, statErr := os.Stat(exe); statErr == nil {
		t.Error("an executable was written")
	}
}
