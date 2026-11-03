// Regression test for REVIEW-2026-11-03-00 N-P1-1 (lane rev20261103-funcname):
// (*runtime.Func).Name()/Entry() must work, including in method-value
// position. Pre-fix, `f := fn.Name` crashed the exporter (nil dereference
// on the retired panicMsg marker); direct calls were already real.
//
// Frame resolution uses the CallersFrames idiom (NOT FuncForPC on a raw
// Callers PC: a raw PC is a return address, and FuncForPC on it lags one
// frame behind — gc returns "runtime.Callers" for the innermost frame.
// CallersFrames applies the -1 adjustment internally).
//
// Assertions are backend-robust by design: they compare graalgo behavior
// against gc's contract in forms that hold on both backends.
// - Name() of a frame must be non-empty and end with the declared
//   function name (gc qualifies with the package path, e.g.
//   "funcname.TestFuncNameDirectAndMethodValue"; graalgo reports the
//   bare wire name — the documented StackTrace.qualifyFuncName
//   divergence, backend scope, not this lane).
// - A method value must return exactly what the direct call returns.
// - Entry() direct and via method value must agree.
// Run under gc with `go test`, under graalgo with `graalgo test ./dir`.
package funcname

import (
	"runtime"
	"strings"
	"testing"
)

// firstFrameFunc returns the *runtime.Func of the first frame above the
// caller of this helper (skip 2: Callers + this helper), via CallersFrames.
func firstFrameFunc(t *testing.T) *runtime.Func {
	t.Helper()
	var pcs [8]uintptr
	n := runtime.Callers(2, pcs[:])
	if n == 0 {
		t.Fatal("Callers returned no PCs")
	}
	frames := runtime.CallersFrames(pcs[:n])
	frame, _ := frames.Next()
	if frame.Func == nil {
		t.Fatal("first frame has nil Func")
	}
	return frame.Func
}

func TestFuncNameDirectAndMethodValue(t *testing.T) {
	fn := firstFrameFunc(t)
	direct := fn.Name()
	if direct == "" {
		t.Fatal("fn.Name() returned empty string")
	}
	if !strings.HasSuffix(direct, "TestFuncNameDirectAndMethodValue") {
		t.Fatalf("fn.Name() = %q, want suffix %q", direct, "TestFuncNameDirectAndMethodValue")
	}
	nameOf := fn.Name
	if got := nameOf(); got != direct {
		t.Fatalf("method value fn.Name() = %q, want direct call %q", got, direct)
	}
	entry := fn.Entry()
	entryOf := fn.Entry
	if got := entryOf(); got != entry {
		t.Fatalf("method value fn.Entry() = %d, want direct call %d", got, entry)
	}
}

//go:noinline
func helperName() string {
	var pcs [8]uintptr
	n := runtime.Callers(1, pcs[:])
	if n == 0 {
		return ""
	}
	frames := runtime.CallersFrames(pcs[:n])
	frame, _ := frames.Next()
	if frame.Func == nil {
		return ""
	}
	return frame.Func.Name()
}

func TestFuncNameHelperFrame(t *testing.T) {
	name := helperName()
	if name == "" {
		t.Fatal("helper frame Name() returned empty string")
	}
	if !strings.HasSuffix(name, "helperName") {
		t.Fatalf("helper frame Name() = %q, want suffix %q", name, "helperName")
	}
}

// TestFuncNameViaInterface is the P1-2 regression test (REVIEW-2026-11-03-10):
// (*runtime.Func).Name()/Entry() must work through an interface value,
// the idiomatic shape for passing stack-inspection results around
// (e.g. a Namer interface). Pre-fix, the interface call lowered to an
// ifacemethod node whose backend itab had no entry for the synthetic
// runtime.Func type, so dispatch failed downstream instead of reaching
// the rtstack nodes that serve direct calls and method values.
func TestFuncNameViaInterface(t *testing.T) {
	fn := firstFrameFunc(t)
	directName := fn.Name()
	directEntry := fn.Entry()
	// Direct + concrete method value (already working) as controls.
	if got := fn.Name; got() != directName {
		t.Fatalf("method value fn.Name() = %q, want %q", got(), directName)
	}
	// Interface dispatch: Name.
	var namer interface{ Name() string } = fn
	if got := namer.Name(); got != directName {
		t.Fatalf("interface namer.Name() = %q, want direct %q", got, directName)
	}
	// Interface dispatch: Entry.
	var enter interface{ Entry() uintptr } = fn
	if got := enter.Entry(); got != directEntry {
		t.Fatalf("interface enter.Entry() = %d, want direct %d", got, directEntry)
	}
	// One interface carrying both methods.
	var both interface {
		Name() string
		Entry() uintptr
	} = fn
	if got := both.Name(); got != directName {
		t.Fatalf("interface both.Name() = %q, want direct %q", got, directName)
	}
	if got := both.Entry(); got != directEntry {
		t.Fatalf("interface both.Entry() = %d, want direct %d", got, directEntry)
	}
}

// NOTE (not a test): a method value taken from an interface-typed
// receiver (`nameOf := namer.Name`) is deliberately NOT covered here.
// It fails at export with "method value via interface (P3-M3 scope)"
// for every type, not just *runtime.Func (verified with a plain user
// type holding a Name method) — dynamic dispatch deferred to call time
// is a general P3-M3 limitation, separate from this lane's itab gap,
// and needs its own lane. Adding such a case to this package would fail
// the whole package's export and mask the interface-call coverage above.

// TestFuncFileLineViaInterface probes the FileLine pair machinery through
// an interface: FileLine is multi-result, so its interface shape may share
// the same table gap or a different (pair-plan) gap. Kept as a probe
// alongside the Name/Entry gate: it must agree with the direct call.
func TestFuncFileLineViaInterface(t *testing.T) {
	fn := firstFrameFunc(t)
	directFile, directLine := fn.FileLine(fn.Entry())
	var filer interface {
		FileLine(uintptr) (string, int)
	} = fn
	gotFile, gotLine := filer.FileLine(fn.Entry())
	if gotFile != directFile || gotLine != directLine {
		t.Fatalf("interface FileLine() = (%q, %d), want direct (%q, %d)", gotFile, gotLine, directFile, directLine)
	}
}
