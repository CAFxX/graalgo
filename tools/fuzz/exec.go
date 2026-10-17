package main

// exec.go — backend execution for the M8 fuzzer.
//
// Two backends: stock gc (~/tools/go) and graalgo-run
// (internal/backend/graalgo-run in the current layout).
// Each run captures build success, exit code, stdout, stderr, and whether
// the backend binary itself failed to start (harness error — never a
// divergence signal).

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

var goBin = os.ExpandEnv("$HOME/tools/go/bin/go")

// graalgoRunLoc locates the graalgo-run launcher without hard-coding one
// checkout: $GRAALGO_RUN wins if set; otherwise walk up from the working
// directory to the enclosing checkout's internal/backend/graalgo-run;
// otherwise fall back to the historical default checkout path.
func graalgoRunLoc() string {
	if p := os.Getenv("GRAALGO_RUN"); p != "" {
		return p
	}
	if wd, err := os.Getwd(); err == nil {
		d := wd
		for {
			cand := filepath.Join(d, "internal", "backend", "graalgo-run")
			if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
				return cand
			}
			parent := filepath.Dir(d)
			if parent == d {
				break
			}
			d = parent
		}
	}
	return os.ExpandEnv("$HOME/workspace/graalgo/internal/backend/graalgo-run")
}

var graalgoRun = graalgoRunLoc()

type runResult struct {
	buildOK    bool
	buildCode  int
	buildErr   string // stderr of the build/check step
	harnessErr bool   // the backend binary itself failed to start
	exit       int
	stdout     string
	stderr     string
	timedOut   bool
}

// runCmd runs name with args in dir. code is -1 and failedToStart is true
// when the process never started (missing/unexecutable binary: *exec.Error
// rather than *exec.ExitError); a non-zero exit from a started process
// yields failedToStart == false.
func runCmd(timeout time.Duration, dir, name string, env []string, args ...string) (code int, stdout, stderr string, timedOut, failedToStart bool) {
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
	timedOut = ctx.Err() == context.DeadlineExceeded
	code = 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = -1
			failedToStart = true
		}
	}
	return code, so.String(), se.String(), timedOut, failedToStart
}

// runGC builds src with gc and runs the binary.
func runGC(workdir, src string) runResult {
	var r runResult
	bin := filepath.Join(workdir, "gcbin")
	env := []string{"GOTOOLCHAIN=local", "GOMAXPROCS=1", "GOFLAGS=-mod=mod"}
	code, _, se, to, fts := runCmd(60*time.Second, workdir, goBin, env, "build", "-o", bin, src)
	r.buildCode = code
	r.timedOut = to
	r.harnessErr = fts
	if to || code != 0 {
		r.buildOK = false
		r.buildErr = se
		return r
	}
	r.buildOK = true
	code, so, se, to, fts := runCmd(30*time.Second, workdir, bin, env)
	r.exit, r.stdout, r.stderr = code, so, se
	r.harnessErr = r.harnessErr || fts
	if to {
		r.timedOut = true
	}
	return r
}

// runGraal checks src with graalgo-run --check, then runs it.
func runGraal(workdir, src string) runResult {
	var r runResult
	code, _, se, to, fts := runCmd(90*time.Second, workdir, graalgoRun, nil, "--check", src)
	r.buildCode = code
	r.timedOut = to
	r.harnessErr = fts
	if to || code != 0 {
		r.buildOK = false
		r.buildErr = se
		return r
	}
	r.buildOK = true
	code, so, se, to, fts := runCmd(60*time.Second, workdir, graalgoRun, nil, src)
	r.exit, r.stdout, r.stderr = code, so, se
	r.harnessErr = r.harnessErr || fts
	if to {
		r.timedOut = true
	}
	return r
}
