package main

// exec_test.go — launcher-discovery tests for the M8 fuzzer.
//
// graalgoRunLoc feeds the harness-error verdict chain (T5): if discovery
// pointed at a stale path, every seed would classify HARNESS_ERROR. These
// tests pin the precedence rules — explicit override first, checkout
// walk-up second, historical default last.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// $GRAALGO_RUN wins when set — an explicit override must never be shadowed
// by walk-up discovery.
func TestGraalgoRunLocEnvPrecedence(t *testing.T) {
	t.Setenv("GRAALGO_RUN", "/explicit/graalgo-run")
	if got := graalgoRunLoc(); got != "/explicit/graalgo-run" {
		t.Fatalf("got %q, want the explicit $GRAALGO_RUN override", got)
	}
}

// With no override and no enclosing checkout, the historical default
// checkout path is returned. (From a bare temp dir the walk-up finds
// nothing, so this deterministically exercises the fallback leg.)
func TestGraalgoRunLocFallback(t *testing.T) {
	t.Setenv("GRAALGO_RUN", "")
	t.Chdir(t.TempDir())
	got := graalgoRunLoc()
	wantSuffix := filepath.Join("internal", "backend", "graalgo-run")
	if !strings.HasSuffix(got, wantSuffix) {
		t.Fatalf("got %q, want suffix %q", got, wantSuffix)
	}
	if home := os.Getenv("HOME"); home == "" || !strings.HasPrefix(got, home) {
		t.Fatalf("got %q, want the $HOME-based historical default", got)
	}
}
