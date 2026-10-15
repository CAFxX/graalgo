package main

// P3-M10: exporter tests for the generic sync/atomic additions
// (atomic.Pointer[T], atomic.Value).
// Red-green: these fail before the fix (import "sync/atomic" has no
// Pointer/Value — "undefined: atomic.Pointer") and pass after.

import (
	"strings"
	"testing"
)

// TestAtomicPointerValueAccepted: Pointer[T] instantiations and Value
// export as normal Go types and method calls (real sync/atomic package,
// 503608ea). Previously these lowered to syncnew/syncop wire nodes under
// the synthetic atomic package.
func TestAtomicPointerValueAccepted(t *testing.T) {
	src := `package main

import "sync/atomic"

type node struct{ n int }

var gp atomic.Pointer[string]

func main() {
	var p atomic.Pointer[string]
	s := "x"
	p.Store(&s)
	_ = p.Load()
	_ = p.Swap(&s)
	_ = p.CompareAndSwap(&s, &s)
	q := new(atomic.Pointer[node])
	q.Store(&node{n: 1})
	_ = q.Load()
	r := atomic.Pointer[int]{}
	_ = r
	var v atomic.Value
	v.Store("y")
	_ = v.Load()
	_ = v.Swap(1)
	_ = v.CompareAndSwap(1, 2)
	var i atomic.Int64
	i.Store(3)
	_ = i.Load()
	_ = gp
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with atomic.Pointer/atomic.Value: %v", err)
	}
	// The instantiation is named with its type argument.
	if !strings.Contains(out, `sync/atomic.Pointer[string]`) {
		t.Errorf("exported JSON missing sync/atomic.Pointer[string]")
	}
	// The method names must appear as call targets.
	for _, want := range []string{"Load", "Store", "Swap", "CompareAndSwap"} {
		if !strings.Contains(out, want) {
			t.Errorf("exported JSON missing method %s", want)
		}
	}
}

// TestAtomicPointerGenericChecked: the Pointer[T] method signatures are
// really instantiated — a *string argument must not type-check against
// Pointer[int]'s *T parameter, and Add (absent in gc) must fail fast
// with the named M4 scope error.
func TestAtomicPointerGenericChecked(t *testing.T) {
	badStore := `package main

import "sync/atomic"

func main() {
	var p atomic.Pointer[int]
	s := "x"
	p.Store(&s)
}
`
	if _, err := ExportFile(writeSyncProg(t, badStore)); err == nil {
		t.Errorf("ExportFile accepted p.Store(&s) for atomic.Pointer[int]: want type error")
	} else if !strings.Contains(err.Error(), "cannot use") {
		t.Errorf("ExportFile wrong error for mistyped Store: %v", err)
	}

	badAdd := `package main

import "sync/atomic"

func main() {
	var p atomic.Pointer[int]
	x := 1
	p.Store(&x)
	p.Add(&x)
}
`
	// gc has no Add on Pointer[T] at all, so the type checker rejects
	// it with "has no field or method" — the gc-faithful error (unlike
	// And/Or on the typed wrappers, which exist in gc but are out of
	// M4 scope and get the named scope error).
	if _, err := ExportFile(writeSyncProg(t, badAdd)); err == nil {
		t.Errorf("ExportFile accepted Pointer.Add: want type error")
	} else if !strings.Contains(err.Error(), "has no field or method") {
		t.Errorf("ExportFile wrong error for Pointer.Add: %v", err)
	}
}

// TestAtomicDeferSynctyp: deferred atomic ops export successfully
// (real sync/atomic package, 503608ea).
func TestAtomicDeferSynctyp(t *testing.T) {
	src := `package main

import "sync/atomic"

func main() {
	var i atomic.Int64
	defer i.Store(1)
	var p atomic.Pointer[string]
	s := "x"
	defer p.Store(&s)
}
`
	_, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with deferred atomic ops: %v", err)
	}
}
