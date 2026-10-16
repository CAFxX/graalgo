package main

// Regression tests for FE-5 + FE-6 (independent codebase review,
// 2026-10-16): exporter crashes on nested multi-result calls and on
// runtime/sync multi-result calls in return position.
//
// FE-5: the nested-call branch of multiResultHelperArgs handled only
// mrUser/mrIfaceMethod; every other kind fell through to multiResultHelper
// -> getSyntheticPackage("") -> panic("gofrontend: no synthetic source
// for "). E.g. a func-field call nested in a synthetic helper call's
// argument crashed the exporter on valid Go.
//
// FE-6: hoistMRCallArg had no mrRuntimeStack/mrSyncMap branch, so
// `return runtime.Caller(0)` / `return m.Load(k)` fell through to the
// synthetic-helper path and panicked the same way.
//
// Red-green: each test panics pre-fix (no synthetic source for "") and
// exports the expected intrinsic wire node post-fix.

import (
	"strings"
	"testing"
)

// TestFE6RuntimeCallerReturn: runtime.Caller in return position desugars
// via hoistMRCallArg to the "runtimecaller" wire node.
func TestFE6RuntimeCallerReturn(t *testing.T) {
	src := `package main

import "runtime"

func caller() (uintptr, string, int, bool) {
	return runtime.Caller(0)
}

func main() {
	pc, file, line, ok := caller()
	_, _, _, _ = pc, file, line, ok
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with return runtime.Caller(0): %v", err)
	}
	if !strings.Contains(out, `"kind": "runtimecaller"`) {
		t.Errorf("exported JSON missing \"kind\": \"runtimecaller\"")
	}
}

// TestFE6RuntimeFramesNextReturn: (*Frames).Next in return position
// desugars via hoistMRCallArg to the "runtimeframesnext" wire node.
func TestFE6RuntimeFramesNextReturn(t *testing.T) {
	src := `package main

import "runtime"

func next(frames *runtime.Frames) (runtime.Frame, bool) {
	return frames.Next()
}

func main() {
	var pcs [8]uintptr
	n := runtime.Callers(1, pcs[:])
	frames := runtime.CallersFrames(pcs[:n])
	fr, more := next(frames)
	_, _ = fr, more
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with return frames.Next(): %v", err)
	}
	if !strings.Contains(out, `"kind": "runtimeframesnext"`) {
		t.Errorf("exported JSON missing \"kind\": \"runtimeframesnext\"")
	}
}

// TestFE6SyncMapLoadReturn: sync.Map.Load in return position must export
// without crashing. (sync resolves from disk on current master, so the
// call is ordinary; the test pins the return-position path for the
// mrSyncMap branch of hoistMRCallArg.)
func TestFE6SyncMapLoadReturn(t *testing.T) {
	src := `package main

import "sync"

func load(m *sync.Map, k any) (any, bool) {
	return m.Load(k)
}

func main() {
	var m sync.Map
	v, ok := load(&m, "k")
	_, _ = v, ok
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with return m.Load(k): %v", err)
	}
	if !strings.Contains(out, `"kind": "return"`) {
		t.Errorf("exported JSON missing \"kind\": \"return\"")
	}
}

// TestFE5NestedFuncField: a func-field multi-result call nested as the
// argument of a synthetic helper call (bits.Add(c.gen(r))) must export;
// the inner call lowers to the "funcfieldcall" wire node.
func TestFE5NestedFuncField(t *testing.T) {
	src := `package main

import "math/bits"

type curve struct {
	gen func(uint) (uint, uint, uint)
}

func main() {
	c := curve{gen: func(r uint) (uint, uint, uint) { return r, r, r }}
	sum, carryOut := bits.Add(c.gen(1))
	_, _ = sum, carryOut
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with bits.Add(c.gen(1)): %v", err)
	}
	if !strings.Contains(out, `"kind": "funcfieldcall"`) {
		t.Errorf("exported JSON missing \"kind\": \"funcfieldcall\"")
	}
}
