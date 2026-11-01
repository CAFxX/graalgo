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

func TestRunEnv_PassesTmpdir(t *testing.T) {
	// Run steps must inherit TMPDIR: the backend launcher falls back to
	// /tmp when it is unset, and a full /tmp truncates the frontend's
	// AST dump mid-write, failing runs with exit 1. Red-green: this
	// fails if runEnv stops passing TMPDIR through.
	t.Setenv("TMPDIR", "/tmp/fake-worktree-tmp")
	env := runEnv(nil)
	found := false
	for _, kv := range env {
		if kv == "TMPDIR=/tmp/fake-worktree-tmp" {
			found = true
		}
		if kv == "TMPDIR=" || kv == "TMPDIR=/tmp" {
			t.Fatalf("runEnv must not reset TMPDIR: %q", kv)
		}
	}
	if !found {
		t.Fatalf("TMPDIR not passed through: %v", env)
	}
	// Unset (empty) TMPDIR stays unset: the launcher fallback is
	// unchanged for runs outside the Makefile.
	t.Setenv("TMPDIR", "")
	env = runEnv(nil)
	for _, kv := range env {
		if kv == "TMPDIR=" || kv == "TMPDIR=/tmp" {
			t.Fatalf("empty TMPDIR must not be materialized: %q", kv)
		}
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

// writeFlakyScript writes an executable script that counts its own
// invocations in counterPath: the first call sleeps past any short cap
// (so the harness kills it), later calls exit 0 at once (build variant)
// or print want and exit 0 (run variant, when want != "").
func writeFlakyScript(t *testing.T, counterPath, want string) string {
	t.Helper()
	script := filepath.Join(t.TempDir(), "flaky.sh")
	body := "#!/bin/sh\n" +
		"echo x >> \"$1\"\n" +
		"if [ \"$(wc -l < \"$1\")\" -gt 1 ]; then\n"
	if want != "" {
		body += "  echo \"" + want + "\"\n"
	}
	body += "  exit 0\n" +
		"fi\n" +
		// exec, not a bare sleep: the shell process BECOMES sleep, so the
		// harness's cap-kill ends the process (and its pipes) at once
		// instead of orphaning a sleeping child that would hold the
		// output pipes open past the kill.
		"exec sleep 30\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return script
}

func countLines(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), "\n")
}

// Red-green for the retry-on-timeout rule: a build killed by the cap on
// the first attempt must be retried and succeed, not recorded as failed.
// (This is the 01_hello / 08_multiple_assignment record-gate failure:
// a cap-kill in run 1 only failed the self-consistency gate.)
func TestBuildOnly_RetriesTimeout(t *testing.T) {
	dir := t.TempDir()
	counter := filepath.Join(dir, "count")
	script := writeFlakyScript(t, counter, "")
	cfg := &Config{BuildTimeoutSec: 1}
	b := &Backend{Name: "t", Build: []string{script, counter}}
	oc := BuildOnly(cfg, b, testProg("hello"), t.TempDir())
	if !oc.BuildOK {
		t.Fatalf("expected retried build to succeed: %s", oc.BuildLog)
	}
	if n := countLines(t, counter); n != 2 {
		t.Fatalf("expected exactly 2 build attempts, got %d", n)
	}
}

// The retry must trigger ONLY on a deadline: a build that fails fast
// with a real error is reported immediately, never retried.
func TestBuildOnly_NoRetryOnFastFailure(t *testing.T) {
	dir := t.TempDir()
	counter := filepath.Join(dir, "count")
	script := filepath.Join(dir, "fail.sh")
	body := "#!/bin/sh\necho x >> \"$1\"\nexit 3\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{BuildTimeoutSec: 1}
	b := &Backend{Name: "t", Build: []string{script, counter}}
	oc := BuildOnly(cfg, b, testProg("hello"), t.TempDir())
	if oc.BuildOK {
		t.Fatal("expected build failure")
	}
	if n := countLines(t, counter); n != 1 {
		t.Fatalf("expected exactly 1 build attempt for a fast failure, got %d", n)
	}
}

// Same retry rule for the run step: a run killed by the cap on the first
// attempt must be retried, and the retried run's output is the outcome.
func TestRunProgram_RetriesRunTimeout(t *testing.T) {
	dir := t.TempDir()
	counter := filepath.Join(dir, "count")
	script := writeFlakyScript(t, counter, "retry-ok")
	cfg := &Config{TimeoutSec: 2}
	b := &Backend{Name: "t", Build: []string{"true"}, Run: []string{script, counter}}
	oc := RunProgram(cfg, b, testProg("hello"), t.TempDir())
	if !oc.BuildOK {
		t.Fatalf("build failed: %s", oc.BuildLog)
	}
	if oc.TimedOut {
		t.Fatal("expected retried run to succeed, got timeout")
	}
	if oc.Stdout != "retry-ok\n" {
		t.Fatalf("stdout = %q", oc.Stdout)
	}
	if oc.ExitCode != 0 {
		t.Fatalf("exit = %d", oc.ExitCode)
	}
	if n := countLines(t, counter); n != 2 {
		t.Fatalf("expected exactly 2 run attempts, got %d", n)
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
