package harness

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Outcome is everything observed from building and running one program
// under one backend. Stderr and BuildLog are stored normalized.
type Outcome struct {
	Program  string        `json:"program"`
	Backend  string        `json:"backend"`
	BuildOK  bool          `json:"buildOk"`
	BuildLog string        `json:"buildLog"`
	Stdout   string        `json:"stdout"`
	Stderr   string        `json:"stderr"`
	ExitCode int           `json:"exitCode"`
	TimedOut bool          `json:"timedOut"`
	Duration time.Duration `json:"durationNs"`
}

// BuildOnly runs just the build step of backend b for prog and returns the
// outcome with only the build fields populated (BuildOK, BuildLog).
// workRoot scopes build artifacts for this invocation. It exists for
// compile-only differential modes (build parity / build-failure parity)
// where running the program is meaningless or impossible.
//
// A build killed by the wall-clock cap is retried once: on a contended
// dev box (sibling lanes, load shedder freezes) a healthy build can
// exceed the cap purely from scheduling delay, and a cap-kill is
// transient noise, not a build outcome — two consecutive records failed
// their self-consistency gate on exactly this signature (buildOk false
// in run 1, true in run 2, different programs each time). Only a
// deadline counts for the retry: a genuine compile error fails fast and
// is reported immediately, so real build failures still fail identically
// in both record runs and the gate still catches real nondeterminism.
func BuildOnly(cfg *Config, b *Backend, prog Program, workRoot string) Outcome {
	oc := Outcome{Program: prog.Name, Backend: b.Name}
	dir := filepath.Join(workRoot, prog.Name, b.Name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		oc.BuildOK = false
		oc.BuildLog = Normalize(fmt.Sprintf("mkdir workdir: %v", err))
		return oc
	}
	bin := filepath.Join(dir, "prog")
	buildCmd := b.RenderBuild(prog.Main, bin)
	buildTimeout := cfg.BuildTimeoutSec
	if buildTimeout <= 0 {
		buildTimeout = 120
	}
	for attempt := 0; attempt < 2; attempt++ {
		buildCtx, cancel := context.WithTimeout(context.Background(), time.Duration(buildTimeout)*time.Second)
		var blog bytes.Buffer
		cmd := exec.CommandContext(buildCtx, buildCmd[0], buildCmd[1:]...)
		cmd.Env = overrideEnv(os.Environ(), b.Env)
		cmd.Stdout = &blog
		cmd.Stderr = &blog
		err := cmd.Run()
		timedOut := buildCtx.Err() == context.DeadlineExceeded
		cancel()
		if err == nil {
			oc.BuildOK = true
			// P2-12: apply the stderr filters on the success path too, symmetric
			// with the failure path below (Compare only reads BuildLog when both
			// builds fail, so this is hygiene, not behavior).
			oc.BuildLog = Normalize(b.FilterStderr(blog.String()))
			return oc
		}
		if timedOut && attempt == 0 {
			continue
		}
		oc.BuildOK = false
		// Scrub the workRoot from the echoed command: it is backend- and
		// run-specific, so leaving it in would make two identical build
		// failures compare as different (false divergence signal).
		// Backend stderr noise filters apply here too (e.g. the Truffle
		// Unsafe warnings), so the first-lines excerpt shows the real error.
		cmdStr := strings.ReplaceAll(fmt.Sprintf("%v", buildCmd), workRoot, "{WORK}")
		oc.BuildLog = Normalize(fmt.Sprintf("build %s failed: %v\n%s", cmdStr, err,
			b.FilterStderr(blog.String())))
		return oc
	}
	return oc
}

// RunProgram builds prog with backend and runs the resulting binary.
// workRoot scopes all build artifacts for this invocation.
//
// Determinism controls (FEASIBILITY.md §7):
//   - builds run with the ambient environment + backend Env overrides
//     (GOCACHE etc. keep working; GOTOOLCHAIN pinned via backend env);
//   - runs execute in a fresh empty working directory with a minimal
//     environment: PATH, HOME, plus backend Env overrides (GOMAXPROCS=1
//     is pinned via the default backend config, not trusted to ambient);
//   - a per-program timeout converts hangs/deadlocks into TimedOut=true.
func RunProgram(cfg *Config, b *Backend, prog Program, workRoot string) Outcome {
	oc := BuildOnly(cfg, b, prog, workRoot)
	if !oc.BuildOK {
		return oc
	}

	runDir, err := os.MkdirTemp("", "diffharness-run-*")
	if err != nil {
		oc.BuildLog = Normalize(fmt.Sprintf("mktemp rundir: %v", err))
		return oc
	}
	defer os.RemoveAll(runDir)

	bin := filepath.Join(workRoot, prog.Name, b.Name, "prog")
	runCmd := b.RenderRun(prog.Main, bin)
	// Like the build step (see BuildOnly), a run killed by the wall-clock
	// cap is retried once: on a contended box a healthy program can
	// exceed the cap purely from scheduling delay, and a cap-kill is
	// transient noise, not a program outcome. Only a deadline retries;
	// a genuine non-zero exit is reported immediately.
	for attempt := 0; attempt < 2; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.TimeoutSec)*time.Second)
		var stdout, stderr bytes.Buffer
		cmd := exec.CommandContext(ctx, runCmd[0], runCmd[1:]...)
		cmd.Dir = runDir
		cmd.Env = runEnv(b.Env)
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		start := time.Now()
		runErr := cmd.Run()
		oc.Duration = time.Since(start)
		// Stdout is normalized for addresses: pointer values printed by the
		// program (%p, %x of pointers) are not semantic observables and differ
		// between runs even of the same binary. Message text is untouched.
		oc.Stdout = Normalize(stdout.String())
		// Stderr carries the backend's declared noise filters (e.g. the
		// Truffle Unsafe warnings on the graalgo backend) after normalization.
		oc.Stderr = b.FilterStderr(Normalize(stderr.String()))
		timedOut := ctx.Err() == context.DeadlineExceeded
		cancel()
		if timedOut && attempt == 0 {
			continue
		}
		if timedOut {
			oc.TimedOut = true
			oc.ExitCode = -1
			return oc
		}
		if runErr != nil {
			if ee, ok := runErr.(*exec.ExitError); ok {
				oc.ExitCode = ee.ExitCode()
			} else {
				oc.ExitCode = -1
				oc.Stderr += Normalize(fmt.Sprintf("\n[harness: run error: %v]", runErr))
			}
		}
		return oc
	}
	return oc
}

// overrideEnv applies KEY=VAL overrides onto a base environment.
func overrideEnv(base []string, overrides map[string]string) []string {
	out := append([]string{}, base...)
	seen := map[string]int{}
	for i, kv := range out {
		if k := kv[:indexByte(kv, '=')]; k != "" {
			seen[k] = i
		}
	}
	for k, v := range overrides {
		kv := k + "=" + v
		if i, ok := seen[k]; ok {
			out[i] = kv
		} else {
			out = append(out, kv)
		}
	}
	return out
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return len(s)
}

// runEnv is the minimal deterministic environment for program runs.
func runEnv(overrides map[string]string) []string {
	base := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
	}
	// The graalgo backend's launcher (graalgo-sync-run) needs GRAALVM_HOME to
	// find java; without it the script falls back to a local-only default
	// and fails with 127 in CI. Pass it through when set.
	if gh := os.Getenv("GRAALVM_HOME"); gh != "" {
		base = append(base, "GRAALVM_HOME="+gh)
	}
	// The backend launcher and frontend dump their temp files under
	// $TMPDIR (defaulting to /tmp when unset); the snapshot Makefile
	// points TMPDIR at the worktree .tmp on persistent disk because
	// /tmp is a small tmpfs that fills up, and a full /tmp truncates
	// the frontend's AST dump mid-write, failing the run with exit 1.
	// Pass it through when set so run steps use the same temp dir as
	// build steps instead of silently falling back to /tmp.
	if td := os.Getenv("TMPDIR"); td != "" {
		base = append(base, "TMPDIR="+td)
	}
	return overrideEnv(base, overrides)
}
