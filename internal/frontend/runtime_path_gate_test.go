package main

// P1-3 (independent codebase review REVIEW-2026-11-03-12): the runtime
// interception gates in export.go must discriminate on the resolved
// package PATH ("runtime"), not the package NAME, and must not depend on
// the import spelling. Two failure directions, both covered here:
//
// (a) A user package that happens to be named "runtime" but lives at a
// non-"runtime" import path must NOT have its Gosched/Goexit/
// SetFinalizer/KeepAlive/GC/NumCPU/GOMAXPROCS calls replaced by graalgo
// intrinsics — those are ordinary user functions.
// (b) The real runtime package imported under an alias (rt "runtime")
// MUST still intercept — the gates may not consult the identifier
// spelling at all, only the resolved object (plus the outcome-neutral
// structural check that the selector's X is a package name, which keeps
// method calls on runtime-typed receivers out of the package gates).
//
// Red-green: (a) fails before the fix (the user calls lower to
// runtimegosched/goexit/gcinterop/runtimenumcpu/runtimegomaxprocs wire
// nodes) and passes after; (b)'s aliased cases for the 8585 gate and
// isGoschedCall fail before the fix (the aliased calls fall through to
// plain calls / unknown-function errors) and pass after. The Goexit
// aliased case already passed before the fix (its gate never consulted
// the spelling) and is pinned here so the Path change cannot regress it.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeUserRuntimePackage creates a user package named "runtime" at
// example.com/user/runtime under a temp GOPATH and points GOPATH at it,
// so the frontend's disk-import path resolves it. The package name is
// deliberately "runtime" while its path is not "runtime".
func writeUserRuntimePackage(t *testing.T) {
	t.Helper()
	gopath := filepath.Join(t.TempDir(), "gopath")
	pkgDir := filepath.Join(gopath, "src", "example.com", "user", "runtime")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	src := `package runtime

func Gosched() {}

func Goexit() {}

func NumCPU() int { return 1 }

func GOMAXPROCS(n int) int { return n }

func SetFinalizer(obj any, finalizer any) {}

func KeepAlive(x any) {}

func GC() {}
`
	if err := os.WriteFile(filepath.Join(pkgDir, "runtime.go"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPATH", gopath)
	t.Setenv("GOMODCACHE", filepath.Join(gopath, "pkg", "mod"))
}

// TestUserPackageNamedRuntimeKeepsItsOwnFunctions (P1-3a): calls to a
// user package named runtime must export as plain calls, never as
// runtime intrinsics.
func TestUserPackageNamedRuntimeKeepsItsOwnFunctions(t *testing.T) {
	writeUserRuntimePackage(t)
	src := `package main

import "example.com/user/runtime"

func main() {
	runtime.Gosched()
	defer runtime.Gosched()
	go runtime.Gosched()
	runtime.Goexit()
	var obj any
	var fin any
	runtime.SetFinalizer(obj, fin)
	runtime.KeepAlive(obj)
	runtime.GC()
	n := runtime.NumCPU()
	_ = n
	m := runtime.GOMAXPROCS(2)
	_ = m
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with user runtime package: %v", err)
	}
	for _, marker := range []string{
		`"kind": "runtimegosched"`,
		`"goexit": true`,
		`"gcinterop"`,
		`"kind": "runtimenumcpu"`,
		`"kind": "runtimegomaxprocs"`,
	} {
		if strings.Contains(out, marker) {
			t.Errorf("user package named runtime was intercepted: export contains %s", marker)
		}
	}
	// The user calls must still be present as plain calls (the test is
	// not vacuous: a scope error or dropped call would fail above, and
	// a plain call node naming the user function must survive).
	if !strings.Contains(out, `"kind": "call"`) {
		t.Error("export contains no plain call nodes for the user runtime functions")
	}
}

// TestRealRuntimeAliasedImportIntercepts (P1-3b): the real runtime
// package imported under an alias must intercept exactly like the
// canonical spelling, for every gate in scope.
func TestRealRuntimeAliasedImportIntercepts(t *testing.T) {
	body := `{
	%s.Gosched()
	defer %s.Gosched()
	go %s.Gosched()
	%s.Goexit()
	var obj any
	var fin any
	%s.SetFinalizer(obj, fin)
	%s.KeepAlive(obj)
	%s.GC()
	n := %s.NumCPU()
	_ = n
	m := %s.GOMAXPROCS(2)
	_ = m
}
`
	for _, tc := range []struct{ name, qual string }{
		{"canonical", "runtime"},
		{"aliased", "rt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			imp := `"runtime"`
			if tc.name == "aliased" {
				imp = `rt "runtime"`
			}
			src := "package main\n\nimport " + imp + "\n\nfunc main() " +
				strings.ReplaceAll(body, "%s", tc.qual)
			out, err := ExportFile(writeSyncProg(t, src))
			if err != nil {
				t.Fatalf("ExportFile with %s runtime import: %v", tc.name, err)
			}
			if n := strings.Count(out, `"kind": "runtimegosched"`); n != 3 {
				t.Errorf("%s import: %d runtimegosched nodes, want 3 (statement + defer + go)", tc.name, n)
			}
			for _, marker := range []string{
				`"goexit": true`,
				`"gcinterop": "SetFinalizer"`,
				`"gcinterop": "KeepAlive"`,
				`"gcinterop": "GC"`,
				`"kind": "runtimenumcpu"`,
				`"kind": "runtimegomaxprocs"`,
			} {
				if !strings.Contains(out, marker) {
					t.Errorf("%s import: export missing %s", tc.name, marker)
				}
			}
		})
	}
}
