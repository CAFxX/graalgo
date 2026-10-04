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
	oc := Outcome{Program: prog.Name, Backend: b.Name}
	dir := filepath.Join(workRoot, prog.Name, b.Name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		oc.BuildOK = false
		oc.BuildLog = Normalize(fmt.Sprintf("mkdir workdir: %v", err))
		return oc
	}
	bin := filepath.Join(dir, "prog")
	buildCmd := b.RenderBuild(prog.Main, bin)
	buildCtx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	var blog bytes.Buffer
	cmd := exec.CommandContext(buildCtx, buildCmd[0], buildCmd[1:]...)
	cmd.Env = overrideEnv(os.Environ(), b.Env)
	cmd.Stdout = &blog
	cmd.Stderr = &blog
	if err := cmd.Run(); err != nil {
		oc.BuildOK = false
		// Scrub the workRoot from the echoed command: it is backend- and
		// run-specific, so leaving it in would make two identical build
		// failures compare as different (false divergence signal).
		cmdStr := strings.ReplaceAll(fmt.Sprintf("%v", buildCmd), workRoot, "{WORK}")
		oc.BuildLog = Normalize(fmt.Sprintf("build %s failed: %v\n%s", cmdStr, err, blog.String()))
		return oc
	}
	oc.BuildOK = true

	runDir, err := os.MkdirTemp("", "diffharness-run-*")
	if err != nil {
		oc.BuildLog = Normalize(fmt.Sprintf("mktemp rundir: %v", err))
		return oc
	}
	defer os.RemoveAll(runDir)

	runCmd := b.RenderRun(prog.Main, bin)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.TimeoutSec)*time.Second)
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd = exec.CommandContext(ctx, runCmd[0], runCmd[1:]...)
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
	oc.Stderr = Normalize(stderr.String())
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
	return overrideEnv(base, overrides)
}
