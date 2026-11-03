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
//   - Name() of a frame must be non-empty and end with the declared
//     function name (gc qualifies with the package path, e.g.
//     "funcname.TestFuncNameDirectAndMethodValue"; graalgo reports the
//     bare wire name — the documented StackTrace.qualifyFuncName
//     divergence, backend scope, not this lane).
//   - A method value must return exactly what the direct call returns.
//   - Entry() direct and via method value must agree.
//
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

// TestFuncNameInterfaceCall (review-20261103-funcname): calling Name()
// and Entry() through an interface holding the *runtime.Func must return
// exactly what the direct calls return. Pre-fix the interface call died
// at dispatch with "no method table installed for dynamic type
// \"*runtime.Func\"" (host fatal error) even though the direct call
// worked — the synthetic Func type carried no itab entry.
func TestFuncNameInterfaceCall(t *testing.T) {
	fn := firstFrameFunc(t)
	direct := fn.Name()
	var namer interface{ Name() string } = fn
	if got := namer.Name(); got != direct {
		t.Fatalf("interface fn.Name() = %q, want direct call %q", got, direct)
	}
	entry := fn.Entry()
	var enter interface{ Entry() uintptr } = fn
	if got := enter.Entry(); got != entry {
		t.Fatalf("interface fn.Entry() = %d, want direct call %d", got, entry)
	}
}

// TestFuncNameNilReceiver: nil-receiver semantics must match gc exactly
// (runtime/symtab.go): Name() returns "" for nil, Entry() panics with a
// nil dereference. Both the direct and the interface forms, and the panic
// must be recoverable.
func TestFuncNameNilReceiver(t *testing.T) {
	var fn *runtime.Func
	if got := fn.Name(); got != "" {
		t.Fatalf("direct nil Name() = %q, want empty string (gc)", got)
	}
	var namer interface{ Name() string } = fn
	if got := namer.Name(); got != "" {
		t.Fatalf("interface nil Name() = %q, want empty string (gc)", got)
	}
	mustPanic := func(name string, f func()) {
		t.Helper()
		defer func() {
			if r := recover(); r == nil {
				t.Errorf("%s on nil *Func: expected panic, got none", name)
			}
		}()
		f()
	}
	mustPanic("direct Entry()", func() { _ = fn.Entry() })
	var enter interface{ Entry() uintptr } = fn
	mustPanic("interface Entry()", func() { _ = enter.Entry() })
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
