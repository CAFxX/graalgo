package main

// exec.go — backend execution for the M8 fuzzer.
//
// Two backends: stock gc (~/tools/go) and graalgo-run
// (internal/backend/graalgo-run, resolved at startup — see
// resolveGraalgoRun). Each run captures build success, exit code, stdout,
// stderr.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

var goBin = os.ExpandEnv("$HOME/tools/go/bin/go")

// graalgoRun is the resolved path to the live graalgo-run launcher
// (internal/backend/graalgo-run). It is set once at startup by cmdRun /
// cmdRegress via resolveGraalgoRun; it must never be used while empty — a
// missing backend is a configuration error, reported loudly at startup,
// not a per-seed divergence (see runGraal).
var graalgoRun string

// graalgoRunRel is the launcher path relative to the repo root.
const graalgoRunRel = "internal/backend/graalgo-run"

// isExecutableLauncher reports whether path is an existing executable file.
func isExecutableLauncher(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0
}

// findLauncherInAncestors walks up from dir looking for relPath
// (e.g. internal/backend/graalgo-run). It returns the first executable hit.
func findLauncherInAncestors(dir, relPath string) string {
	for {
		cand := filepath.Join(dir, relPath)
		if isExecutableLauncher(cand) {
			return cand
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// resolveGraalgoRun resolves the graalgo-run launcher to invoke, in order:
//
//  1. explicit --graalgo-run flag value (must exist and be executable);
//  2. $GRAALGO_RUN (same check);
//  3. walk-up from the fuzz binary's directory, then from the CWD, looking
//     for internal/backend/graalgo-run (covers `go run ./tools/fuzz` and a
//     binary built anywhere inside a checkout);
//  4. the legacy $HOME/workspace/graalgo checkout location.
//
// An explicit value that does not resolve is an error (fail fast — a typo'd
// backend path must not silently become per-seed failures). When nothing
// resolves, the error names the flag and the env var so the operator can
// point the fuzzer at a built launcher.
func resolveGraalgoRun(explicit string) (string, error) {
	if explicit != "" {
		if isExecutableLauncher(explicit) {
			abs, err := filepath.Abs(explicit)
			if err != nil {
				return "", err
			}
			return abs, nil
		}
		return "", fmt.Errorf("--graalgo-run %q is not an executable file", explicit)
	}
	if env := os.Getenv("GRAALGO_RUN"); env != "" {
		if isExecutableLauncher(env) {
			return env, nil
		}
		return "", fmt.Errorf("$GRAALGO_RUN %q is not an executable file", env)
	}
	if exe, err := os.Executable(); err == nil {
		if hit := findLauncherInAncestors(filepath.Dir(exe), graalgoRunRel); hit != "" {
			return hit, nil
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		if hit := findLauncherInAncestors(cwd, graalgoRunRel); hit != "" {
			return hit, nil
		}
	}
	legacy := os.ExpandEnv("$HOME/workspace/graalgo/" + graalgoRunRel)
	if isExecutableLauncher(legacy) {
		return legacy, nil
	}
	return "", errors.New("no graalgo-run launcher found: pass --graalgo-run PATH or set $GRAALGO_RUN (looked for " + graalgoRunRel + " above the fuzz binary and the working directory, and at " + legacy + ")")
}

// initGraalgoRun resolves the backend and fatals with a clear message when
// it cannot be found. Call after flag parsing, before any seed runs.
func initGraalgoRun(explicit string) {
	resolved, err := resolveGraalgoRun(explicit)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fuzz: cannot find graalgo-run backend:", err)
		os.Exit(1)
	}
	graalgoRun = resolved
	fmt.Fprintln(os.Stderr, "fuzz: graalgo-run backend:", resolved)
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
// Requires initGraalgoRun to have resolved the backend; a call with an
// unresolved backend is a programming error, not a seed verdict.
func runGraal(workdir, src string) runResult {
	var r runResult
	if graalgoRun == "" {
		r.buildOK = false
		r.buildCode = -1
		r.buildErr = "internal error: graalgo-run backend not resolved (initGraalgoRun not called)"
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
