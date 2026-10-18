package main

// Regression tests for RV18-P1-09a: runtime intrinsic interceptions are
// alias-invariant — they key on the resolved package (import path
// "runtime"), never on the local import spelling. The gcinterop lowering
// (SetFinalizer/KeepAlive/GC) and the Gosched interception used to require
// the local name to be literally "runtime", so `import rt "runtime"`
// silently skipped them. (The Goexit renamed-import case is already pinned
// by TestGoexitRenamedImport in atomic_rtatomic_r2_test.go.)

import (
	"strings"
	"testing"
)

// TestGcinteropRenamedImport: rt "runtime"; rt.SetFinalizer/KeepAlive/GC
// must lower to the gcinterop wire nodes exactly like the canonical
// spelling.
func TestGcinteropRenamedImport(t *testing.T) {
	canonical := `package main

import "runtime"

func main() {
	var x *int
	runtime.SetFinalizer(x, func(p *int) {})
	runtime.KeepAlive(x)
	runtime.GC()
}
`
	renamed := `package main

import rt "runtime"

func main() {
	var x *int
	rt.SetFinalizer(x, func(p *int) {})
	rt.KeepAlive(x)
	rt.GC()
}
`
	for _, tc := range []struct{ name, src string }{
		{"canonical", canonical},
		{"renamed", renamed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := ExportFile(writeSyncProg(t, tc.src))
			if err != nil {
				t.Fatalf("ExportFile: %v", err)
			}
			for _, want := range []string{
				`"gcinterop": "SetFinalizer"`,
				`"gcinterop": "KeepAlive"`,
				`"gcinterop": "GC"`,
			} {
				if !strings.Contains(out, want) {
					t.Errorf("exported JSON missing %s (intrinsic not lowered)", want)
				}
			}
		})
	}
}

// TestGoschedRenamedImport: rt "runtime"; `go rt.Gosched()` must lower to
// the runtimegosched wire node exactly like the canonical spelling. This
// exercises isGoschedCall (the defer/go statement-position interception),
// which had the same local-name gate as the gcinterop block.
func TestGoschedRenamedImport(t *testing.T) {
	canonical := `package main

import "runtime"

func main() {
	go runtime.Gosched()
}
`
	renamed := `package main

import rt "runtime"

func main() {
	go rt.Gosched()
}
`
	for _, tc := range []struct{ name, src string }{
		{"canonical", canonical},
		{"renamed", renamed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := ExportFile(writeSyncProg(t, tc.src))
			if err != nil {
				t.Fatalf("ExportFile: %v", err)
			}
			if !strings.Contains(out, `"kind": "runtimegosched"`) {
				t.Error("Gosched call was not intercepted to the runtimegosched wire node")
			}
		})
	}
}
