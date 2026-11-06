package main

// F3: runtime interception gates must key on the resolved package PATH,
// not the package NAME. A user package named `runtime` (import path
// f3test<n>/runtime, package clause `runtime`) must NOT be rewritten to
// the runtime intrinsics: its Version/NumCPU/GOMAXPROCS/NumGoroutine/
// Gosched/GC/KeepAlive/SetFinalizer/ReadMemStats/GOROOT/Stack/Goexit
// calls are ordinary calls. Only the real overlay runtime (import path
// "runtime", types.NewPackage("runtime", "runtime") in syntheticRuntimePackage)
// is intercepted.
//
// Red-green: with the parent export.go the two negative tests fail — the
// intrinsic wire nodes (runtimeversion, runtimenumcpu, runtimegomaxprocs,
// runtimenumgoroutine, runtimegosched, runtimereadmemstats, runtimegoroot,
// runtimestack), the goexit marker, and the gcinterop markers appear, and
// the ordinary-call counts drop — while the positive control passes
// pre- and post-fix.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeRuntimeSeq mints a fresh import path per test: the disk-package
// caches (diskDirCache/diskCache) are process-global and keyed by import
// path, so reusing one path across tests would resolve to the first
// test's GOPATH directory.
var fakeRuntimeSeq atomic.Int32

// fakeRuntimeSrc is the user package named `runtime`: same package name
// and same intercepted function names as the real runtime, but an
// unrelated import path, so Pkg().Name() == "runtime" while
// Pkg().Path() != "runtime". All functions are ordinary Go with bodies.
const fakeRuntimeSrc = `package runtime

type MemStats struct {
	Alloc uint64
}

func Version() string { return "fake" }
func NumCPU() int { return 1 }
func GOMAXPROCS(n int) int { return n }
func NumGoroutine() int { return 1 }
func Gosched() {}
func GC() {}
func KeepAlive(x any) {}
func SetFinalizer(obj any, finalizer any) {}
func ReadMemStats(m *MemStats) {}
func Goexit() {}
func GOROOT() string { return "" }
func Stack(buf []byte, all bool) int { return 0 }
`

// writeFakeRuntimePackage installs the fake runtime package under a
// fresh GOPATH (GOPATH/src/<importPath>) and returns the import path.
// The program under test imports it by that path.
func writeFakeRuntimePackage(t *testing.T) string {
	t.Helper()
	n := fakeRuntimeSeq.Add(1)
	importPath := fmt.Sprintf("f3test%d/runtime", n)
	gopath := t.TempDir()
	dir := filepath.Join(gopath, "src", importPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "runtime.go"), []byte(fakeRuntimeSrc), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPATH", gopath)
	return importPath
}

// TestUserPackageNamedRuntimeNotRewritten: every intercepted runtime name
// called on the user package named `runtime` exports as an ordinary call:
// no intrinsic wire node, no goexit marker, no gcinterop marker, and the
// ordinary-call count is exactly the number of call sites (14: one per
// name, Gosched in all three statement positions).
func TestUserPackageNamedRuntimeNotRewritten(t *testing.T) {
	imp := writeFakeRuntimePackage(t)
	src := `package main

import "` + imp + `"

func main() {
	_ = runtime.Version()
	_ = runtime.NumCPU()
	_ = runtime.GOMAXPROCS(2)
	_ = runtime.NumGoroutine()
	runtime.Gosched()
	defer runtime.Gosched()
	go runtime.Gosched()
	runtime.GC()
	runtime.KeepAlive(1)
	x := 1
	runtime.SetFinalizer(&x, func(*int) {})
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	_ = runtime.GOROOT()
	var buf [64]byte
	_ = runtime.Stack(buf[:], false)
	runtime.Goexit()
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with user package named runtime: %v", err)
	}
	for _, kind := range []string{
		`"kind": "runtimeversion"`,
		`"kind": "runtimenumcpu"`,
		`"kind": "runtimegomaxprocs"`,
		`"kind": "runtimenumgoroutine"`,
		`"kind": "runtimegosched"`,
		`"kind": "runtimereadmemstats"`,
		`"kind": "runtimegoroot"`,
		`"kind": "runtimestack"`,
	} {
		if strings.Contains(out, kind) {
			t.Errorf("user runtime call was rewritten to intrinsic %s", kind)
		}
	}
	if strings.Contains(out, `"goexit": true`) {
		t.Errorf("user runtime.Goexit was rewritten to the goexit marker")
	}
	if strings.Contains(out, `"gcinterop"`) {
		t.Errorf("user runtime GC/KeepAlive/SetFinalizer was rewritten to a gcinterop marker")
	}
	// 14 call sites, each exactly one ordinary "call" node.
	if n := strings.Count(out, `"kind": "call"`); n != 14 {
		t.Errorf("exported JSON has %d ordinary call nodes, want 14", n)
	}
}

// TestUserPackageNamedRuntimeAliasedGoexit: an aliased import of the user
// package named `runtime` (rt.Goexit()) is likewise an ordinary call,
// not the goexit marker. The Goexit block has no identifier-spelling
// pre-filter, so the alias exercises the Path() gate alone.
func TestUserPackageNamedRuntimeAliasedGoexit(t *testing.T) {
	imp := writeFakeRuntimePackage(t)
	src := `package main

import rt "` + imp + `"

func main() {
	rt.Goexit()
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with aliased user runtime.Goexit: %v", err)
	}
	if strings.Contains(out, `"goexit": true`) {
		t.Errorf("aliased user runtime.Goexit was rewritten to the goexit marker")
	}
	if n := strings.Count(out, `"kind": "call"`); n != 1 {
		t.Errorf("exported JSON has %d ordinary call nodes, want 1", n)
	}
}

// TestRealRuntimeStillIntercepts: positive control — the same 14 calls
// against the real overlay runtime (import path "runtime") still lower
// to the intrinsic wire nodes, the goexit marker, and the gcinterop
// markers, with the exact counts. This passes pre- and post-fix.
func TestRealRuntimeStillIntercepts(t *testing.T) {
	src := `package main

import "runtime"

func main() {
	_ = runtime.Version()
	_ = runtime.NumCPU()
	_ = runtime.GOMAXPROCS(2)
	_ = runtime.NumGoroutine()
	runtime.Gosched()
	defer runtime.Gosched()
	go runtime.Gosched()
	runtime.GC()
	runtime.KeepAlive(1)
	x := 1
	runtime.SetFinalizer(&x, func(*int) {})
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	_ = runtime.GOROOT()
	var buf [64]byte
	_ = runtime.Stack(buf[:], false)
	runtime.Goexit()
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with real runtime: %v", err)
	}
	for kind, want := range map[string]int{
		`"kind": "runtimeversion"`:      1,
		`"kind": "runtimenumcpu"`:       1,
		`"kind": "runtimegomaxprocs"`:   1,
		`"kind": "runtimenumgoroutine"`: 1,
		`"kind": "runtimegosched"`:      3,
		`"kind": "runtimereadmemstats"`: 1,
		`"kind": "runtimegoroot"`:       1,
		`"kind": "runtimestack"`:        1,
	} {
		if n := strings.Count(out, kind); n != want {
			t.Errorf("exported JSON has %d %s nodes, want %d", n, kind, want)
		}
	}
	if n := strings.Count(out, `"goexit": true`); n != 1 {
		t.Errorf("exported JSON has %d goexit markers, want 1", n)
	}
	if n := strings.Count(out, `"gcinterop"`); n != 3 {
		t.Errorf("exported JSON has %d gcinterop markers, want 3 (GC, KeepAlive, SetFinalizer)", n)
	}
}
