package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// captureOutput runs fn with os.Stdout and os.Stderr redirected to a pipe
// and returns everything fn printed on either stream.
func captureOutput(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = w, w
	done := make(chan string, 1)
	go func() {
		var buf strings.Builder
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()
	fn()
	_ = w.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	return <-done
}

// fakeBackendRoot creates a temp dir containing a fake
// internal/backend/graalgo-sync-run launcher running script, so cmdTest
// can be exercised end to end without the real Truffle backend. The
// script runs with /bin/sh; use "exec sleep 3600" for a backend that
// never returns (mirroring the real launcher, which execs the JVM, so
// the process killed on timeout is the backend itself).
func fakeBackendRoot(t *testing.T, script string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "internal", "backend")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(dir, "graalgo-sync-run")
	if err := os.WriteFile(launcher, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

// Regression for NEW-P1-1 (REVIEW-2026-11-06-06): `graalgo test` had no
// timeout and rejected -timeout, so a hung guest test hung the driver
// forever. A guest test that blocks forever must fail with a timeout
// line within the (short, test-overridden) deadline.
func TestCmdTestTimeout(t *testing.T) {
	for _, tc := range []struct {
		name  string
		flags []string
	}{
		{"equals-form", []string{"-timeout=200ms"}},
		{"separate-arg-form", []string{"-timeout", "200ms"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := fakeBackendRoot(t, "exec sleep 3600")
			dir := writeSubtestPkg(t, `package subtest

import "testing"

func TestBlocksForever(t *testing.T) { select {} }
`)
			start := time.Now()
			var code int
			out := captureOutput(t, func() {
				args := append(append([]string{}, tc.flags...), dir)
				code = cmdTest(root, args)
			})
			elapsed := time.Since(start)
			if code != 1 {
				t.Errorf("cmdTest exit = %d, want 1 (package must fail on timeout):\n%s", code, out)
			}
			mustContain(t, out, "panic: test timed out after 200ms")
			mustContain(t, out, "FAIL")
			if elapsed > 30*time.Second {
				t.Errorf("cmdTest took %v, want < 30s (the timeout must kill the hung backend)", elapsed)
			}
		})
	}
}

// -timeout must reject a value time.ParseDuration rejects, with exit 2
// like the other flag errors.
func TestCmdTestTimeoutInvalid(t *testing.T) {
	root := fakeBackendRoot(t, "exit 0")
	dir := writeSubtestPkg(t, `package subtest

import "testing"

func TestTrivial(t *testing.T) {}
`)
	var code int
	out := captureOutput(t, func() {
		code = cmdTest(root, []string{"-timeout=abc", dir})
	})
	if code != 2 {
		t.Errorf("cmdTest exit = %d, want 2 (invalid -timeout):\n%s", code, out)
	}
	mustContain(t, out, "invalid -timeout")
}

// -timeout=0 disables the timeout (go test semantics): a fast backend
// must pass and -timeout=0 must be accepted.
func TestCmdTestTimeoutZeroDisables(t *testing.T) {
	root := fakeBackendRoot(t, "exit 0")
	dir := writeSubtestPkg(t, `package subtest

import "testing"

func TestTrivial(t *testing.T) {}
`)
	var code int
	out := captureOutput(t, func() {
		code = cmdTest(root, []string{"-timeout=0", dir})
	})
	if code != 0 {
		t.Errorf("cmdTest exit = %d, want 0 (fast backend, timeout disabled):\n%s", code, out)
	}
	mustContain(t, out, "ok  \t")
}

// graalgoRunCtx must kill a hung backend process when the context
// expires and report it as a timeout — the launcher execs the JVM
// directly, so killing the child kills the backend (the fake launcher
// mirrors that with exec sleep).
func TestGraalgoRunCtxKillsHungBackend(t *testing.T) {
	root := fakeBackendRoot(t, "exec sleep 3600")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	code, timedOut := graalgoRunCtx(ctx, root, nil)
	elapsed := time.Since(start)
	if !timedOut {
		t.Errorf("timedOut = false, want true (hung backend must be reported as a timeout)")
	}
	if code == 0 {
		t.Errorf("code = 0, want non-zero for a backend killed by the timeout")
	}
	if elapsed > 30*time.Second {
		t.Errorf("graalgoRunCtx took %v, want < 30s (the timeout must kill the hung backend)", elapsed)
	}
}

// graalgoRunCtx must pass through a fast backend's exit code and never
// report a timeout when the context is still live.
func TestGraalgoRunCtxFastAndFailing(t *testing.T) {
	for _, tc := range []struct {
		name   string
		script string
		want   int
	}{
		{"exit-0", "exit 0", 0},
		{"exit-3", "exit 3", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := fakeBackendRoot(t, tc.script)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			code, timedOut := graalgoRunCtx(ctx, root, nil)
			if timedOut {
				t.Errorf("timedOut = true, want false (backend exited on its own)")
			}
			if code != tc.want {
				t.Errorf("code = %d, want %d", code, tc.want)
			}
		})
	}
}
