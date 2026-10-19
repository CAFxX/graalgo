package main

// exec.go — backend execution for the M8 fuzzer.
//
// Two backends: stock gc (~/tools/go) and graalgo-run
// (internal/backend/graalgo-run, the Phase-1 backend launcher script).
// Each run captures build success, exit code, stdout, stderr.

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

var goBin = os.ExpandEnv("$HOME/tools/go/bin/go")

// repoRoot resolves the graalgo repo root: the GR env var wins; else walk
// up from this source file (tools/fuzz/*.go -> tools -> repo); else
// $HOME/workspace/graalgo as a last resort.
func repoRoot() string {
	if gr := os.Getenv("GR"); gr != "" {
		return gr
	}
	if _, src, _, ok := runtime.Caller(0); ok {
		if abs, err := filepath.Abs(src); err == nil {
			root := filepath.Dir(filepath.Dir(filepath.Dir(abs)))
			launcher := filepath.Join(root, "internal", "backend", "graalgo-run")
			if fi, err := os.Stat(launcher); err == nil && !fi.IsDir() {
				return root
			}
		}
	}
	return os.ExpandEnv("$HOME/workspace/graalgo")
}

var graalgoRun = filepath.Join(repoRoot(), "internal", "backend", "graalgo-run")

type runResult struct {
	buildOK   bool
	buildCode int
	buildErr  string // stderr of the build/check step
	exit      int
	stdout    string
	stderr    string
	timedOut  bool
}

func runCmd(timeout time.Duration, dir, name string, env []string, args ...string) (int, string, string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	var so, se bytes.Buffer
	cmd.Stdout = &so
	cmd.Stderr = &se
	err := cmd.Run()
	timedOut := ctx.Err() == context.DeadlineExceeded
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = -1
		}
	}
	return code, so.String(), se.String(), timedOut
}

// runGC builds src with gc and runs the binary.
func runGC(workdir, src string) runResult {
	var r runResult
	bin := filepath.Join(workdir, "gcbin")
	env := []string{"GOTOOLCHAIN=local", "GOMAXPROCS=1", "GOFLAGS=-mod=mod"}
	code, _, se, to := runCmd(60*time.Second, workdir, goBin, env, "build", "-o", bin, src)
	r.buildCode = code
	r.timedOut = to
	if to || code != 0 {
		r.buildOK = false
		r.buildErr = se
		return r
	}
	r.buildOK = true
	code, so, se, to := runCmd(30*time.Second, workdir, bin, env)
	r.exit, r.stdout, r.stderr = code, so, se
	if to {
		r.timedOut = true
	}
	return r
}

// runGraal checks src with graalgo-run --check, then runs it.
func runGraal(workdir, src string) runResult {
	var r runResult
	if fi, err := os.Stat(graalgoRun); err != nil || fi.IsDir() {
		r.buildOK = false
		r.buildErr = "graalgo-run launcher missing: " + graalgoRun +
			" (run `make build` at the repo root)"
		return r
	}
	code, _, se, to := runCmd(90*time.Second, workdir, graalgoRun, nil, "--check", src)
	r.buildCode = code
	r.timedOut = to
	if to || code != 0 {
		r.buildOK = false
		r.buildErr = se
		return r
	}
	r.buildOK = true
	code, so, se, to := runCmd(60*time.Second, workdir, graalgoRun, nil, src)
	r.exit, r.stdout, r.stderr = code, so, se
	if to {
		r.timedOut = true
	}
	return r
}
