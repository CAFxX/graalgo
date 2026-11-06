package main

// F3 (independent codebase review 2026-11-06, P1): the runtime intrinsic
// interception in export.go was name-keyed — obj.Pkg().Name() == "runtime"
// (exportCall's Goexit block and main intrinsic block, plus isGoschedCall
// for the statement/defer/go positions) — never Path(). A user package
// declared `package runtime` at any other import path (e.g.
// example.com/runtime) defining Version/NumCPU/GOMAXPROCS had its calls
// silently rewritten to graalgo wire nodes instead of calling the user's
// function. The gate is now path-keyed (obj.Pkg().Path() == "runtime",
// matching the synthetic overlay package built by
// types.NewPackage("runtime", "runtime")); these tests pin the negative
// case end to end, plus the positive control that the real runtime still
// intercepts.
//
// Red-green: before the fix, TestUserPackageNamedRuntimeNotRewritten fails
// (Version lowers to "runtimeversion", Gosched to "runtimegosched",
// Goexit to the goexit marker) and TestUserPackageNamedRuntimeAliasedGoexit
// fails (aliased rt.Goexit() hits the goexit marker); after the fix both
// pass, and TestRealRuntimeStillIntercepts keeps passing throughout.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// userRuntimeFixtureSource is a user package that happens to be named
// "runtime". Its functions mirror the real runtime's signatures for the
// intercepted names, so the only thing distinguishing it from the overlay
// is the import path: example.com/runtime vs "runtime".
const userRuntimeFixtureSource = `package runtime

// Version reports this package's own version, not the toolchain's.
func Version() string { return "user-runtime-v1" }

// NumCPU reports this package's own CPU count, not the host's.
func NumCPU() int { return 7 }

// GOMAXPROCS reports this package's own value, not the scheduler's.
func GOMAXPROCS(n int) int { return n + 100 }

// NumGoroutine reports this package's own count, not the scheduler's.
func NumGoroutine() int { return 3 }

// GC is this package's own collection hook, not the runtime's.
func GC() {}

// KeepAlive is this package's own hook, not the runtime's.
func KeepAlive(x any) {}

// Gosched is this package's own yield, not the scheduler's.
func Gosched() {}

// Goexit is this package's own exit, not the runtime's.
func Goexit() {}
`

// writeUserRuntimeFixture lays out the fixture as a GOPATH package at
// example.com/runtime and points GOPATH at it (test-scoped via t.Setenv,
// so no other test or the developer's GOPATH is affected). The frontend's
// disk importer resolves example.com/runtime through the GOPATH probe.
func writeUserRuntimeFixture(t *testing.T) {
	t.Helper()
	gopath := filepath.Join(t.TempDir(), "gopath")
	dir := filepath.Join(gopath, "src", "example.com", "runtime")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "runtime.go"), []byte(userRuntimeFixtureSource), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPATH", gopath)
}

// exportedCallInfo is the structural summary of an exported frontend/v1
// document relevant to the interception gate: which wire-node kinds
// appear, which functions are called as ordinary calls (fun ident name),
// and whether any call carries the goexit or gcinterop intrinsic marker.
type exportedCallInfo struct {
	kinds     map[string]bool
	called    map[string]int
	goexit    bool
	gcinterop map[string]int
}

// inspectExportedCalls decodes out as JSON and walks the whole document.
func inspectExportedCalls(t *testing.T, out string) exportedCallInfo {
	t.Helper()
	var doc any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("exported output is not valid JSON: %v", err)
	}
	info := exportedCallInfo{
		kinds:     map[string]bool{},
		called:    map[string]int{},
		gcinterop: map[string]int{},
	}
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if kind, ok := x["kind"].(string); ok {
				info.kinds[kind] = true
				if kind == "call" {
					if fields, ok := x["fields"].(map[string]any); ok {
						if gv, ok := fields["goexit"].(bool); ok && gv {
							info.goexit = true
						}
						if gv, ok := fields["gcinterop"].(string); ok {
							info.gcinterop[gv]++
						}
						if fun, ok := fields["fun"].(map[string]any); ok && fun["kind"] == "ident" {
							if ff, ok := fun["fields"].(map[string]any); ok {
								if name, ok := ff["name"].(string); ok {
									info.called[name]++
								}
							}
						}
					}
				}
			}
			for _, vv := range x {
				walk(vv)
			}
		case []any:
			for _, vv := range x {
				walk(vv)
			}
		}
	}
	walk(doc)
	return info
}

// runtimeIntrinsicKinds are the wire-node kinds the interception gate may
// emit for the real runtime. None of them may appear when the callee is a
// user package merely named "runtime".
var runtimeIntrinsicKinds = []string{
	"runtimeversion", "runtimenumcpu", "runtimegomaxprocs",
	"runtimenumgoroutine", "runtimegosched", "runtimereadmemstats",
	"runtimegoroot", "runtimestack", "runtimecallers",
	"runtimecallersframes", "runtimefuncforpc",
}

// userRuntimeMain exercises every intercepted name against the user
// package: expression position (Version/NumCPU/GOMAXPROCS/NumGoroutine),
// the gcinterop pair (GC/KeepAlive), and all three Gosched statement
// positions (bare, defer, go), plus Goexit.
const userRuntimeMain = `package main

import "example.com/runtime"

func main() {
	v := runtime.Version()
	n := runtime.NumCPU()
	m := runtime.GOMAXPROCS(2)
	g := runtime.NumGoroutine()
	runtime.GC()
	runtime.KeepAlive(v)
	_, _, _, _ = v, n, m, g
	runtime.Gosched()
	defer runtime.Gosched()
	go runtime.Gosched()
	runtime.Goexit()
}
`

// TestUserPackageNamedRuntimeNotRewritten: calls into a user package named
// "runtime" must lower to ordinary calls, never to runtime wire nodes.
func TestUserPackageNamedRuntimeNotRewritten(t *testing.T) {
	writeUserRuntimeFixture(t)
	out, err := ExportFile(writeSyncProg(t, userRuntimeMain))
	if err != nil {
		t.Fatalf("ExportFile with user runtime package: %v", err)
	}
	info := inspectExportedCalls(t, out)
	for _, kind := range runtimeIntrinsicKinds {
		if info.kinds[kind] {
			t.Errorf("user runtime call lowered to intrinsic wire node %q (F3: name-keyed gate)", kind)
		}
	}
	if info.goexit {
		t.Errorf("user runtime.Goexit() lowered to the goexit marker (F3: name-keyed gate)")
	}
	if len(info.gcinterop) != 0 {
		t.Errorf("user runtime GC/KeepAlive lowered with gcinterop markers %v (F3: name-keyed gate)", info.gcinterop)
	}
	for name, want := range map[string]int{
		"Version": 1, "NumCPU": 1, "GOMAXPROCS": 1, "NumGoroutine": 1,
		"GC": 1, "KeepAlive": 1, "Gosched": 3, "Goexit": 1,
	} {
		if got := info.called[name]; got != want {
			t.Errorf("ordinary calls to user runtime.%s: got %d, want %d", name, got, want)
		}
	}
}

// TestUserPackageNamedRuntimeAliasedGoexit: the Goexit block accepts any
// import ident (rt.Goexit() is intercepted for the real runtime), so the
// alias alone must not let a user package's Goexit through the gate —
// only the import path discriminates.
func TestUserPackageNamedRuntimeAliasedGoexit(t *testing.T) {
	writeUserRuntimeFixture(t)
	src := `package main

import rt "example.com/runtime"

func main() {
	rt.Goexit()
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with aliased user runtime package: %v", err)
	}
	info := inspectExportedCalls(t, out)
	if info.goexit {
		t.Errorf("aliased user rt.Goexit() lowered to the goexit marker (F3: name-keyed gate)")
	}
	if got := info.called["Goexit"]; got != 1 {
		t.Errorf("ordinary calls to user rt.Goexit: got %d, want 1", got)
	}
}

// TestRealRuntimeStillIntercepts: positive control — the real runtime
// package must still lower to its wire nodes after the gate becomes
// path-keyed. Guards against "fixing" F3 by deleting the interception.
func TestRealRuntimeStillIntercepts(t *testing.T) {
	src := `package main

import "runtime"

func main() {
	v := runtime.Version()
	n := runtime.NumCPU()
	m := runtime.GOMAXPROCS(2)
	g := runtime.NumGoroutine()
	runtime.GC()
	runtime.KeepAlive(v)
	_, _, _, _ = v, n, m, g
	runtime.Gosched()
	defer runtime.Gosched()
	go runtime.Gosched()
	runtime.Goexit()
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with real runtime: %v", err)
	}
	info := inspectExportedCalls(t, out)
	for _, kind := range []string{
		"runtimeversion", "runtimenumcpu", "runtimegomaxprocs",
		"runtimenumgoroutine", "runtimegosched",
	} {
		if !info.kinds[kind] {
			t.Errorf("real runtime call did not lower to intrinsic wire node %q", kind)
		}
	}
	if !info.goexit {
		t.Errorf("real runtime.Goexit() did not lower to the goexit marker")
	}
	for _, name := range []string{"GC", "KeepAlive"} {
		if info.gcinterop[name] == 0 {
			t.Errorf("real runtime.%s() did not lower with the gcinterop marker", name)
		}
	}
	// The real runtime's functions are rewritten, never called directly.
	for _, name := range []string{"Version", "NumCPU", "GOMAXPROCS", "NumGoroutine", "Gosched", "Goexit"} {
		if got := info.called[name]; got != 0 {
			t.Errorf("real runtime.%s lowered to an ordinary call (%d), want rewritten to a wire node", name, got)
		}
	}
}
