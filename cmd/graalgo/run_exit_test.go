package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// V1 driver unit tests for the fix-p1-run-exitcode sentinel protocol
// (DESIGN.md §8 V1). A fake backend script implements the launcher
// contract: it writes the --status-file sentinel iff instructed, exits
// with an arbitrary code, or dies by signal on demand. No JVM needed.
//
// Each case pins exact stderr bytes + exit code, and each is
// demonstrably capable of failing: reverting graalgoRunProgram to the
// legacy "exit status N for everything" behavior fails the
// backend-failure cases; dropping the --status-file flag fails the
// sentinel cases; dropping the signal branch fails the SIGKILL case.
const fakeBackendScript = `#!/bin/bash
# Fake backend for run-exit-code tests. Directives travel as explicit argv:
#   --fake-mode=MODE   run | backend-fail | spurious-sentinel | sigkill
#   --fake-code=N      process exit code (run / backend-fail modes)
# The driver-under-test prepends --status-file=PATH; this script honors it
# exactly like the real launcher: sentinel iff its own failure path runs.
# Test-only directive: FAKE_BACKEND_CHECK_FAIL=1 makes a --check invocation
# fail like a real build failure (sentinel written, exit 2).
status_file=""
mode="run"
code=0
for a in "$@"; do
  case "$a" in
    --status-file=*) status_file="${a#--status-file=}" ;;
    --fake-mode=*) mode="${a#--fake-mode=}" ;;
    --fake-code=*) code="${a#--fake-code=}" ;;
  esac
done
if [ -n "$FAKE_BACKEND_CHECK_FAIL" ]; then
  for a in "$@"; do
    if [ "$a" = "--check" ]; then
      if [ -n "$status_file" ]; then
        printf '{"backend_failure":true,"exit_code":2}' > "$status_file"
      fi
      exit 2
    fi
  done
fi
case "$mode" in
  backend-fail)
    if [ -n "$status_file" ]; then
      printf '{"backend_failure":true,"exit_code":%s}' "$code" > "$status_file"
    fi
    exit "$code"
    ;;
  spurious-sentinel)
    # Impossible by construction (sentinel is only written on non-zero
    # backend-failure paths); the driver must ignore it defensively.
    if [ -n "$status_file" ]; then
      printf '{"backend_failure":true,"exit_code":1}' > "$status_file"
    fi
    exit 0
    ;;
  sigkill)
    kill -KILL $$
    ;;
  run)
    exit "$code"
    ;;
esac
`

// installFakeBackend points the driver's exec choke point at the fake
// backend script. The override is reset when the test finishes.
func installFakeBackend(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-backend")
	writeFile(t, dir, "fake-backend", fakeBackendScript)
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}
	old := backendRunnerOverride
	backendRunnerOverride = script
	t.Cleanup(func() { backendRunnerOverride = old })
}

// runProgramStderr runs graalgoRunProgram with os.Stderr captured,
// returning the driver's exit code and the exact stderr bytes.
func runProgramStderr(t *testing.T, args []string) (int, string) {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	code := graalgoRunProgram("test-root-ignored", args)
	w.Close()
	os.Stderr = old
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return code, string(out)
}

func TestRunProgramExitContract(t *testing.T) {
	installFakeBackend(t)
	cases := []struct {
		name       string
		args       []string
		wantCode   int
		wantStderr string
	}{
		// Backend's own failures: silent, rc 1 (gc parity — no
		// "exit status" line for backend failures).
		{name: "backend-failure-1", args: []string{"--fake-mode=backend-fail", "--fake-code=1"}, wantCode: 1, wantStderr: ""},
		{name: "backend-failure-2", args: []string{"--fake-mode=backend-fail", "--fake-code=2"}, wantCode: 1, wantStderr: ""},
		// Program outcomes: "exit status N", rc 1 (gc-exact, unchanged).
		{name: "program-exit-0", args: []string{"--fake-mode=run", "--fake-code=0"}, wantCode: 0, wantStderr: ""},
		{name: "program-exit-1", args: []string{"--fake-mode=run", "--fake-code=1"}, wantCode: 1, wantStderr: "exit status 1\n"},
		{name: "program-exit-2", args: []string{"--fake-mode=run", "--fake-code=2"}, wantCode: 1, wantStderr: "exit status 2\n"},
		{name: "program-exit-3", args: []string{"--fake-mode=run", "--fake-code=3"}, wantCode: 1, wantStderr: "exit status 3\n"},
		{name: "program-exit-42", args: []string{"--fake-mode=run", "--fake-code=42"}, wantCode: 1, wantStderr: "exit status 42\n"},
		// A panic (backend exit 2) with no sentinel is a program
		// outcome: "exit status 2", rc 1 (gc parity, probed).
		{name: "panic-2-no-sentinel", args: []string{"--fake-mode=run", "--fake-code=2"}, wantCode: 1, wantStderr: "exit status 2\n"},
		// Backend killed by a signal: "signal: killed", rc 1 (gc parity).
		{name: "sigkill", args: []string{"--fake-mode=sigkill"}, wantCode: 1, wantStderr: "signal: killed\n"},
		// Sentinel present but the process exited 0: ignored, rc 0.
		{name: "sentinel-with-code-0-ignored", args: []string{"--fake-mode=spurious-sentinel"}, wantCode: 0, wantStderr: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, stderr := runProgramStderr(t, tc.args)
			if code != tc.wantCode {
				t.Errorf("exit code = %d, want %d (stderr %q)", code, tc.wantCode, stderr)
			}
			if stderr != tc.wantStderr {
				t.Errorf("stderr = %q, want %q", stderr, tc.wantStderr)
			}
		})
	}
}

// TestRunProgramTempdirFailureFallback pins the E4 degraded-but-safe
// fallback: when the status temp dir cannot be created, the driver warns
// and runs without the flag, so a backend failure gets the legacy
// "exit status N" treatment (never worse than before the change).
//
// The random MkdirTemp suffix makes the warning's middle nondeterministic;
// the "cannot create status dir" warning prefix and the exact
// "exit status 2" tail are asserted.
func TestRunProgramTempdirFailureFallback(t *testing.T) {
	installFakeBackend(t)
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "does-not-exist"))
	code, stderr := runProgramStderr(t, []string{"--fake-mode=backend-fail", "--fake-code=2"})
	if code != 1 {
		t.Errorf("exit code = %d, want 1 (stderr %q)", code, stderr)
	}
	const wantPrefix = "graalgo: warning: cannot create status dir: "
	if !strings.HasPrefix(stderr, wantPrefix) {
		t.Errorf("stderr = %q, want prefix %q", stderr, wantPrefix)
	}
	if !strings.HasSuffix(stderr, "exit status 2\n") {
		t.Errorf("stderr = %q, want suffix %q", stderr, "exit status 2\n")
	}
}

// TestGraalgoRunSignalDeathReturnsOne pins the graalgoRun normalization:
// a signal death used to leak Go's -1 ExitCode(); it now returns 1.
func TestGraalgoRunSignalDeathReturnsOne(t *testing.T) {
	installFakeBackend(t)
	if got := graalgoRun("test-root-ignored", []string{"--fake-mode=sigkill"}); got != 1 {
		t.Errorf("graalgoRun(signal death) = %d, want 1", got)
	}
}

// TestCmdRunCheckPrePassReturnsOne pins the commands.go change: a failed
// --check pre-pass returns 1, never the backend's raw code 2 (gc parity:
// `go run` exits 1 when the build fails).
func TestCmdRunCheckPrePassReturnsOne(t *testing.T) {
	installFakeBackend(t)
	// The fake backend fails --check invocations like a real build
	// failure (sentinel + exit 2).
	t.Setenv("FAKE_BACKEND_CHECK_FAIL", "1")
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")
	if got := cmdRun("test-root-ignored", []string{dir}); got != 1 {
		t.Errorf("cmdRun(failed --check) = %d, want 1", got)
	}
}
