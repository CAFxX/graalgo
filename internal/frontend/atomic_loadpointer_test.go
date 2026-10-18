package main

// L08 (review-20261018): atomic.LoadPointer must lower through the
// atomicaddr+syncop machinery like the other package-level word ops —
// an unsafe.Pointer is the 64-bit D5 handle, so the uintptr cell
// (int64) is exactly the pointer cell. The old lowering (a plain
// "deref" read) silently dropped the atomicity that is LoadPointer's
// entire contract.

import (
	"encoding/json"
	"strings"
	"testing"
)

// mainFuncBody returns the body node of the main package's main
// function in an export document. The export includes the imported
// sync/atomic package's own function bodies (whose word ops already
// lower through atomicaddr+syncop), so assertions must scope to the
// main package.
func mainFuncBody(t *testing.T, out string) map[string]any {
	t.Helper()
	var doc struct {
		Funcs []struct {
			Fields struct {
				Name    string         `json:"name"`
				PkgPath string         `json:"pkgpath"`
				Body    map[string]any `json:"body"`
			} `json:"fields"`
		} `json:"funcs"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("export is not valid JSON: %v", err)
	}
	for _, f := range doc.Funcs {
		if f.Fields.PkgPath == "main" && f.Fields.Name == "main" {
			return f.Fields.Body
		}
	}
	t.Fatalf("export has no main.main funcdecl")
	return nil
}

// walkKinds collects every node of the given kind under n.
func walkKinds(n any, kind string, acc *[]map[string]any) {
	switch v := n.(type) {
	case map[string]any:
		if k, _ := v["kind"].(string); k == kind {
			*acc = append(*acc, v)
		}
		for _, c := range v {
			walkKinds(c, kind, acc)
		}
	case []any:
		for _, c := range v {
			walkKinds(c, kind, acc)
		}
	}
}

func nodesOfKind(body map[string]any, kind string) []map[string]any {
	var acc []map[string]any
	walkKinds(body, kind, &acc)
	return acc
}

func strField(n map[string]any, key string) string {
	f, _ := n["fields"].(map[string]any)
	s, _ := f[key].(string)
	return s
}

// TestAtomicLoadPointerLowersToAtomicAddrSyncop pins the lowering:
// atomic.LoadPointer(&p) must emit an atomicaddr cell (cell "int64")
// feeding a syncop node with op "Load" and synctyp "atomic.Uintptr" —
// never a plain "deref" read (the L08 bug: atomicity silently lost).
func TestAtomicLoadPointerLowersToAtomicAddrSyncop(t *testing.T) {
	src := `package main

import (
	"sync/atomic"
	"unsafe"
)

var p unsafe.Pointer

func main() {
	q := atomic.LoadPointer(&p)
	_ = q
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with atomic.LoadPointer: %v", err)
	}
	body := mainFuncBody(t, out)
	if derefs := nodesOfKind(body, "deref"); len(derefs) != 0 {
		t.Errorf("main.main contains %d plain deref node(s): atomic.LoadPointer lost its atomicity", len(derefs))
	}
	syncops := nodesOfKind(body, "syncop")
	if len(syncops) != 1 {
		t.Fatalf("main.main contains %d syncop nodes, want 1 (the LoadPointer lowering)", len(syncops))
	}
	sop := syncops[0]
	if got := strField(sop, "op"); got != "Load" {
		t.Errorf("syncop op = %q, want \"Load\"", got)
	}
	if got := strField(sop, "synctyp"); got != "atomic.Uintptr" {
		t.Errorf("syncop synctyp = %q, want \"atomic.Uintptr\"", got)
	}
	recv, _ := sop["fields"].(map[string]any)["recv"].(map[string]any)
	if k, _ := recv["kind"].(string); k != "atomicaddr" {
		t.Errorf("syncop recv kind = %q, want \"atomicaddr\"", k)
	} else if got := strField(recv, "cell"); got != "int64" {
		t.Errorf("atomicaddr cell = %q, want \"int64\"", got)
	}
}

// TestAtomicLoadPointerRenamedImport: the interception discriminates
// on the resolved object (R2-P1-1), so a renamed sync/atomic import
// routes through the same atomic machinery.
func TestAtomicLoadPointerRenamedImport(t *testing.T) {
	src := `package main

import (
	atom "sync/atomic"
	"unsafe"
)

var p unsafe.Pointer

func main() {
	q := atom.LoadPointer(&p)
	_ = q
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with renamed atomic.LoadPointer: %v", err)
	}
	body := mainFuncBody(t, out)
	if derefs := nodesOfKind(body, "deref"); len(derefs) != 0 {
		t.Errorf("renamed import: main.main contains %d plain deref node(s)", len(derefs))
	}
	syncops := nodesOfKind(body, "syncop")
	if len(syncops) != 1 || strField(syncops[0], "op") != "Load" {
		t.Errorf("renamed import: atom.LoadPointer did not lower to syncop Load")
	}
}

// TestAtomicLoadPointerArgCount: wrong arity fails fast instead of
// silently miscompiling.
func TestAtomicLoadPointerArgCount(t *testing.T) {
	src := `package main

import "sync/atomic"

func main() {
	_ = atomic.LoadPointer()
}
`
	if _, err := ExportFile(writeSyncProg(t, src)); err == nil {
		t.Errorf("ExportFile accepted atomic.LoadPointer() with 0 args: want fail-fast error")
	} else if !strings.Contains(err.Error(), "LoadPointer") {
		t.Errorf("ExportFile error does not name LoadPointer: %v", err)
	}
}
