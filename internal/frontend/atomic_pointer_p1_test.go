package main

// P1-1 (REVIEW-2026-11-03-09): atomic.LoadPointer / StorePointer must
// lower to the "atomicptrop" wire node — the same ordered path as
// SwapPointer / CompareAndSwapPointer — never to a plain deref read or
// a plain deref write. A plain deref gives no happens-before edge, so a
// program synchronizing through LoadPointer/StorePointer could observe
// stale values with no error. These tests are the deterministic
// red-green gate for that fix: on the pre-fix lowering the load emits a
// "deref" node and the store an "assign"+"deref" pair, so every count
// assertion below fails there and passes here. (A concurrent timing
// test cannot deterministically fail on the old lowering — a stale
// read is permitted, not guaranteed — so the wire shape, not timing,
// is the gate; the end-to-end publication program lives in the interop
// test AtomicPointerPublishE2ETest.)

import (
	"encoding/json"
	"strings"
	"testing"
)

// countAtomicPtrOps counts "atomicptrop" nodes by op inside n.
func countAtomicPtrOps(t *testing.T, n any) map[string]int {
	t.Helper()
	ops := map[string]int{}
	walkJSONNodes(n, func(m map[string]any) {
		if m["kind"] != "atomicptrop" {
			return
		}
		fields, _ := m["fields"].(map[string]any)
		if op, _ := fields["op"].(string); op != "" {
			ops[op]++
		}
	})
	return ops
}

// countPlainAtomicCalls counts "call" nodes whose target ident names one
// of the package-level pointer atomics (i.e. calls the frontend failed
// to intercept, which would reach the bodyless assembly declarations).
func countPlainAtomicCalls(n any) int {
	bad := 0
	targets := map[string]bool{
		"LoadPointer": true, "StorePointer": true,
		"SwapPointer": true, "CompareAndSwapPointer": true,
	}
	walkJSONNodes(n, func(m map[string]any) {
		if m["kind"] != "call" {
			return
		}
		walkJSONNodes(m, func(inner map[string]any) {
			name, _ := inner["name"].(string)
			_, hasObj := inner["obj"]
			if targets[name] && hasObj {
				bad++
			}
		})
	})
	return bad
}

// mainFuncdecl returns the main package's main funcdecl subtree.
func mainFuncdecl(t *testing.T, out string) map[string]any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("wire JSON does not parse: %v", err)
	}
	var mainBody map[string]any
	walkJSONNodes(v, func(m map[string]any) {
		if name, _ := m["name"].(string); name == "main" && mainBody == nil {
			if _, hasBody := m["body"]; hasBody {
				mainBody = m
			}
		}
	})
	if mainBody == nil {
		t.Fatal("main funcdecl not found in export")
	}
	return mainBody
}

// TestAtomicPointerOpsLowerToAtomicPtrOp: all four package-level pointer
// atomics on a plain unsafe.Pointer variable lower to "atomicptrop"
// nodes — load and store included — with no plain deref node and no
// un-intercepted call left in main.
func TestAtomicPointerOpsLowerToAtomicPtrOp(t *testing.T) {
	src := `package main

import (
	"sync/atomic"
	"unsafe"
)

func main() {
	var p unsafe.Pointer
	var v unsafe.Pointer
	atomic.StorePointer(&p, v)
	q := atomic.LoadPointer(&p)
	old := atomic.SwapPointer(&p, v)
	ok := atomic.CompareAndSwapPointer(&p, old, v)
	_, _, _ = q, old, ok
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	mainBody := mainFuncdecl(t, out)
	ops := countAtomicPtrOps(t, mainBody)
	for op, want := range map[string]int{"load": 1, "store": 1, "swap": 1, "cas": 1} {
		if ops[op] != want {
			t.Errorf("atomicptrop %q nodes in main = %d, want %d (all ops: %v)", op, ops[op], want, ops)
		}
	}
	derefs := 0
	walkJSONNodes(mainBody, func(m map[string]any) {
		if m["kind"] == "deref" {
			derefs++
		}
	})
	if derefs != 0 {
		t.Errorf("found %d deref nodes in main: LoadPointer/StorePointer must not lower to plain deref", derefs)
	}
	if bad := countPlainAtomicCalls(mainBody); bad != 0 {
		t.Errorf("%d un-intercepted calls to package-level pointer atomics in main", bad)
	}
}

// TestAtomicLoadPointerRenamedImport: a renamed sync/atomic import
// lowers LoadPointer to "atomicptrop" load, like the canonical spelling
// (the StorePointer renamed case is pinned in atomic_rtatomic_r2_test.go).
func TestAtomicLoadPointerRenamedImport(t *testing.T) {
	canonical := `package main

import (
	"sync/atomic"
	"unsafe"
)

func main() {
	var p unsafe.Pointer
	_ = atomic.LoadPointer(&p)
}
`
	renamed := strings.Replace(canonical, `"sync/atomic"`, `atom "sync/atomic"`, 1)
	renamed = strings.Replace(renamed, "atomic.LoadPointer", "atom.LoadPointer", 1)
	for _, tc := range []struct{ name, src string }{
		{"canonical", canonical},
		{"renamed", renamed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := ExportFile(writeSyncProg(t, tc.src))
			if err != nil {
				t.Fatalf("ExportFile: %v", err)
			}
			ops := countAtomicPtrOps(t, mainFuncdecl(t, out))
			if ops["load"] != 1 {
				t.Errorf("atomicptrop load nodes in main = %d, want 1 (all ops: %v)", ops["load"], ops)
			}
		})
	}
}

// TestAtomicPointerMethodsLowerToAtomicPtrOp: the real
// atomic.Pointer[T] method bodies (indexed from disk on demand) must
// route their internal LoadPointer/StorePointer calls through
// "atomicptrop" too — on the pre-fix lowering the Store body emitted
// assign/deref and the Load body a deref, so no atomicptrop load/store
// node appeared anywhere in the export.
func TestAtomicPointerMethodsLowerToAtomicPtrOp(t *testing.T) {
	src := `package main

import (
	"sync/atomic"
)

func main() {
	var p atomic.Pointer[int]
	x := 7
	p.Store(&x)
	y := p.Load()
	_ = y
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	var v any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("wire JSON does not parse: %v", err)
	}
	ops := countAtomicPtrOps(t, v)
	if ops["load"] == 0 {
		t.Error("no atomicptrop load node in export: Pointer[T].Load still lowers to plain deref")
	}
	if ops["store"] == 0 {
		t.Error("no atomicptrop store node in export: Pointer[T].Store still lowers to plain deref write")
	}
	if bad := countPlainAtomicCalls(v); bad != 0 {
		t.Errorf("%d un-intercepted calls to package-level pointer atomics in export", bad)
	}
}
