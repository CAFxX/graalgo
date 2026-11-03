package main

// Exporter tests for (*runtime.Func).Name()/Entry() through an interface
// (review-20261103-funcname): an interface call must export as an
// ifacemethod node — it must compile, and the backend supplies the itab
// target (RtStackIfaceTargets), so dispatch returns the direct call's
// result instead of dying with "no method table installed". Red-green:
// if the exporter ever scope-errors or misroutes the call, the kind
// assertion fails; the runtime behavior is pinned by
// test/regression/funcname and RtStackIfaceTargetsTest.

import (
	"strings"
	"testing"
)

// TestRuntimeFuncNameIfaceCallExport: i.Name()/i.Entry() where i is an
// interface holding a *runtime.Func lower to ifacemethod nodes.
func TestRuntimeFuncNameIfaceCallExport(t *testing.T) {
	src := `package main

import "runtime"

func main() {
	var pcs [8]uintptr
	n := runtime.Callers(1, pcs[:])
	fn := runtime.FuncForPC(pcs[0])
	_, _ = n, fn
	var namer interface{ Name() string } = fn
	var enter interface{ Entry() uintptr } = fn
	_, _ = namer.Name(), enter.Entry()
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with interface Func calls: %v", err)
	}
	if !strings.Contains(out, `"kind": "ifacemethod"`) {
		t.Errorf("exported JSON missing \"kind\": \"ifacemethod\"")
	}
}
