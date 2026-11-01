package p3frontend

// Pins for the toolchain-location fix (CI run 41267301466, branch
// wt/main/rem-review-20261031-p1-3): the loader and the gc oracle must
// resolve the Go toolchain wherever it is actually installed, not only
// under $HOME/tools. On CI runners Go lives under the hostedtoolcache,
// so the old hardcoded $HOME/tools/go-1.27.1 and $HOME/tools/go/bin/go
// paths made seven tests fail with "pinned GOROOT src dir missing" and
// "go toolchain not found" before any product code ran.

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// The build toolchain's GOROOT is the fallback when neither
// GRAALGO_GOROOT nor the developer install exists: with an empty HOME
// the loader must still land on a GOROOT whose src dir exists.
func TestGorootResolutionFallsBackToBuildToolchain(t *testing.T) {
	gr := runtime.GOROOT()
	if gr == "" || !isDir(filepath.Join(gr, "src")) {
		t.Skip("build toolchain has no usable GOROOT on this machine")
	}
	emptyHome := t.TempDir()
	t.Setenv("HOME", emptyHome)
	t.Setenv("GRAALGO_GOROOT", "")
	if got := NewLoader().GOROOT(); got != gr {
		t.Fatalf("GOROOT() = %q, want build toolchain %q", got, gr)
	}
}

// The developer install keeps precedence over the build toolchain when
// it is present, preserving local behavior.
func TestGorootPrefersPinnedInstall(t *testing.T) {
	home := t.TempDir()
	pinned := filepath.Join(home, "tools", "go-1.27.1")
	if err := os.MkdirAll(filepath.Join(pinned, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("GRAALGO_GOROOT", "")
	if got := NewLoader().GOROOT(); got != pinned {
		t.Fatalf("GOROOT() = %q, want pinned install %q", got, pinned)
	}
}

// An explicit GRAALGO_GOROOT always wins, even without a src dir: the
// environment override is trusted, as before.
func TestGorootEnvOverrideWins(t *testing.T) {
	scratch := t.TempDir()
	t.Setenv("GRAALGO_GOROOT", scratch)
	if got := NewLoader().GOROOT(); got != scratch {
		t.Fatalf("GOROOT() = %q, want override %q", got, scratch)
	}
}

// goTool must resolve through the whole chain, not just the legacy
// path: with GOROOT pointing at a dir without bin/go and a `go` on
// PATH, the PATH binary is returned; with a bin/go under the resolved
// GOROOT, that one wins over PATH (oracle and stdlib sources stay on
// the same install).
func TestGoToolFallbackChain(t *testing.T) {
	stub := func(dir string) string {
		p := filepath.Join(dir, "go")
		if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Case 1: GOROOT without bin/go -> PATH stub.
	gorootDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(gorootDir, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	pathDir := t.TempDir()
	pathGo := stub(pathDir)
	t.Setenv("GRAALGO_GOROOT", gorootDir)
	t.Setenv("PATH", pathDir)
	if got := goTool(t); got != pathGo {
		t.Fatalf("goTool() = %q, want PATH stub %q", got, pathGo)
	}

	// Case 2: bin/go under the resolved GOROOT beats PATH.
	gorootBin := filepath.Join(gorootDir, "bin")
	if err := os.MkdirAll(gorootBin, 0755); err != nil {
		t.Fatal(err)
	}
	gorootGo := stub(gorootBin)
	if got := goTool(t); got != gorootGo {
		t.Fatalf("goTool() = %q, want GOROOT stub %q", got, gorootGo)
	}
}
