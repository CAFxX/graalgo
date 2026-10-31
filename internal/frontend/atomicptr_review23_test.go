package main

// Review23 P1-1 regression tests: atomic.LoadPointer / atomic.StorePointer
// must lower to the "atomicptrop" load/store ops — never to a plain
// deref read or a plain *addr = val assign. The pre-fix lowering had no
// ordering guarantee at all: a publication spin loop could never observe
// the store (reproduced on the backend's own GoBox storage, hanging at
// rounds 34 and 39 of the plain-access harness, while the shared-lock
// form ran 20000 rounds clean).
//
// These tests are the deterministic gate: the concurrency shape itself is
// timing-dependent (a 50-round guest publication program passed pre-fix
// on x86), but the lowering is not — pre-fix, the load test finds a
// deref node and no atomicptrop load, and the store test finds an
// assign/deref instead of an atomicptrop store, so both fail.

import (
	"encoding/json"
	"strings"
	"testing"
)

// mainBodyOf returns the main package's main funcdecl node from exported
// wire JSON, or fails the test. Scoping to main keeps sync/atomic's own
// internals (which legitimately contain the same ops) out of the counts.
func mainBodyOf(t *testing.T, out string) map[string]any {
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

// countPtrOps counts atomicptrop nodes by op, plus plain deref nodes,
// inside one subtree.
func countPtrOps(t *testing.T, body map[string]any) (loads, stores, swaps, cass, derefs int) {
	t.Helper()
	walkJSONNodes(body, func(m map[string]any) {
		switch m["kind"] {
		case "atomicptrop":
			fields, _ := m["fields"].(map[string]any)
			switch fields["op"] {
			case "load":
				loads++
			case "store":
				stores++
			case "swap":
				swaps++
			case "cas":
				cass++
			}
		case "deref":
			derefs++
		}
	})
	return
}

const atomicPtrProg = `package main

import (
	"sync/atomic"
	"unsafe"
)

func main() {
	var p unsafe.Pointer
	var v unsafe.Pointer
	atomic.StorePointer(&p, v)
	q := atomic.LoadPointer(&p)
	_ = q
	_ = v
}
`

// TestAtomicLoadStorePointerLowerToAtomicptrop: both package-level ops
// lower to atomicptrop load/store, with no plain deref left in main.
func TestAtomicLoadStorePointerLowerToAtomicptrop(t *testing.T) {
	out, err := ExportFile(writeSyncProg(t, atomicPtrProg))
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	loads, stores, _, _, derefs := countPtrOps(t, mainBodyOf(t, out))
	if loads != 1 {
		t.Errorf("atomicptrop load nodes in main = %d, want 1", loads)
	}
	if stores != 1 {
		t.Errorf("atomicptrop store nodes in main = %d, want 1", stores)
	}
	if derefs != 0 {
		t.Errorf("plain deref nodes in main = %d, want 0 (LoadPointer/StorePointer must not lower to plain access)", derefs)
	}
}

// TestAtomicLoadPointerRenamedImport: the renamed-import spelling lowers
// exactly like the canonical spelling (cf. R2-P1-1 for StorePointer).
func TestAtomicLoadPointerRenamedImport(t *testing.T) {
	renamed := strings.Replace(atomicPtrProg, `"sync/atomic"`, `atom "sync/atomic"`, 1)
	renamed = strings.Replace(renamed, "atomic.StorePointer", "atom.StorePointer", 1)
	renamed = strings.Replace(renamed, "atomic.LoadPointer", "atom.LoadPointer", 1)
	out, err := ExportFile(writeSyncProg(t, renamed))
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	loads, stores, _, _, derefs := countPtrOps(t, mainBodyOf(t, out))
	if loads != 1 {
		t.Errorf("atomicptrop load nodes in main (renamed import) = %d, want 1", loads)
	}
	if stores != 1 {
		t.Errorf("atomicptrop store nodes in main (renamed import) = %d, want 1", stores)
	}
	if derefs != 0 {
		t.Errorf("plain deref nodes in main (renamed import) = %d, want 0", derefs)
	}
}
