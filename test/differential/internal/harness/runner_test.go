package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testBackend builds a Backend pointing at the local go toolchain.
// DH_GO overrides the toolchain root (as in run.sh); GO (the binary path,
// as exported by CI) derives the root; otherwise PATH lookup.
func testBackend(t *testing.T, name string) *Backend {
	t.Helper()
	goRoot := os.Getenv("DH_GO")
	if goRoot == "" {
		if goBin := os.Getenv("GO"); goBin != "" {
			goRoot = filepath.Dir(filepath.Dir(goBin))
		}
	}
	if goRoot == "" {
		goRoot = os.ExpandEnv("${HOME}/tools/go")
	}
	vars := map[string]string{"GO": goRoot}
	return &Backend{
		Name:  name,
		Vars:  vars,
		Build: []string{"{GO}/bin/go", "build", "-o", "{BIN}", "{SRC}"},
		Run:   []string{"{BIN}"},
		Env:   map[string]string{"GOMAXPROCS": "1", "GOTOOLCHAIN": "local"},
	}
}

func testProg(name string) Program {
	return Program{
		Name: name,
		Dir:  filepath.Join("../../testdata", name),
		Main: filepath.Join("../../testdata", name, "main.go"),
	}
}

func TestRunProgram_Hello(t *testing.T) {
	cfg := &Config{TimeoutSec: 15}
	oc := RunProgram(cfg, testBackend(t, "t"), testProg("hello"), t.TempDir())
	if !oc.BuildOK {
		t.Fatalf("build failed: %s", oc.BuildLog)
	}
	if oc.Stdout != "probe-ok\n" {
		t.Fatalf("stdout = %q", oc.Stdout)
	}
	if oc.ExitCode != 0 || oc.TimedOut {
		t.Fatalf("exit=%d timedOut=%v", oc.ExitCode, oc.TimedOut)
	}
}

func TestRunProgram_EnvPinned(t *testing.T) {
	// GOMAXPROCS must be 1 regardless of ambient environment. Red-green:
	// this fails if the runner stops pinning the variable.
	t.Setenv("GOMAXPROCS", "8")
	cfg := &Config{TimeoutSec: 15}
	oc := RunProgram(cfg, testBackend(t, "t"), testProg("envprobe"), t.TempDir())
	if !oc.BuildOK {
		t.Fatalf("build failed: %s", oc.BuildLog)
	}
	if strings.TrimSpace(oc.Stdout) != "gomaxprocs: 1" {
		t.Fatalf("GOMAXPROCS not pinned: %q", oc.Stdout)
	}
}

func TestRunProgram_BuildFailure(t *testing.T) {
	cfg := &Config{TimeoutSec: 15}
	// NOTE: the fixture lives in testdata/_broken (underscore prefix) so the
	// go tool ignores the intentionally-invalid package; the runner reads it
	// via an explicit path.
	workDir := t.TempDir()
	prog := Program{Name: "broken", Dir: "../../testdata/_broken", Main: "../../testdata/_broken/main.go"}
	oc := RunProgram(cfg, testBackend(t, "t"), prog, workDir)
	if oc.BuildOK {
		t.Fatal("expected build failure")
	}
	if oc.BuildLog == "" {
		t.Fatal("expected non-empty build log")
	}
	// Regression: the echoed build command must not leak the run-specific
	// workdir, or two identical build failures compare as different.
	if strings.Contains(oc.BuildLog, workDir) {
		t.Fatalf("build log leaks workdir: %q", oc.BuildLog)
	}
	if oc.BuildOK {
		t.Fatal("expected build failure")
	}
	if oc.BuildLog == "" {
		t.Fatal("expected non-empty build log")
	}
}

func TestRunProgram_Timeout(t *testing.T) {
	cfg := &Config{TimeoutSec: 2}
	oc := RunProgram(cfg, testBackend(t, "t"), testProg("sleeper"), t.TempDir())
	if !oc.BuildOK {
		t.Fatalf("build failed: %s", oc.BuildLog)
	}
	if !oc.TimedOut {
		t.Fatal("expected timeout for sleeping program")
	}
}

func TestRunProgram_StdoutAddressNormalized(t *testing.T) {
	// A program printing a pointer must compare equal across two runs even
	// though the raw addresses differ (ASLR). This is the red-green test
	// for the stdout address-normalization decision.
	dir := t.TempDir()
	src := `package main

import "fmt"

func main() { x := 42; fmt.Printf("%p %p\n", &x, &x) }
`
	main := filepath.Join(dir, "main.go")
	if err := os.WriteFile(main, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	prog := Program{Name: "ptrprint", Dir: dir, Main: main}
	cfg := &Config{TimeoutSec: 15}
	b := testBackend(t, "t")
	a := RunProgram(cfg, b, prog, t.TempDir())
	c := RunProgram(cfg, b, prog, t.TempDir())
	if !a.BuildOK || !c.BuildOK {
		t.Fatalf("build failed: %s / %s", a.BuildLog, c.BuildLog)
	}
	if strings.Contains(a.Stdout, "0xADDR") == false {
		t.Fatalf("stdout address not normalized: %q", a.Stdout)
	}
	if cmp := Compare(a, c); !cmp.Same {
		t.Fatalf("two runs of pointer-printing program diverged: %+v", cmp)
	}
}
