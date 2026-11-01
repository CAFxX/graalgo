package main

// P1-3: (*runtime.Func).Name/Entry are real via the PC interning
// (graalgo.runtime.StackTrace, the FuncForPC precedent) — not only as
// direct calls (runtimefuncname/runtimefuncentry wire nodes) but also
// as method values (rtstackmethodvalue wire node) and as go-statement
// function values. The old panicMsg marker (a runtime panic claiming
// "no PC-to-function table", plus a nil-decl crash in methodValueNode)
// is deleted.

import (
	"strings"
	"testing"
)

func TestRuntimeFuncMethodValueExport(t *testing.T) {
	cases := map[string]string{
		"Name": `package main

import "runtime"

func main() {
	var pcs [8]uintptr
	n := runtime.Callers(1, pcs[:])
	fn := runtime.FuncForPC(pcs[0])
	_ = n
	if fn != nil {
		f := fn.Name
		println(f())
	}
}
`,
		"Entry": `package main

import "runtime"

func main() {
	var pcs [8]uintptr
	n := runtime.Callers(1, pcs[:])
	fn := runtime.FuncForPC(pcs[0])
	_ = n
	if fn != nil {
		f := fn.Entry
		println(f())
	}
}
`,
	}
	for op, src := range cases {
		out, err := ExportFile(writeSyncProg(t, src))
		if err != nil {
			t.Errorf("fn.%s as method value: expected export success, got: %v", op, err)
			continue
		}
		if !strings.Contains(out, `"kind": "rtstackmethodvalue"`) {
			t.Errorf("fn.%s as method value: export missing rtstackmethodvalue node", op)
		}
		if !strings.Contains(out, `"op": "`+op+`"`) {
			t.Errorf("fn.%s as method value: export missing op %q", op, op)
		}
		if strings.Contains(out, "not implemented") {
			t.Errorf("fn.%s as method value: export still carries a not-implemented marker", op)
		}
	}
}

func TestRuntimeFuncMethodValueFileLineFailsFast(t *testing.T) {
	// FileLine is multi-result: no method-value lowering exists, and
	// the export must fail with a named scope error — never a crash
	// and never a runtime panic marker.
	src := `package main

import "runtime"

func main() {
	var pcs [8]uintptr
	n := runtime.Callers(1, pcs[:])
	fn := runtime.FuncForPC(pcs[0])
	_ = n
	if fn != nil {
		f := fn.FileLine
		_, _ = f(0)
	}
}
`
	_, err := ExportFile(writeSyncProg(t, src))
	if err == nil {
		t.Fatalf("fn.FileLine as method value: expected export failure, got success")
	}
	if !strings.Contains(err.Error(), "multi-result") {
		t.Fatalf("fn.FileLine as method value: error %q does not name the multi-result scope limit", err)
	}
}

func TestRuntimeFuncGoStmtExport(t *testing.T) {
	// go fn.Name(): the go statement runs the bound method value
	// inside a funclit (the direct-call wire node has no fun field
	// for the backend's go builder), not a panic call.
	src := `package main

import "runtime"

func main() {
	var pcs [8]uintptr
	n := runtime.Callers(1, pcs[:])
	fn := runtime.FuncForPC(pcs[0])
	_ = n
	if fn != nil {
		go fn.Name()
	}
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("go fn.Name(): expected export success, got: %v", err)
	}
	if !strings.Contains(out, `"kind": "rtstackmethodvalue"`) {
		t.Errorf("go fn.Name(): export missing rtstackmethodvalue node")
	}
	if !strings.Contains(out, `"kind": "gostmt"`) {
		t.Errorf("go fn.Name(): export missing gostmt node")
	}
	if strings.Contains(out, "not implemented") {
		t.Errorf("go fn.Name(): export still carries a not-implemented marker")
	}
}

func TestRuntimeFuncDeferStmtExport(t *testing.T) {
	// defer fn.Name(): same funclit desugar as the go statement.
	src := `package main

import "runtime"

func main() {
	var pcs [8]uintptr
	n := runtime.Callers(1, pcs[:])
	fn := runtime.FuncForPC(pcs[0])
	_ = n
	if fn != nil {
		defer fn.Name()
	}
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("defer fn.Name(): expected export success, got: %v", err)
	}
	if !strings.Contains(out, `"kind": "rtstackmethodvalue"`) {
		t.Errorf("defer fn.Name(): export missing rtstackmethodvalue node")
	}
	if !strings.Contains(out, `"kind": "defer"`) {
		t.Errorf("defer fn.Name(): export missing defer node")
	}
	if strings.Contains(out, "not implemented") {
		t.Errorf("defer fn.Name(): export still carries a not-implemented marker")
	}
}

func TestRuntimeFuncNameInGenericBodyExport(t *testing.T) {
	// A generic function calling fn.Name() directly must still export:
	// instantiation enumeration skips the synthetic stack methods
	// (previously the deleted panicMsg marker carried them through).
	src := `package main

import "runtime"

func nameOf[T any](fn *runtime.Func, _ T) string {
	return fn.Name()
}

func main() {
	var pcs [8]uintptr
	n := runtime.Callers(1, pcs[:])
	fn := runtime.FuncForPC(pcs[0])
	_ = n
	if fn != nil {
		println(nameOf(fn, 0))
	}
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("generic body calling fn.Name(): expected export success, got: %v", err)
	}
	if !strings.Contains(out, `"kind": "runtimefuncname"`) {
		t.Errorf("generic body calling fn.Name(): export missing runtimefuncname node")
	}
}
