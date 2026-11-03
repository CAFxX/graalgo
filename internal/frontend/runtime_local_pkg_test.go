package main

// REVIEW-2026-11-03-16 L-K (P1-F2): runtime intrinsics must key on the
// import PATH, never the package NAME. A third-party/local package named
// "runtime" exposing Gosched/Goexit/GC/SetFinalizer/KeepAlive must NOT be
// rewritten to the graalgo runtime intrinsics: its calls lower as
// ordinary calls to its own bodies. Red-green: before the fix, the
// Gosched/Goexit/GC calls below export as runtimegosched/goexit/
// gcinterop wire nodes (the Name() gates matched); after the fix the
// export contains none of those markers.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLocalRuntimePackageNotRewritten stages a fake module whose
// subpackage is named "runtime" (path example.com/fakelocalruntime/
// runtime, NOT "runtime") and exports a main program calling its
// Gosched (statement + defer + go positions), GC, SetFinalizer,
// KeepAlive, and Goexit. None of those calls may lower to a graalgo
// runtime intrinsic.
func TestLocalRuntimePackageNotRewritten(t *testing.T) {
	modCache := t.TempDir()
	rtDir := filepath.Join(modCache, "example.com", "fakelocalruntime@v1.0.0", "runtime")
	if err := os.MkdirAll(rtDir, 0755); err != nil {
		t.Fatal(err)
	}
	rtSrc := `package runtime

var GoschedCalled, GoexitCalled, GCCalled bool

func Gosched() { GoschedCalled = true }
func Goexit()  { GoexitCalled = true }
func GC()      { GCCalled = true }
func SetFinalizer(obj, finalizer any) {}
func KeepAlive(x any)                  {}
`
	if err := os.WriteFile(filepath.Join(rtDir, "runtime.go"), []byte(rtSrc), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOMODCACHE", modCache)

	src := `package main

import "example.com/fakelocalruntime/runtime"

func main() {
	runtime.Gosched()
	defer runtime.Gosched()
	go runtime.Gosched()
	runtime.GC()
	runtime.SetFinalizer(nil, nil)
	runtime.KeepAlive(nil)
	runtime.Goexit()
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with local runtime package: %v", err)
	}
	for _, marker := range []string{`"runtimegosched"`, `"goexit": true`, `"gcinterop"`} {
		if strings.Contains(out, marker) {
			t.Errorf("local runtime call rewritten to intrinsic: export contains %s", marker)
		}
	}
}
