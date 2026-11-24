package harness

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Outcome is everything observed from building and running one program
// under one backend. Stderr and BuildLog are stored normalized.
type Outcome struct {
	Program  string `json:"program"`
	Backend  string `json:"backend"`
	BuildOK  bool   `json:"buildOk"`
	BuildLog string `json:"buildLog"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exitCode"`
	TimedOut bool   `json:"timedOut"`
	// BuildTimedOut: the build step hit buildTimeout (BuildOK is false).
	BuildTimedOut bool `json:"buildTimedOut,omitempty"`
	// RetriedTimeout: the run timed out once and the retry with the
	// extended timeout finished (RunReference only; a host-speed note).
	RetriedTimeout bool          `json:"retriedTimeout,omitempty"`
	Duration       time.Duration `json:"durationNs"`
}

// buildTimeout bounds one build step (a var so tests can shrink it).
var buildTimeout = 120 * time.Second

// BuildOnly runs just the build step of backend b for prog and returns the
// outcome with only the build fields populated (BuildOK, BuildLog).
// workRoot scopes build artifacts for this invocation. It exists for
// compile-only differential modes (build parity / build-failure parity)
// where running the program is meaningless or impossible.
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
	buildCtx, cancel := context.WithTimeout(context.Background(), buildTimeout)
	defer cancel()
	var blog bytes.Buffer
	cmd := exec.CommandContext(buildCtx, buildCmd[0], buildCmd[1:]...)
	cmd.Env = overrideEnv(os.Environ(), b.Env)
	cmd.Stdout = &blog
	cmd.Stderr = &blog
	if err := cmd.Run(); err != nil {
		oc.BuildOK = false
		oc.BuildTimedOut = buildCtx.Err() == context.DeadlineExceeded
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
	oc.BuildOK = true
	// P2-12: apply the stderr filters on the success path too, symmetric
	// with the failure path above (Compare only reads BuildLog when both
	// builds fail, so this is hygiene, not behavior).
	oc.BuildLog = Normalize(b.FilterStderr(blog.String()))
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
	return runBuilt(cfg, b, prog, workRoot, oc)
}

// RunSkippingBuild runs prog under b without its build step, as if the
// build had succeeded with an empty log. Only for a backend whose run
// command needs no build product (Backend.LazyBuild: the launcher takes
// the source).
func RunSkippingBuild(cfg *Config, b *Backend, prog Program, workRoot string) Outcome {
	return runBuilt(cfg, b, prog, workRoot, Outcome{Program: prog.Name, Backend: b.Name, BuildOK: true})
}

func runBuilt(cfg *Config, b *Backend, prog Program, workRoot string, oc Outcome) Outcome {
	runDir, err := os.MkdirTemp("", "diffharness-run-*")
	if err != nil {
		oc.BuildLog = Normalize(fmt.Sprintf("mktemp rundir: %v", err))
		return oc
	}
	defer os.RemoveAll(runDir)

	bin := filepath.Join(workRoot, prog.Name, b.Name, "prog")
	runCmd := b.RenderRunArgs(prog.Main, bin, prog.Args)
	ctx, cancel := context.WithTimeout(context.Background(), prog.timeout(cfg))
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, runCmd[0], runCmd[1:]...)
	killGroupOnCancel(cmd)
	cmd.Dir = runDir
	if prog.RunDir != "" {
		cmd.Dir = prog.RunDir
	}
	cmd.Env = runEnv(b.Env)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	start := time.Now()
	runErr := cmd.Run()
	oc.Duration = time.Since(start)
	// Stdout is compared exactly, except for the program's declared
	// opt-in rules (e.g. "pointers" for a program printing %p).
	oc.Stdout = NormalizeStdout(stdout.String(), prog.StdoutRules)
	// Stderr carries the backend's declared noise filters (e.g. the
	// Truffle Unsafe warnings on the graalgo backend) after normalization.
	oc.Stderr = b.FilterStderr(Normalize(stderr.String()))
	if ctx.Err() == context.DeadlineExceeded {
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

// refTimeoutRetryFactor is the factor RunReference extends a timed-out
// reference run's timeout by for its single retry.
const refTimeoutRetryFactor = 2

// RunReference is RunProgram for the REFERENCE (gc) backend, whose
// outcome is the oracle: a timeout there is a property of the host (a
// loaded or slow CI runner), never of graalgo, so it must not be
// compared as a behavior. A timed-out run is retried once with
// refTimeoutRetryFactor times the timeout (Outcome.RetriedTimeout notes a
// retry that finished); if it still times out, callers must treat the
// outcome as INFRA (RefInfra) and never as a divergence.
func RunReference(cfg *Config, b *Backend, prog Program, workRoot string) Outcome {
	return runReference(func(p Program) Outcome { return RunProgram(cfg, b, p, workRoot) }, prog)
}

func runReference(run func(Program) Outcome, prog Program) Outcome {
	oc := run(prog)
	if !oc.TimedOut {
		return oc
	}
	oc2 := run(prog.Extended())
	oc2.RetriedTimeout = !oc2.TimedOut
	return oc2
}

// Extended is prog with its timeout extended by the reference retry factor
// (the footing a slow host's retried reference run was given; the
// alternate backend's run must get the same).
func (p Program) Extended() Program {
	p.TimeoutScale = p.timeoutScale() * refTimeoutRetryFactor
	return p
}

// RefInfra reports whether a reference outcome is unusable as an oracle:
// the run (after RunReference's retry) or the build timed out. The
// verdict is INFRA (exit 3), never DIVERGE.
func RefInfra(oc Outcome) bool { return oc.TimedOut || oc.BuildTimedOut }

// timeoutScale is the program's timeout multiplier (>= 1).
func (p Program) timeoutScale() int {
	if p.TimeoutScale < 1 {
		return 1
	}
	return p.TimeoutScale
}

// timeout is the run timeout of p under cfg.
func (p Program) timeout(cfg *Config) time.Duration {
	unit := cfg.TimeoutUnit
	if unit <= 0 {
		unit = time.Second
	}
	return time.Duration(cfg.TimeoutSec*p.timeoutScale()) * unit
}

// killGroupOnCancel makes a context cancellation (the timeout) kill the
// command's whole process group, and bounds the wait for its pipes. A
// program that spawns children (the go tool, a shell) otherwise leaves them
// alive holding stdout open: cmd.Run blocks until they exit, so a "timeout"
// takes as long as the children, and they leak.
func killGroupOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second
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
	// The graalgo backend's launcher (graalgo-run) needs GRAALVM_HOME to
	// find java; without it the script falls back to a local-only default
	// and fails with 127 in CI. Pass it through when set.
	if gh := os.Getenv("GRAALVM_HOME"); gh != "" {
		base = append(base, "GRAALVM_HOME="+gh)
	}
	// GRAALGO_JAVA_OPTS: launcher JVM resource caps on shared hosts
	// (read only by the graalgo launchers; inert for gc binaries).
	if jo := os.Getenv("GRAALGO_JAVA_OPTS"); jo != "" {
		base = append(base, "GRAALGO_JAVA_OPTS="+jo)
	}
	// The launchers' class locations (jars of the same classes, for the
	// gates' class-data-sharing archive: test/lib/jvmcds.sh); inert for gc.
	for _, k := range []string{"GRAALGO_TRUFFLE_CLASSES", "GRAALGO_INTEROP_CLASSES"} {
		if v := os.Getenv(k); v != "" {
			base = append(base, k+"="+v)
		}
	}
	return overrideEnv(base, overrides)
}
