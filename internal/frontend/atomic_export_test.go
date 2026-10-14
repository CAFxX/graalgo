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
// export; constructions lower to syncnew and method calls to syncop
// wire nodes with the atomic.Pointer/atomic.Value synctyps.
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
	for _, want := range []string{
		`"kind": "syncnew"`,
		`"kind": "syncop"`,
		`"synctyp": "atomic.Pointer"`,
		`"synctyp": "atomic.Value"`,
		`"synctyp": "atomic.Int64"`,
		`"op": "Load"`,
		`"op": "Store"`,
		`"op": "Swap"`,
		`"op": "CompareAndSwap"`,
		// The instantiation is named with its type argument.
		`sync/atomic.Pointer[string]`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("exported JSON missing %s", want)
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

// TestAtomicDeferSynctyp: deferred atomic ops must carry the
// atomic.-qualified synctyp (the pre-P3-M10 bare-name form misrouted
// them to the sync.* SyncOpNode).
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
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with deferred atomic ops: %v", err)
	}
	for _, want := range []string{
		`"synctyp": "atomic.Int64"`,
		`"synctyp": "atomic.Pointer"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("exported JSON missing %s", want)
		}
	}
	if strings.Contains(out, `"synctyp": "Int64"`) {
		t.Errorf("exported JSON has bare-name synctyp Int64 (misroutes to SyncOpNode)")
	}
}
