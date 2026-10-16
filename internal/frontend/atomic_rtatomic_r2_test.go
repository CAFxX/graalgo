package main

// R2 remediation tests (lane review-r2-atomic-rtatomic-p0).
//
// - P0: atomicWordOps must carry the internal/runtime/atomic word ops
//   (LoadAcquintptr/StoreReluintptr) that the f9735cc2 rebase dropped;
//   corpus 46_sync_pool must lower the sync/pool.go rtatomic call sites
//   to atomicaddr+syncop wire nodes.
// - R2-P1-1: exportAtomicStorePointerStmt must intercept a renamed
//   sync/atomic import in statement position, like the canonical
//   spelling.
// - R2-P2-9: runtime.Goexit must be intercepted under a renamed import.

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestAtomicWordOpsTableCoversRtatomic: the dispatch table must contain
// the two internal/runtime/atomic entries verbatim from 2438b6f7. Fails
// on the f9735cc2 tree (entries dropped by the rebase).
func TestAtomicWordOpsTableCoversRtatomic(t *testing.T) {
	load, ok := atomicWordOps["LoadAcquintptr"]
	if !ok {
		t.Fatal("atomicWordOps missing LoadAcquintptr (internal/runtime/atomic)")
	}
	if want := (atomicWordOp{"Load", "atomic.Uintptr", "int64", 0}); load != want {
		t.Errorf("LoadAcquintptr = %+v, want %+v", load, want)
	}
	store, ok := atomicWordOps["StoreReluintptr"]
	if !ok {
		t.Fatal("atomicWordOps missing StoreReluintptr (internal/runtime/atomic)")
	}
	if want := (atomicWordOp{"Store", "atomic.Uintptr", "int64", 1}); store != want {
		t.Errorf("StoreReluintptr = %+v, want %+v", store, want)
	}
}

// walkJSONNodes calls f on every object node of a decoded wire-JSON tree.
func walkJSONNodes(n any, f func(map[string]any)) {
	switch x := n.(type) {
	case map[string]any:
		f(x)
		for _, c := range x {
			walkJSONNodes(c, f)
		}
	case []any:
		for _, c := range x {
			walkJSONNodes(c, f)
		}
	}
}

// TestSyncPoolRtatomicLowersToSyncop: exporting corpus 46_sync_pool must
// lower the three sync/pool.go rtatomic call sites (LoadAcquintptr x2,
// StoreReluintptr x1) to atomicaddr+syncop wire nodes. Without the table
// entries they export as plain calls to the internal/runtime/atomic funcs
// (a silent memory-ordering miscompile on amd64: LoadAcquintptr is a plain
// Go `return *ptr` there).
func TestSyncPoolRtatomicLowersToSyncop(t *testing.T) {
	out, err := ExportFile("../../test/differential/corpus/46_sync_pool/main.go")
	if err != nil {
		t.Fatalf("ExportFile(46_sync_pool): %v", err)
	}
	var v any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("wire JSON does not parse: %v", err)
	}
	rtatomic := map[string]bool{"LoadAcquintptr": true, "StoreReluintptr": true}
	syncops := 0
	badCalls := 0
	walkJSONNodes(v, func(m map[string]any) {
		if m["kind"] == "syncop" {
			syncops++
		}
		if m["kind"] == "call" {
			// A surviving plain call to an rtatomic func names it via a
			// descendant ident node carrying both name and obj.
			walkJSONNodes(m, func(inner map[string]any) {
				name, _ := inner["name"].(string)
				_, hasObj := inner["obj"]
				if rtatomic[name] && hasObj {
					badCalls++
				}
			})
		}
	})
	if syncops == 0 {
		t.Error("no syncop wire nodes in 46_sync_pool export")
	}
	if badCalls != 0 {
		t.Errorf("%d plain call nodes still target internal/runtime/atomic funcs", badCalls)
	}
}

// TestAtomicStorePointerStmtRenamedImport (R2-P1-1): a renamed sync/atomic
// import in statement position must be intercepted exactly like the
// canonical spelling — the bare call lowers to *addr = val (assign/deref)
// instead of a plain call to the bodyless StorePointer func.
func TestAtomicStorePointerStmtRenamedImport(t *testing.T) {
	canonical := `package main

import (
	"sync/atomic"
	"unsafe"
)

func main() {
	var p unsafe.Pointer
	var v unsafe.Pointer
	atomic.StorePointer(&p, v)
	_ = p
	_ = v
}
`
	renamed := strings.Replace(canonical, `"sync/atomic"`, `atom "sync/atomic"`, 1)
	renamed = strings.Replace(renamed, "atomic.StorePointer", "atom.StorePointer", 1)
	for _, tc := range []struct{ name, src string }{
		{"canonical", canonical},
		{"renamed", renamed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := ExportFile(writeSyncProg(t, tc.src))
			if err != nil {
				t.Fatalf("ExportFile: %v", err)
			}
			var v any
			if err := json.Unmarshal([]byte(out), &v); err != nil {
				t.Fatalf("wire JSON does not parse: %v", err)
			}
			// Scope to the main package's main funcdecl: the rest of the
			// export legitimately contains sync/atomic's own internals.
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
			plainCalls := 0
			derefs := 0
			walkJSONNodes(mainBody, func(m map[string]any) {
				if m["kind"] == "deref" {
					derefs++
				}
				if m["kind"] == "call" {
					walkJSONNodes(m, func(inner map[string]any) {
						name, _ := inner["name"].(string)
						_, hasObj := inner["obj"]
						if name == "StorePointer" && hasObj {
							plainCalls++
						}
					})
				}
			})
			if plainCalls != 0 {
				t.Errorf("StorePointer call was not intercepted in statement position (%d plain calls)", plainCalls)
			}
			if derefs == 0 {
				t.Error("expected lowered *addr = val (deref node) in main")
			}
		})
	}
}

// TestGoexitRenamedImport (R2-P2-9): rt "runtime"; rt.Goexit() must lower
// to the goexit wire node exactly like the canonical spelling.
func TestGoexitRenamedImport(t *testing.T) {
	canonical := `package main

import "runtime"

func main() {
	runtime.Goexit()
}
`
	renamed := `package main

import rt "runtime"

func main() {
	rt.Goexit()
}
`
	for _, tc := range []struct{ name, src string }{
		{"canonical", canonical},
		{"renamed", renamed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := ExportFile(writeSyncProg(t, tc.src))
			if err != nil {
				t.Fatalf("ExportFile: %v", err)
			}
			if !strings.Contains(out, `"goexit": true`) {
				t.Error("Goexit call was not intercepted to the goexit wire node")
			}
		})
	}
}
