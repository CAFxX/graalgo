package main

// exec.go — backend execution for the M8 fuzzer.
//
// Two backends: stock gc ($GO, default ~/tools/go/bin/go) and the graalgo
// product launcher ($GRAALGO_SYNC_RUN, default
// <repo>/internal/backend/graalgo-sync-run, found by walking up from the
// working directory). Each run captures build success, exit code, stdout,
// stderr.

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

var goBin = envOr("GO", os.ExpandEnv("$HOME/tools/go/bin/go"))
var graalgoRun = envOr("GRAALGO_SYNC_RUN", findLauncher())

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// findLauncher walks up from the working directory to the repo's product
// launcher. An unresolvable launcher is returned as a non-existent path:
// every run then fails to start and is classified INFRA, never DIVERGED.
func findLauncher() string {
	dir, err := os.Getwd()
	if err != nil {
		return "graalgo-sync-run-not-found"
	}
	for {
		p := filepath.Join(dir, "internal", "backend", "graalgo-sync-run")
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "graalgo-sync-run-not-found"
		}
		dir = parent
	}
}

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
