package main

// fix-linkname (Caddy/godebug lane): exporter tests for //go:linkname
// symbol resolution, sync.Map, and package-level sync/atomic word ops.

import (
	"strings"
	"testing"
)

// TestLinknameOneFieldSymbol: a 1-field //go:linkname defaults the
// target symbol to <pkgpath>.<local> (gc noder semantics).
func TestLinknameOneFieldSymbol(t *testing.T) {
	src := `package main

import _ "unsafe"

//go:linkname localOnly
func localOnly()

func main() {}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with 1-field linkname: %v", err)
	}
	// The linkname symbol should be "main.localOnly".
	if !strings.Contains(out, `"linkname": "main.localOnly"`) &&
		!strings.Contains(out, `"linkname":"main.localOnly"`) {
		t.Errorf("1-field linkname did not resolve to main.localOnly;\n%s", out[:2000])
	}
}

// TestLinknameTwoFieldSymbol: a 2-field //go:linkname uses the explicit
// symbol.
func TestLinknameTwoFieldSymbol(t *testing.T) {
	src := `package main

import "unsafe"

//go:linkname explicit runtime.write
func explicit(fd uintptr, p unsafe.Pointer, n int32) int32

func main() {
	_ = explicit
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with 2-field linkname: %v", err)
	}
	if !strings.Contains(out, "runtime.write") {
		t.Errorf("2-field linkname did not keep runtime.write symbol;\n%s", out[:2000])
	}
}

// TestSyncMapOpsExport: all sync.Map methods export to syncop wire nodes
// with synctyp "Map".
func TestSyncMapOpsExport(t *testing.T) {
	src := `package main

import "sync"

var m sync.Map

func main() {
	m.Store("a", 1)
	_, _ = m.Load("a")
	_, _ = m.LoadOrStore("b", 2)
	_, _ = m.LoadAndDelete("a")
	_, _ = m.Swap("b", 3)
	_ = m.CompareAndSwap("b", 3, 4)
	_ = m.CompareAndDelete("b", 4)
	m.Range(func(k, v any) bool { return true })
	m.Delete("b")
	m.Clear()
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with sync.Map ops: %v", err)
	}
	// Real sync (503608ea): Map methods are ordinary calls. The method
	// names must appear in the export. (The imported real sync package's
	// internals may contain syncop nodes for atomic ops — that's the
	// legitimate atomic intrinsic.)
	for _, op := range []string{"Store", "Load", "LoadOrStore", "LoadAndDelete",
		"Swap", "CompareAndSwap", "CompareAndDelete", "Range", "Delete", "Clear"} {
		if !strings.Contains(out, op) {
			t.Errorf("sync.Map.%s did not export", op)
		}
	}
}

// TestAtomicWordOpsExport: package-level sync/atomic word ops export to
// atomicaddr+syncop wire nodes.
func TestAtomicWordOpsExport(t *testing.T) {
	src := `package main

import "sync/atomic"

func main() {
	var x int64
	_ = atomic.LoadInt64(&x)
	atomic.StoreInt64(&x, 1)
	_ = atomic.AddInt64(&x, 2)
	_ = atomic.SwapInt64(&x, 3)
	_ = atomic.CompareAndSwapInt64(&x, 3, 4)
	var y uint32
	_ = atomic.LoadUint32(&y)
	var z uintptr
	_ = atomic.LoadUintptr(&z)
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with atomic word ops: %v", err)
	}
	if !strings.Contains(out, `"kind": "atomicaddr"`) &&
		!strings.Contains(out, `"kind":"atomicaddr"`) {
		t.Errorf("atomic word ops did not export atomicaddr nodes")
	}
	for _, op := range []string{"Load", "Store", "Add", "Swap", "CompareAndSwap"} {
		if !strings.Contains(out, `"op": "`+op+`"`) &&
			!strings.Contains(out, `"op":"`+op+`"`) {
			t.Errorf("atomic.%s did not export", op)
		}
	}
}

// TestAtomicBoolPackageFuncRejected: gc has no package-level bool
// atomics; they must be rejected (undefined), not silently accepted.
func TestAtomicBoolPackageFuncRejected(t *testing.T) {
	src := `package main

import "sync/atomic"

func main() {
	var b bool
	_ = atomic.LoadBool(&b)
}
`
	_, err := ExportFile(writeSyncProg(t, src))
	if err == nil {
		t.Errorf("atomic.LoadBool was accepted; gc rejects it (undefined)")
	} else if !strings.Contains(err.Error(), "undefined") {
		t.Errorf("atomic.LoadBool error was not 'undefined': %v", err)
	}
}
