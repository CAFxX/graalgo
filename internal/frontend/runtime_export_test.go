package main

// Exporter tests for the synthetic `runtime` package's stack-inspection
// family: runtime.Callers/Caller/CallersFrames/FuncForPC and the
// (*Frames).Next / (*Func).Name/Entry/FileLine methods. Red-green: these
// fail before the overlay declares the family (`undefined:
// runtime.Callers`, the internal/bisect build failure) and pass after.

import (
	"go/token"
	"go/types"
	"strings"
	"testing"
)

// TestRuntimeCallersExport: runtime.Callers lowers to the additive
// "runtimecallers" wire node.
func TestRuntimeCallersExport(t *testing.T) {
	src := `package main

import "runtime"

func main() {
	var pcs [8]uintptr
	n := runtime.Callers(1, pcs[:])
	_ = n
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with runtime.Callers: %v", err)
	}
	if !strings.Contains(out, `"kind": "runtimecallers"`) {
		t.Errorf("exported JSON missing \"kind\": \"runtimecallers\"")
	}
}

// TestRuntimeCallersFramesExport: runtime.CallersFrames lowers to
// "runtimecallersframes", and frames.Next() in 2-target position desugars
// to the "runtimeframesnext" wire node.
func TestRuntimeCallersFramesExport(t *testing.T) {
	src := `package main

import "runtime"

func main() {
	var pcs [8]uintptr
	n := runtime.Callers(1, pcs[:])
	frames := runtime.CallersFrames(pcs[:n])
	fr, more := frames.Next()
	_, _ = fr, more
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with runtime.CallersFrames/Next: %v", err)
	}
	for _, want := range []string{
		`"kind": "runtimecallersframes"`,
		`"kind": "runtimeframesnext"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("exported JSON missing %s", want)
		}
	}
}

// TestRuntimeCallerExport: runtime.Caller in 4-target position desugars
// to the "runtimecaller" wire node.
func TestRuntimeCallerExport(t *testing.T) {
	src := `package main

import "runtime"

func main() {
	pc, file, line, ok := runtime.Caller(0)
	_, _, _, _ = pc, file, line, ok
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with runtime.Caller: %v", err)
	}
	if !strings.Contains(out, `"kind": "runtimecaller"`) {
		t.Errorf("exported JSON missing \"kind\": \"runtimecaller\"")
	}
}

// TestRuntimeFuncExport: runtime.FuncForPC lowers to "runtimefuncforpc",
// and the (*Func) methods lower to their wire nodes (Name/Entry direct;
// FileLine via the 2-target desugar).
func TestRuntimeFuncExport(t *testing.T) {
	src := `package main

import "runtime"

func main() {
	var pcs [8]uintptr
	n := runtime.Callers(1, pcs[:])
	fn := runtime.FuncForPC(pcs[0])
	_ = n
	if fn == nil {
		return
	}
	name := fn.Name()
	entry := fn.Entry()
	file, line := fn.FileLine(pcs[0])
	_, _, _, _ = name, entry, file, line
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with runtime.FuncForPC/methods: %v", err)
	}
	for _, want := range []string{
		`"kind": "runtimefuncforpc"`,
		`"kind": "runtimefuncname"`,
		`"kind": "runtimefuncentry"`,
		`"kind": "runtimefuncfileline"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("exported JSON missing %s", want)
		}
	}
}

// TestRuntimeStackFailFast: the multi-result members (Caller, Next,
// FileLine) outside an N-target assignment fail fast and loud: a bare
// statement reaches the exporter's named multi-result scope error, and a
// single-target assignment is a go/types arity error (like gc) — never a
// confusing "undefined".
func TestRuntimeStackFailFast(t *testing.T) {
	// Bare statement: the exporter's named multi-result scope error.
	src := "package main\n\nimport \"runtime\"\n\nfunc main() {\n\tvar pcs [8]uintptr\n\tn := runtime.Callers(1, pcs[:])\n\tframes := runtime.CallersFrames(pcs[:n])\n\t_, _ = n, frames\n\tframes.Next()\n}\n"
	_, err := ExportFile(writeSyncProg(t, src))
	if err == nil {
		t.Errorf("bare frames.Next(): expected scope error, got success")
	} else if !strings.Contains(err.Error(), "multi-result") {
		t.Errorf("bare frames.Next(): error %q does not contain %q", err, "multi-result")
	}
	// Single-target assignments: go/types arity errors, like gc.
	for _, body := range []string{
		`x := runtime.Caller(0)`,
		`f := frames.Next()`,
		`x := fn.FileLine(0)`,
	} {
		src := "package main\n\nimport \"runtime\"\n\nfunc main() {\n\tvar pcs [8]uintptr\n\tn := runtime.Callers(1, pcs[:])\n\tframes := runtime.CallersFrames(pcs[:n])\n\tfn := runtime.FuncForPC(pcs[0])\n\t_, _, _, _ = pcs, n, frames, fn\n\t" + body + "\n}\n"
		_, err := ExportFile(writeSyncProg(t, src))
		if err == nil {
			t.Errorf("%q: expected type error, got success", body)
			continue
		}
		if !strings.Contains(err.Error(), "assignment mismatch") {
			t.Errorf("%q: error %q does not contain %q", body, err, "assignment mismatch")
		}
	}
}

// TestRuntimeStackFrameFields: the runtime.Frame struct exposes gc's
// exact field set (PC, Func, Function, File, Line, Entry).
func TestRuntimeStackFrameFields(t *testing.T) {
	src := `package main

import "runtime"

func useFrame(fr runtime.Frame) {
	_, _, _, _, _, _ = fr.PC, fr.Func, fr.Function, fr.File, fr.Line, fr.Entry
}

func main() {
	var pcs [8]uintptr
	n := runtime.Callers(1, pcs[:])
	frames := runtime.CallersFrames(pcs[:n])
	fr, _ := frames.Next()
	useFrame(fr)
}
`
	if _, err := ExportFile(writeSyncProg(t, src)); err != nil {
		t.Fatalf("ExportFile with runtime.Frame fields: %v", err)
	}
}

// TestRuntimeNumGoroutineExport: runtime.NumGoroutine lowers to the
// additive "runtimenumgoroutine" wire node. Red-green: this fails before
// the overlay declares NumGoroutine (`undefined: runtime.NumGoroutine`,
// the PKG-023 iter failure) and passes after.
func TestRuntimeNumGoroutineExport(t *testing.T) {
	src := `package main

import "runtime"

func main() {
	n := runtime.NumGoroutine()
	_ = n
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with runtime.NumGoroutine: %v", err)
	}
	if !strings.Contains(out, `"kind": "runtimenumgoroutine"`) {
		t.Errorf("exported JSON missing \"kind\": \"runtimenumgoroutine\"")
	}
}

// TestRuntimeErrorView: PKG-038 — gofrontend's runtime view must declare
// the Error interface (gc 1.27.1, src/runtime/error.go). The math/bits
// test driver does err.(runtime.Error) after recover(); the view gap made
// the whole package fail to type-check. Red-green: fails pre-fix with
// `undefined: runtime.Error`, passes post-fix.
func TestRuntimeErrorView(t *testing.T) {
	src := `package main

import "runtime"

func classify(err any) string {
	if e, ok := err.(runtime.Error); ok {
		return "runtime:" + e.Error()
	}
	return "other"
}

func main() {
	println(classify(nil))
}
`
	if _, err := ExportFile(writeSyncProg(t, src)); err != nil {
		t.Fatalf("ExportFile with err.(runtime.Error): %v", err)
	}
}

// TestRuntimeErrorShape: the view's Error interface is gc's shape — error
// plus the RuntimeError marker method — not net.Error's Timeout/Temporary
// shape. A type with only Error() must NOT satisfy it (negative control).
func TestRuntimeErrorShape(t *testing.T) {
	src := `package main

import "runtime"

type myRuntimeErr struct{ msg string }

func (e myRuntimeErr) Error() string { return e.msg }
func (e myRuntimeErr) RuntimeError()  {}

var (
	_ runtime.Error = myRuntimeErr{}
	_ runtime.Error = (*runtime.TypeAssertionError)(nil)
	_ runtime.Error = (*runtime.PanicNilError)(nil)
)

func main() {}
`
	if _, err := ExportFile(writeSyncProg(t, src)); err != nil {
		t.Fatalf("ExportFile with runtime.Error satisfaction: %v", err)
	}
	neg := `package main

import "runtime"

type justError struct{}

func (e justError) Error() string { return "x" }

var _ runtime.Error = justError{}

func main() {}
`
	if _, err := ExportFile(writeSyncProg(t, neg)); err == nil {
		t.Errorf("justError (Error() only): expected type error, got success")
	}
}

// TestRuntimeViewDecls: the remaining documented go1.27.1 runtime API
// members resolve in the view — the Compiler const, the MemProfileRate var
// (read and write), and the profile-record structs with gc-exact fields.
func TestRuntimeViewDecls(t *testing.T) {
	src := `package main

import "runtime"

func main() {
	var compiler string = runtime.Compiler
	_ = compiler
	runtime.MemProfileRate = 0
	n := runtime.MemProfileRate
	_ = n
	var sr runtime.StackRecord
	_ = sr.Stack0
	var bpr runtime.BlockProfileRecord
	_, _, _ = bpr.Count, bpr.Cycles, bpr.StackRecord
	var mpr runtime.MemProfileRecord
	_, _, _, _, _ = mpr.AllocBytes, mpr.FreeBytes, mpr.AllocObjects, mpr.FreeObjects, mpr.Stack0
	var cl runtime.Cleanup
	_ = cl
	var p runtime.Pinner
	_ = p
	var tae runtime.TypeAssertionError
	_ = tae
	var pne runtime.PanicNilError
	_ = pne
}
`
	if _, err := ExportFile(writeSyncProg(t, src)); err != nil {
		t.Fatalf("ExportFile with runtime view decls: %v", err)
	}
}

// TestRuntimeViewFuncsHonest: newly declared runtime functions that have no
// backend intrinsic yet fail LOUD at export ("call to unknown function"),
// never with the view-gap "undefined". Red-green per function: pre-fix the
// error names "undefined", post-fix it names the honest export error.
//
// NOTE (PKG-040, 2026-10-16): runtime.Gosched used to be in this list — it
// was declared surface with no backend. PKG-040 implemented it for real
// (frontend lowers to the "runtimegosched" statement wire node, backend
// GoschedNode yields via SchedulerCore.yieldCurrent), so it now exports
// successfully and moved out of the honest-error list into the assertion
// below. This is the expected 038→040 transition, not a weakening: the
// list still covers every remaining unimplemented declared function.
func TestRuntimeViewFuncsHonest(t *testing.T) {
	for _, call := range []string{
		`runtime.Breakpoint()`,
		`_ = runtime.NumCgoCall()`,
	} {
		src := "package main\n\nimport \"runtime\"\n\nfunc main() {\n\t" + call + "\n}\n"
		_, err := ExportFile(writeSyncProg(t, src))
		if err == nil {
			t.Errorf("%s: expected honest unimplemented error, got success", call)
			continue
		}
		if strings.Contains(err.Error(), "undefined") {
			t.Errorf("%s: view gap, error %q contains \"undefined\"", call, err)
		}
		if !strings.Contains(err.Error(), "unknown function") {
			t.Errorf("%s: error %q does not contain %q", call, err, "unknown function")
		}
	}

	// PKG-040: runtime.Gosched() is now a known, implemented function —
	// the call exports (to the real "runtimegosched" wire node), never
	// with "undefined" and never with the honest "unknown function" error.
	src := "package main\n\nimport \"runtime\"\n\nfunc main() {\n\truntime.Gosched()\n}\n"
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("runtime.Gosched: implemented since PKG-040, export failed: %v", err)
	}
	if strings.Contains(out, "undefined") || strings.Contains(out, "unknown function") {
		t.Errorf("runtime.Gosched: export output must not contain view-gap or honest-error markers")
	}
	if !strings.Contains(out, `"kind": "runtimegosched"`) {
		t.Errorf("runtime.Gosched: export did not lower to the real runtimegosched wire node")
	}
}

// TestRuntimeViewCompleteness: mechanical audit pin — every member of gc
// 1.27.1's documented runtime API (enumerated via `go doc -all runtime`)
// is declared in the synthetic view, with gc-exact shapes for the Error
// interface, the record structs, and the method sets. The view is fetched
// through the real overlay importer, exactly as importing code sees it.
func TestRuntimeViewCompleteness(t *testing.T) {
	pkg, err := overlayImporter{}.Import("runtime")
	if err != nil {
		t.Fatalf("overlayImporter.Import(runtime): %v", err)
	}
	scope := pkg.Scope()
	for _, name := range []string{
		// consts
		"Compiler", "GOARCH", "GOOS",
		// var
		"MemProfileRate",
		// funcs
		"AddCleanup", "BlockProfile", "Breakpoint", "CPUProfile",
		"Caller", "Callers", "CallersFrames", "FuncForPC",
		"GC", "GOMAXPROCS", "GOROOT", "Goexit", "GoroutineProfile",
		"Gosched", "KeepAlive", "LockOSThread", "MemProfile",
		"MutexProfile", "NumCPU", "NumCgoCall", "NumGoroutine",
		"ReadMemStats", "ReadTrace", "SetBlockProfileRate",
		"SetCPUProfileRate", "SetCgoTraceback", "SetDefaultGOMAXPROCS",
		"SetFinalizer", "SetMutexProfileFraction", "Stack",
		"StartTrace", "StopTrace", "ThreadCreateProfile",
		"UnlockOSThread", "Version",
		// types
		"BlockProfileRecord", "Cleanup", "Error", "Frame", "Frames",
		"Func", "MemProfileRecord", "MemStats", "PanicNilError",
		"Pinner", "StackRecord", "TypeAssertionError",
	} {
		if scope.Lookup(name) == nil {
			t.Errorf("runtime view: documented API member %q not declared", name)
		}
	}

	// Error is gc's shape: embeds error, declares RuntimeError.
	errObj, ok := scope.Lookup("Error").(*types.TypeName)
	if !ok {
		t.Fatalf("runtime.Error is not a *types.TypeName")
	}
	errNamed, ok := errObj.Type().(*types.Named)
	if !ok {
		t.Fatalf("runtime.Error is not a named type")
	}
	iface, ok := errNamed.Underlying().(*types.Interface)
	if !ok {
		t.Fatalf("runtime.Error underlying is not an interface")
	}
	embedsError := false
	for i := 0; i < iface.NumEmbeddeds(); i++ {
		if e, ok := iface.EmbeddedType(i).(*types.Named); ok &&
			e.Obj().Name() == "error" && e.Obj().Parent() == types.Universe {
			embedsError = true
		}
	}
	if !embedsError {
		t.Errorf("runtime.Error does not embed error")
	}
	errMethods := types.NewMethodSet(errNamed)
	for _, m := range []string{"Error", "RuntimeError"} {
		if errMethods.Lookup(nil, m) == nil {
			t.Errorf("runtime.Error method set missing %q", m)
		}
	}

	// gc-exact method sets (pointer vs value receivers matter).
	checkMethods := func(typeName string, ptrRecv bool, methods ...string) {
		t.Helper()
		obj, ok := scope.Lookup(typeName).(*types.TypeName)
		if !ok {
			t.Errorf("runtime.%s is not a *types.TypeName", typeName)
			return
		}
		named, ok := obj.Type().(*types.Named)
		if !ok {
			t.Errorf("runtime.%s is not a named type", typeName)
			return
		}
		var ms *types.MethodSet
		if ptrRecv {
			ms = types.NewMethodSet(types.NewPointer(named))
		} else {
			ms = types.NewMethodSet(named)
		}
		for _, m := range methods {
			if ms.Lookup(nil, m) == nil {
				t.Errorf("runtime.%s method set missing %q", typeName, m)
			}
		}
	}
	checkMethods("TypeAssertionError", true, "Error", "RuntimeError")
	checkMethods("PanicNilError", true, "Error", "RuntimeError")
	checkMethods("Pinner", true, "Pin", "Unpin")
	checkMethods("Cleanup", false, "Stop") // gc: func (c Cleanup) Stop()
	checkMethods("Cleanup", true, "Stop")
	checkMethods("StackRecord", true, "Stack")
	checkMethods("MemProfileRecord", true, "InUseBytes", "InUseObjects", "Stack")

	// gc-exact struct shapes.
	fieldTypes := func(typeName string) []types.Type {
		t.Helper()
		obj, ok := scope.Lookup(typeName).(*types.TypeName)
		if !ok {
			t.Errorf("runtime.%s is not a *types.TypeName", typeName)
			return nil
		}
		named, ok := obj.Type().(*types.Named)
		if !ok {
			t.Errorf("runtime.%s is not a named type", typeName)
			return nil
		}
		st, ok := named.Underlying().(*types.Struct)
		if !ok {
			t.Errorf("runtime.%s underlying is not a struct", typeName)
			return nil
		}
		var ts []types.Type
		for i := 0; i < st.NumFields(); i++ {
			ts = append(ts, st.Field(i).Type())
		}
		return ts
	}
	srFields := fieldTypes("StackRecord")
	if len(srFields) != 1 {
		t.Errorf("runtime.StackRecord: %d fields, want 1 (Stack0)", len(srFields))
	} else if arr, ok := srFields[0].(*types.Array); !ok || arr.Len() != 32 {
		t.Errorf("runtime.StackRecord.Stack0: not [32]uintptr")
	}
	// AddCleanup is generic over [T, S any] with gc's parameter order.
	ac, ok := scope.Lookup("AddCleanup").(*types.Func)
	if !ok {
		t.Fatalf("runtime.AddCleanup is not a *types.Func")
	}
	sig, ok := ac.Type().(*types.Signature)
	if !ok {
		t.Fatalf("runtime.AddCleanup type is not a signature")
	}
	if sig.TypeParams().Len() != 2 {
		t.Errorf("runtime.AddCleanup: %d type params, want 2 [T, S]",
			sig.TypeParams().Len())
	}
	if sig.Params().Len() != 3 || sig.Results().Len() != 1 {
		t.Errorf("runtime.AddCleanup: %d params/%d results, want 3/1",
			sig.Params().Len(), sig.Results().Len())
	}
}

// TestRuntimeGoschedExport: runtime.Gosched() lowers to the additive
// "runtimegosched" statement wire node. Red-green: this fails before the
// overlay declares Gosched (`undefined: runtime.Gosched`, the PKG-040 iter
// failure) and passes after.
func TestRuntimeGoschedExport(t *testing.T) {
	src := `package main

import "runtime"

func main() {
	runtime.Gosched()
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with runtime.Gosched: %v", err)
	}
	if !strings.Contains(out, `"kind": "runtimegosched"`) {
		t.Errorf("exported JSON missing \"kind\": \"runtimegosched\"")
	}
}

// TestRuntimeGoschedDeferGoExport: `defer runtime.Gosched()` and
// `go runtime.Gosched()` desugar through a funclit (the "runtimegosched"
// wire node is a statement, not a call, so it cannot ride the defer/
// gostmt call slot directly). The funclit body must carry the wire node.
func TestRuntimeGoschedDeferGoExport(t *testing.T) {
	src := `package main

import "runtime"

func main() {
	defer runtime.Gosched()
	go runtime.Gosched()
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with defer/go runtime.Gosched: %v", err)
	}
	if n := strings.Count(out, `"kind": "runtimegosched"`); n != 2 {
		t.Errorf("exported JSON has %d runtimegosched nodes, want 2 (defer + go)", n)
	}
}

// TestRuntimeFuncNameMethodValueFailsAtExport (L11, R1-13): a direct
// fn.Name() call lowers to the runtimefuncname intrinsic (see
// TestRuntimeFuncExport), but the bound method value `fn.Name` has no
// lowering: it must fail at export time with a named scope error —
// never a runtime panic marker, and never an exporter crash (before
// this fix, methodValueNode dereferenced the marker's nil funcdecl).
func TestRuntimeFuncNameMethodValueFailsAtExport(t *testing.T) {
	src := `package main

import "runtime"

func main() {
	var pcs [8]uintptr
	n := runtime.Callers(1, pcs[:])
	_ = n
	fn := runtime.FuncForPC(pcs[0])
	if fn == nil {
		return
	}
	nf := fn.Name
	_ = nf
}
`
	_, err := ExportFile(writeSyncProg(t, src))
	if err == nil {
		t.Fatalf("ExportFile with fn.Name method value: expected export-time scope error, got nil")
	}
	if !strings.Contains(err.Error(), "method value") || !strings.Contains(err.Error(), "direct intrinsic call") {
		t.Errorf("ExportFile error = %v, want a method-value/direct-intrinsic scope error", err)
	}
}

// TestIsStdlibRuntimeFuncByPath (L11, R1-14): the runtime interception
// predicate keys on import path, never package name. A package merely
// named runtime at another path must not qualify; only path "runtime"
// does. (An export-level test with a fake runtime package is not
// possible in this harness: ExportFile type-checks via the overlay /
// fail importer, so non-stdlib imports cannot resolve here.)
func TestIsStdlibRuntimeFuncByPath(t *testing.T) {
	sig := types.NewSignatureType(nil, nil, nil, types.NewTuple(), types.NewTuple(), false)
	stdlib := types.NewFunc(token.NoPos, types.NewPackage("runtime", "runtime"), "SetFinalizer", sig)
	other := types.NewFunc(token.NoPos, types.NewPackage("example.com/foo/runtime", "runtime"), "SetFinalizer", sig)
	if !isStdlibRuntimeFunc(stdlib) {
		t.Errorf("isStdlibRuntimeFunc(stdlib runtime.SetFinalizer) = false, want true")
	}
	if isStdlibRuntimeFunc(other) {
		t.Errorf("isStdlibRuntimeFunc(example.com/foo/runtime.SetFinalizer) = true, want false (name match must not qualify)")
	}
	if isStdlibRuntimeFunc(nil) {
		t.Errorf("isStdlibRuntimeFunc(nil) = true, want false")
	}
}
