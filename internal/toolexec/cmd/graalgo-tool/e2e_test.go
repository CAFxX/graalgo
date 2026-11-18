package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"

	"graalgo-toolexec/archive"
	"graalgo-toolexec/bundle"
	"graalgo-toolexec/fork"
	"graalgo-toolexec/intrinsics"
	"graalgo-toolexec/ir"
	"graalgo-toolexec/release"
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

// The standard library these tests build against is graalgo's fork
// (design §5.1), loaded once per test process: forkOverlay applies it to
// the toolchain's GOROOT with -overlay, the launcher tests assemble a
// GOROOT from it.
var theFork struct {
	once    sync.Once
	fork    *fork.Fork
	overlay string // GOFLAGS entry
	err     error
}

func loadFork(t *testing.T) *fork.Fork {
	t.Helper()
	theFork.once.Do(func() {
		goroot, err := goCmd(t, ".", nil, "env", "GOROOT")
		if err != nil {
			theFork.err = err
			return
		}
		f, err := fork.Load(filepath.Join("..", "..", "..", "..", "stdlib"), strings.TrimSpace(goroot), release.GoVersion)
		if err != nil {
			theFork.err = err
			return
		}
		// Not t.TempDir: the overlay outlives the first test that asks.
		dir, err := os.MkdirTemp("", "graalgo-fork-overlay")
		if err != nil {
			theFork.err = err
			return
		}
		data, err := f.Overlay(filepath.Join(dir, "work"))
		if err == nil {
			err = os.WriteFile(filepath.Join(dir, "overlay.json"), data, 0o644)
		}
		theFork.fork, theFork.err = f, err
		theFork.overlay = "-overlay=" + filepath.Join(dir, "overlay.json")
	})
	if theFork.err != nil {
		t.Fatalf("stdlib fork: %v", theFork.err)
	}
	return theFork.fork
}

// forkOverlay is the GOFLAGS entry that applies the fork.
func forkOverlay(t *testing.T) string {
	t.Helper()
	loadFork(t)
	return theFork.overlay
}

func TestMain(m *testing.M) {
	code := m.Run()
	if theFork.overlay != "" {
		os.RemoveAll(filepath.Dir(strings.TrimPrefix(theFork.overlay, "-overlay=")))
	}
	os.Exit(code)
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
		"GOCACHE=" + e.cache, "GOFLAGS=" + forkOverlay(e.t), "CGO_ENABLED=0",
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

	// The executable carries the IR of both user packages and of the
	// standard-library packages of the link: the facade runtime and what
	// it imports.
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
	if got := strings.Join(paths, ","); got != "example.com/skel,example.com/skel/lib,internal/goarch,internal/goos,runtime" {
		t.Errorf("bundle packages = %s", got)
	}
	if r := b.Resolution; r.RegistryVersion != uint64(intrinsics.Registry.Version) || r.RegistryHash == "" {
		t.Errorf("bundle resolution names registry %d %q", r.RegistryVersion, r.RegistryHash)
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
	// Compile only: package embed's closure (io/fs, time, syscall) does
	// not link yet — its intrinsics are milestones M1r–M1c — and the
	// subject here is the compile step.
	archivePath, err := goCmd(t, e.moduleDir, []string{
		"GOCACHE=" + e.cache, "GOFLAGS=" + forkOverlay(t), "CGO_ENABLED=0",
	}, "list", "-toolexec", e.tool, "-export", "-f", "{{.Export}}", "./lib")
	if err != nil {
		t.Fatalf("build: %v\n%s", err, archivePath)
	}
	gir, err := archive.Read(strings.TrimSpace(archivePath), archive.IRMember)
	if err != nil {
		t.Fatal(err)
	}
	f, err := ir.DecodeValidate(gir)
	if err != nil {
		t.Fatal(err)
	}
	if string(f.Data) != "embedded!" {
		t.Errorf("DATA = %q", f.Data)
	}
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

// Coordinator review (c): the IR is emitted for the build's target, not
// the host: GOARCH=386 gives 4-byte layout and a 386 GIR header.
func TestCrossTargetIR(t *testing.T) {
	e := setup(t)
	src := "package lib\n\nimport \"unsafe\"\n\nconst W = unsafe.Sizeof(uintptr(0))\n\nfunc Answer() int {\n\treturn int(W)\n}\n"
	if err := os.WriteFile(filepath.Join(e.moduleDir, "lib", "lib.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	a := filepath.Join(t.TempDir(), "lib.a")
	out, err := goCmd(t, e.moduleDir, []string{"GOCACHE=" + e.cache, "GOFLAGS=", "CGO_ENABLED=0", "GOARCH=386"},
		"build", "-toolexec", e.tool, "-o", a, "./lib")
	if err != nil {
		t.Fatalf("GOARCH=386 build: %v\n%s", err, out)
	}
	data, err := archive.Read(a, archive.IRMember)
	if err != nil {
		t.Fatal(err)
	}
	f, err := ir.DecodeValidate(data)
	if err != nil {
		t.Fatal(err)
	}
	if f.GOARCH != "386" {
		t.Errorf("GIR GOARCH = %q, want 386", f.GOARCH)
	}
	for _, o := range f.Objects {
		if o.Name == "W" && (o.Value == nil || o.Value.Text != "4") {
			t.Errorf("unsafe.Sizeof(uintptr(0)) = %+v, want 4 on 386", o.Value)
		}
	}
}

// Coordinator review (d): link -X settings (the user's and cmd/go's) and
// the module build info reach the bundle instead of being dropped.
func TestLinkCarriesXAndModinfo(t *testing.T) {
	e := setup(t)
	exe := filepath.Join(t.TempDir(), "skel")
	if out, err := e.build(exe, "-ldflags=-X=example.com/skel/lib.Version=v1.2"); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	b, err := bundle.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, x := range b.X {
		found = found || x == "example.com/skel/lib.Version=v1.2"
	}
	if !found {
		t.Errorf("bundle -X settings %q lack the user's", b.X)
	}
	if !strings.Contains(b.ModInfo, "example.com/skel") {
		t.Errorf("bundle modinfo %q lacks the main module", b.ModInfo)
	}
}

// Coordinator review (e): environment the link step reads is part of the
// link tool's -V=full identity, so cmd/go does not reuse a link output
// produced under another GRAALGO_RUNNER / GRAALGO_RACE.
func TestLinkIdentityCoversEnv(t *testing.T) {
	e := setup(t)
	gotooldir, err := goCmd(t, ".", nil, "env", "GOTOOLDIR")
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(strings.TrimSpace(gotooldir), "link")
	probe := func(env ...string) string {
		cmd := exec.Command(e.tool, link, "-V=full")
		cmd.Env = append(os.Environ(), env...)
		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	base := probe("GRAALGO_RUNNER=/a", "GRAALGO_RACE=")
	if probe("GRAALGO_RUNNER=/b", "GRAALGO_RACE=") == base {
		t.Error("GRAALGO_RUNNER does not change the link identity")
	}
	if probe("GRAALGO_RUNNER=/a", "GRAALGO_RACE=ignore") == base {
		t.Error("GRAALGO_RACE does not change the link identity")
	}
}

// Coordinator review (g): a tool killed by a signal is mirrored (W1 exited
// 255), and every -d flag is checked for the libfuzzer refusal.
func TestSignalAndRepeatedFlags(t *testing.T) {
	e := setup(t)
	cmd := exec.Command(e.tool, "/bin/sh", "-c", "kill -TERM $$")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("pass-through of a signalled tool: %v", err)
	}
	if ws, ok := ee.Sys().(syscall.WaitStatus); !ok || !ws.Signaled() || ws.Signal() != syscall.SIGTERM {
		t.Errorf("graalgo-tool exited with %v, want death by SIGTERM", ee.ProcessState)
	}
	gotooldir, _ := goCmd(t, ".", nil, "env", "GOTOOLDIR")
	compile := filepath.Join(strings.TrimSpace(gotooldir), "compile")
	src := filepath.Join(t.TempDir(), "x.go")
	os.WriteFile(src, []byte("package x\n"), 0o644)
	out, err := exec.Command(e.tool, compile, "-d=libfuzzer", "-d=checkptr", "-o", filepath.Join(t.TempDir(), "x.a"), "-p", "x", src).CombinedOutput()
	if err == nil || !strings.Contains(string(out), "libfuzzer") {
		t.Errorf("-d=libfuzzer followed by another -d was not refused: %v\n%s", err, out)
	}
}
