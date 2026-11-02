package main

// Lane F (review 2026-11-02 P2-10): concurrent importers of a disk
// package already being built used to get a spurious "import cycle"
// error. getDiskPackage now single-flights the build: concurrent
// callers wait for the in-flight build and receive its outcome, while
// a true import cycle still fails (via the ancestry stack plus the
// wait-chain walk) instead of deadlocking.

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestDiskCycleDetected pins the wait-chain walk without goroutines:
// a chain back to the requester's stack is a cycle; anything else
// (including a stale edge to a finished build, or a loop in the
// graph itself) is not.
func TestDiskCycleDetected(t *testing.T) {
	mk := func(path string) *diskBuild {
		return &diskBuild{path: path, done: make(chan struct{})}
	}
	// Chain A -> B -> C, all in flight.
	a, b, c := mk("a"), mk("b"), mk("c")
	a.waitsFor, b.waitsFor = b, c
	inflight := map[string]*diskBuild{"a": a, "b": b, "c": c}
	if diskCycleDetected([]string{"x"}, a, inflight) {
		t.Error("chain A->B->C with stack [x] reported a cycle; want no cycle")
	}
	// The chain leads back to the requester's stack: cycle.
	if !diskCycleDetected([]string{"b"}, a, inflight) {
		t.Error("chain A->B with stack [b] did not report a cycle")
	}
	// Two-goroutine A<->B shape: G2 (stack [b]) requests A while
	// G1's A build waits for G2's B build.
	if !diskCycleDetected([]string{"b"}, a, inflight) {
		t.Error("A<->B cross-goroutine cycle not detected")
	}
	// Stale edge: A left the in-flight map (build finished).
	stale := map[string]*diskBuild{"b": b, "c": c}
	if diskCycleDetected([]string{"b"}, a, stale) {
		t.Error("stale start record reported a cycle; want no cycle")
	}
	// Stale middle: B finished, so the chain ends there.
	a2, b2 := mk("a"), mk("b")
	a2.waitsFor = b2
	partial := map[string]*diskBuild{"a": a2}
	if diskCycleDetected([]string{"b"}, a2, partial) {
		t.Error("chain through a finished build reported a cycle; want no cycle")
	}
	// Graph loop A -> B -> A with an unrelated stack: terminates, no cycle.
	la, lb := mk("a"), mk("b")
	la.waitsFor, lb.waitsFor = lb, la
	loop := map[string]*diskBuild{"a": la, "b": lb}
	if diskCycleDetected([]string{"z"}, la, loop) {
		t.Error("graph loop with unrelated stack reported a cycle; want no cycle")
	}
}

// writeGopathPkg writes a GOPATH package dir with the given files.
func writeGopathPkg(t *testing.T, gopath, importPath string, files map[string]string) {
	t.Helper()
	dir := filepath.Join(gopath, "src", importPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

// runWithTimeout runs f and fails the test if it does not finish in d
// (a deadlock in the code under test must fail, not hang the suite).
func runWithTimeout(t *testing.T, d time.Duration, f func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- f() }()
	select {
	case err := <-done:
		return err
	case <-time.After(d):
		t.Fatalf("timed out after %s (deadlock?)", d)
		return nil
	}
}

// TestGetDiskPackageConcurrentCoalesces pins that N goroutines
// importing the same disk package concurrently all succeed with the
// same built package — no spurious "import cycle" for the losers.
func TestGetDiskPackageConcurrentCoalesces(t *testing.T) {
	gopath := t.TempDir()
	t.Setenv("GOPATH", gopath)
	t.Setenv("GOMODCACHE", t.TempDir())
	writeGopathPkg(t, gopath, "lanefcoalesce", map[string]string{
		"a.go": "package lanefcoalesce\n\nfunc A() int { return 1 }\n",
		"b.go": "package lanefcoalesce\n\ntype T struct{ X int }\n\nfunc (t T) Get() int { return t.X }\n",
		"c.go": "package lanefcoalesce\n\nfunc C() string { return \"c\" }\n",
	})
	const n = 32
	err := runWithTimeout(t, 60*time.Second, func() error {
		start := make(chan struct{})
		results := make([]*syntheticPackage, n)
		errs := make([]error, n)
		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				results[i], errs[i] = getDiskPackage("lanefcoalesce", nil)
			}(i)
		}
		close(start)
		wg.Wait()
		for i := 0; i < n; i++ {
			if errs[i] != nil {
				t.Errorf("goroutine %d: getDiskPackage failed: %v", i, errs[i])
			}
		}
		for i := 1; i < n; i++ {
			if results[i] != results[0] {
				t.Errorf("goroutine %d got a different package object than goroutine 0 (duplicate build, not coalesced)", i)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestGetDiskPackageImportCycle pins that a true import cycle between
// two disk packages fails with an import-cycle error instead of
// hanging or recursing.
func TestGetDiskPackageImportCycle(t *testing.T) {
	gopath := t.TempDir()
	t.Setenv("GOPATH", gopath)
	t.Setenv("GOMODCACHE", t.TempDir())
	writeGopathPkg(t, gopath, "cyca", map[string]string{
		"a.go": "package cyca\n\nimport \"cycb\"\n\nfunc G() int { return cycb.F() }\n",
	})
	writeGopathPkg(t, gopath, "cycb", map[string]string{
		"b.go": "package cycb\n\nimport \"cyca\"\n\nfunc F() int { return cyca.G() }\n",
	})
	err := runWithTimeout(t, 60*time.Second, func() error {
		_, err := getDiskPackage("cyca", nil)
		return err
	})
	if err == nil {
		t.Fatal("getDiskPackage(cyca) succeeded for a cyclic import pair; want an import-cycle error")
	}
	if !strings.Contains(err.Error(), "import cycle") {
		t.Fatalf("cyclic import error = %v, want it to mention \"import cycle\"", err)
	}
}
