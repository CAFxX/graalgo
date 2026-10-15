package main

// Exporter tests for the synthetic `runtime` package's stack-inspection
// family: runtime.Callers/Caller/CallersFrames/FuncForPC and the
// (*Frames).Next / (*Func).Name/Entry/FileLine methods. Red-green: these
// fail before the overlay declares the family (`undefined:
// runtime.Callers`, the internal/bisect build failure) and pass after.

import (
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
