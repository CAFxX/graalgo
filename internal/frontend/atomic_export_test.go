package main

// RV-P0 (fix-p0-atomic-lowering): exporter tests for the restored
// method-call -> "syncop" lowering on the REAL sync/atomic wrapper types
// (atomic.Value, atomic.Pointer[T], and the typed int/uint/bool wrappers).
//
// Red-green: before the restoration, v.Store(x) fell through to the real
// GOROOT method body (a "methodref"+"call" to a funcdecl compiled from
// unsafe.Pointer/asm-stub sources); after, it lowers to a "syncop" wire
// node with synctyp "atomic.<Name>" that the intact backend (AtomicOpNode)
// already builds.

import (
	"encoding/json"
	"strings"
	"testing"
)

// mainBodySyncops exports src and returns the (synctyp, op) pairs of all
// "syncop" wire nodes found in the body of func main. Scoping to main's
// body (not the whole document) keeps the assertions red-green: the dead
// funcdecls of the real sync/atomic method bodies also contain syncops
// (from the in-package word-op interceptions, PKG-012), which are present
// both before and after the restoration.
func mainBodySyncops(t *testing.T, src string) [][2]string {
	t.Helper()
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("decoding export JSON: %v", err)
	}
	var mainBody any
	for _, f := range doc["funcs"].([]any) {
		fields := f.(map[string]any)["fields"].(map[string]any)
		if fields["name"] == "main" {
			mainBody = fields["body"]
		}
	}
	if mainBody == nil {
		t.Fatal("no func main in export")
	}
	var ops [][2]string
	walkJSONNodes(mainBody, func(m map[string]any) {
		if m["kind"] == "syncop" {
			f := m["fields"].(map[string]any)
			ops = append(ops, [2]string{f["synctyp"].(string), f["op"].(string)})
		}
	})
	return ops
}

// mainBodyHasMethodRef reports whether func main's body contains any
// "methodref" wire node.
func mainBodyHasMethodRef(t *testing.T, src string) bool {
	t.Helper()
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("decoding export JSON: %v", err)
	}
	found := false
	for _, f := range doc["funcs"].([]any) {
		fields := f.(map[string]any)["fields"].(map[string]any)
		if fields["name"] == "main" {
			walkJSONNodes(fields["body"], func(m map[string]any) {
				if m["kind"] == "methodref" {
					found = true
				}
			})
		}
	}
	return found
}

// syncopCount counts (synctyp, op) pairs.
func syncopCount(ops [][2]string) map[[2]string]int {
	c := map[[2]string]int{}
	for _, op := range ops {
		c[op]++
	}
	return c
}

// TestAtomicValueMethodCallsLowerToSyncop: the four atomic.Value methods
// lower to syncop wire nodes with synctyp "atomic.Value" (not to
// methodref+call against the real, uncompilable GOROOT body).
func TestAtomicValueMethodCallsLowerToSyncop(t *testing.T) {
	src := `package main

import "sync/atomic"

func main() {
	var v atomic.Value
	v.Store("y")
	_ = v.Load()
	_ = v.Swap(1)
	_ = v.CompareAndSwap(1, 2)
}
`
	ops := syncopCount(mainBodySyncops(t, src))
	for _, op := range []string{"Store", "Load", "Swap", "CompareAndSwap"} {
		if ops[[2]string{"atomic.Value", op}] != 1 {
			t.Errorf("want exactly one syncop atomic.Value.%s in main body, got %d (all: %v)",
				op, ops[[2]string{"atomic.Value", op}], ops)
		}
	}
	if mainBodyHasMethodRef(t, src) {
		t.Error("main body contains a methodref: atomic.Value calls must lower to syncop, not to the real method body")
	}
}

// TestAtomicPointerMethodCallsLowerToSyncop: the four atomic.Pointer[T]
// methods lower to syncop wire nodes with synctyp "atomic.Pointer",
// including through a new(atomic.Pointer[node]) receiver.
func TestAtomicPointerMethodCallsLowerToSyncop(t *testing.T) {
	src := `package main

import "sync/atomic"

type node struct{ n int }

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
}
`
	ops := syncopCount(mainBodySyncops(t, src))
	for _, op := range []string{"Store", "Load", "Swap", "CompareAndSwap"} {
		if ops[[2]string{"atomic.Pointer", op}] < 1 {
			t.Errorf("want at least one syncop atomic.Pointer.%s in main body (all: %v)", op, ops)
		}
	}
	// Two Stores: p.Store and q.Store (the new() receiver).
	if ops[[2]string{"atomic.Pointer", "Store"}] != 2 {
		t.Errorf("want 2 syncop atomic.Pointer.Store (p and new() receiver), got %d",
			ops[[2]string{"atomic.Pointer", "Store"}])
	}
	if mainBodyHasMethodRef(t, src) {
		t.Error("main body contains a methodref: atomic.Pointer calls must lower to syncop")
	}
}

// TestAtomicIntWrappersLowerToSyncop: the typed int/uint wrappers lower
// their gc method sets to syncop nodes (Int64 fully, Int32 and Bool for
// kind coverage).
func TestAtomicIntWrappersLowerToSyncop(t *testing.T) {
	src := `package main

import "sync/atomic"

func main() {
	var i atomic.Int64
	i.Store(3)
	i.Add(1)
	_ = i.Swap(2)
	_ = i.Load()
	_ = i.CompareAndSwap(1, 2)
	var j atomic.Int32
	j.Store(1)
	_ = j.Load()
	var b atomic.Bool
	b.Store(true)
	_ = b.Load()
	_ = b.Swap(false)
	_ = b.CompareAndSwap(true, false)
}
`
	ops := syncopCount(mainBodySyncops(t, src))
	for _, op := range []string{"Store", "Add", "Swap", "Load", "CompareAndSwap"} {
		if ops[[2]string{"atomic.Int64", op}] != 1 {
			t.Errorf("want exactly one syncop atomic.Int64.%s, got %d (all: %v)",
				op, ops[[2]string{"atomic.Int64", op}], ops)
		}
	}
	for _, op := range []string{"Store", "Load"} {
		if ops[[2]string{"atomic.Int32", op}] != 1 {
			t.Errorf("want exactly one syncop atomic.Int32.%s, got %d", op, ops[[2]string{"atomic.Int32", op}])
		}
	}
	for _, op := range []string{"Store", "Load", "Swap", "CompareAndSwap"} {
		if ops[[2]string{"atomic.Bool", op}] != 1 {
			t.Errorf("want exactly one syncop atomic.Bool.%s, got %d", op, ops[[2]string{"atomic.Bool", op}])
		}
	}
	if mainBodyHasMethodRef(t, src) {
		t.Error("main body contains a methodref: typed wrapper calls must lower to syncop")
	}
}

// TestAtomicAndOrLowerToSyncop: And/Or on the int/uint wrappers lower to
// syncop nodes (DESIGN-RV-P0-atomic-lowering §5.5 option B: the backend
// AtomicOpNode implements them; previously the real bodies called the
// bodyless AndInt64/OrInt64 asm stubs and panicked at runtime).
func TestAtomicAndOrLowerToSyncop(t *testing.T) {
	src := `package main

import "sync/atomic"

func main() {
	var i atomic.Int64
	_ = i.And(6)
	_ = i.Or(1)
	var u atomic.Uintptr
	_ = u.And(6)
	_ = u.Or(1)
}
`
	ops := syncopCount(mainBodySyncops(t, src))
	for _, typ := range []string{"atomic.Int64", "atomic.Uintptr"} {
		for _, op := range []string{"And", "Or"} {
			if ops[[2]string{typ, op}] != 1 {
				t.Errorf("want exactly one syncop %s.%s, got %d (all: %v)",
					typ, op, ops[[2]string{typ, op}], ops)
			}
		}
	}
	if mainBodyHasMethodRef(t, src) {
		t.Error("main body contains a methodref: And/Or must lower to syncop, not to the asm-stub bodies")
	}
}

// mainBodySyncnews exports src and returns the synctyp of every "syncnew"
// wire node in func main's body plus the package-level globals.
func mainBodySyncnews(t *testing.T, src string) []string {
	t.Helper()
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("decoding export JSON: %v", err)
	}
	var synctyps []string
	collect := func(n any) {
		walkJSONNodes(n, func(m map[string]any) {
			if m["kind"] == "syncnew" {
				synctyps = append(synctyps, m["fields"].(map[string]any)["synctyp"].(string))
			}
		})
	}
	for _, f := range doc["funcs"].([]any) {
		fields := f.(map[string]any)["fields"].(map[string]any)
		if fields["name"] == "main" {
			collect(fields["body"])
		}
	}
	for _, g := range doc["globals"].([]any) {
		collect(g)
	}
	return synctyps
}

// TestAtomicSyncnewMaterialization: `var v atomic.Value` (no init),
// `atomic.Value{}` and `new(atomic.Value)` materialize the Java atomic
// cell via "syncnew" — gc's ready-to-use zero value — instead of the
// factory's unusable empty-struct zero.
func TestAtomicSyncnewMaterialization(t *testing.T) {
	src := `package main

import "sync/atomic"

var gp atomic.Pointer[string]

func main() {
	var v atomic.Value
	_ = v
	w := atomic.Value{}
	_ = w
	u := new(atomic.Value)
	_ = u
	var i atomic.Int64
	_ = i
	_ = gp
}
`
	count := map[string]int{}
	for _, s := range mainBodySyncnews(t, src) {
		count[s]++
	}
	if count["atomic.Value"] != 3 {
		t.Errorf("want 3 syncnew atomic.Value (var, literal, new), got %d (all: %v)", count["atomic.Value"], count)
	}
	if count["atomic.Int64"] != 1 {
		t.Errorf("want 1 syncnew atomic.Int64 (var), got %d (all: %v)", count["atomic.Int64"], count)
	}
	if count["atomic.Pointer"] != 1 {
		t.Errorf("want 1 syncnew atomic.Pointer (package-level var), got %d (all: %v)", count["atomic.Pointer"], count)
	}
}

// TestAtomicMethodValueLowersToFunclit: a method value f := v.Store
// lowers to a funclit capturing the receiver whose body performs the
// syncop (not a methodvalue/methodref against the real body).
func TestAtomicMethodValueLowersToFunclit(t *testing.T) {
	src := `package main

import "sync/atomic"

func main() {
	var v atomic.Value
	f := v.Store
	f("x")
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("decoding export JSON: %v", err)
	}
	var mainBody any
	for _, f := range doc["funcs"].([]any) {
		fields := f.(map[string]any)["fields"].(map[string]any)
		if fields["name"] == "main" {
			mainBody = fields["body"]
		}
	}
	funclits, syncops, methodrefs := 0, 0, 0
	walkJSONNodes(mainBody, func(m map[string]any) {
		switch m["kind"] {
		case "funclit":
			funclits++
		case "syncop":
			if m["fields"].(map[string]any)["synctyp"] == "atomic.Value" {
				syncops++
			}
		case "methodref", "methodvalue":
			methodrefs++
		}
	})
	if funclits != 1 {
		t.Errorf("want 1 funclit for the method value, got %d", funclits)
	}
	if syncops != 1 {
		t.Errorf("want 1 syncop atomic.Value in the funclit body, got %d", syncops)
	}
	if methodrefs != 0 {
		t.Errorf("want no methodref/methodvalue nodes, got %d", methodrefs)
	}
	if !strings.Contains(out, "__primrecv_") {
		t.Error("exported JSON missing the hoisted __primrecv_ receiver temporary")
	}
}

// TestAtomicDeferGoLowerToSyncop: `defer v.Store(x)` / `go v.Store(x)`
// desugar to a defer/gostmt whose funclit body performs the syncop (the
// frozen buildDefer/GoStatementNode only understand funclit calls).
func TestAtomicDeferGoLowerToSyncop(t *testing.T) {
	src := `package main

import "sync/atomic"

func main() {
	var v atomic.Value
	defer v.Store("d")
	var p atomic.Pointer[int]
	x := 1
	p.Store(&x)
	go p.Store(&x)
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with deferred/spawned atomic ops: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("decoding export JSON: %v", err)
	}
	var mainBody any
	for _, f := range doc["funcs"].([]any) {
		fields := f.(map[string]any)["fields"].(map[string]any)
		if fields["name"] == "main" {
			mainBody = fields["body"]
		}
	}
	// Each defer/gostmt subtree must contain its syncop.
	checks := map[string]bool{"defer": false, "gostmt": false}
	walkJSONNodes(mainBody, func(m map[string]any) {
		if k := m["kind"]; k == "defer" || k == "gostmt" {
			hasSyncop := false
			walkJSONNodes(m, func(inner map[string]any) {
				if inner["kind"] == "syncop" {
					hasSyncop = true
				}
			})
			if hasSyncop {
				checks[k.(string)] = true
			}
		}
	})
	for k, ok := range checks {
		if !ok {
			t.Errorf("want a %q node whose subtree contains the syncop", k)
		}
	}
	if !strings.Contains(out, "__sync_go_") {
		t.Error("exported JSON missing the __sync_go_ hoisted go-time temporaries")
	}
}

// TestAtomicValueAddRejectedByTypeChecker: atomic.Value has no Add in gc,
// so go/types rejects v.Add(1) with "has no field or method" — proving the
// detector does not over-match unknown methods into the syncop path.
func TestAtomicValueAddRejectedByTypeChecker(t *testing.T) {
	src := `package main

import "sync/atomic"

func main() {
	var v atomic.Value
	v.Add(1)
}
`
	if _, err := ExportFile(writeSyncProg(t, src)); err == nil {
		t.Error("ExportFile accepted v.Add(1) on atomic.Value: want type error")
	} else if !strings.Contains(err.Error(), "has no field or method") {
		t.Errorf("ExportFile wrong error for v.Add: %v", err)
	}
}

// TestAtomicDetectorIsPackageScoped: a user type named Value in another
// package keeps its own methods (no interception), while a type alias
// for atomic.Value IS the wrapper and lowers to syncop.
func TestAtomicDetectorIsPackageScoped(t *testing.T) {
	src := `package main

import "sync/atomic"

type Value struct{ x int }

func (v *Value) Store(x int) { v.x = x }

type V = atomic.Value

func main() {
	var u Value
	u.Store(1)
	var w V
	w.Store("aliased")
}
`
	ops := syncopCount(mainBodySyncops(t, src))
	if ops[[2]string{"atomic.Value", "Store"}] != 1 {
		t.Errorf("want exactly one syncop atomic.Value.Store (the alias), got %d (all: %v)",
			ops[[2]string{"atomic.Value", "Store"}], ops)
	}
	// The user type's own Store must go through the normal method path.
	if !mainBodyHasMethodRef(t, src) {
		t.Error("want a methodref for the user-defined Value.Store (must not be intercepted)")
	}
}

// TestAtomicPointerValueAccepted: Pointer[T] instantiations and Value
// export as usable Go types; their method calls lower to syncop wire
// nodes (restored RV-P0 lowering) rather than to the real,
// uncompilable GOROOT method bodies.
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
	// Every method call on the wrappers lowers to a syncop — none may
	// reach the real method bodies via methodref.
	if mainBodyHasMethodRef(t, src) {
		t.Error("main body contains a methodref: wrapper calls must lower to syncop")
	}
	ops := syncopCount(mainBodySyncops(t, src))
	if ops[[2]string{"atomic.Pointer", "Store"}] != 2 {
		t.Errorf("want 2 syncop atomic.Pointer.Store, got %d", ops[[2]string{"atomic.Pointer", "Store"}])
	}
	if ops[[2]string{"atomic.Value", "Store"}] != 1 {
		t.Errorf("want 1 syncop atomic.Value.Store, got %d", ops[[2]string{"atomic.Value", "Store"}])
	}
	if ops[[2]string{"atomic.Int64", "Store"}] != 1 {
		t.Errorf("want 1 syncop atomic.Int64.Store, got %d", ops[[2]string{"atomic.Int64", "Store"}])
	}
}

// TestAtomicPointerGenericChecked: the Pointer[T] method signatures are
// really instantiated — a *string argument must not type-check against
// Pointer[int]'s *T parameter, and Add (absent in gc) must fail fast
// with the gc type error.
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
	// it with "has no field or method" — the gc-faithful error. (And/Or
	// on the typed int/uint wrappers DO exist in gc and lower to syncop
	// per TestAtomicAndOrLowerToSyncop.)
	if _, err := ExportFile(writeSyncProg(t, badAdd)); err == nil {
		t.Errorf("ExportFile accepted Pointer.Add: want type error")
	} else if !strings.Contains(err.Error(), "has no field or method") {
		t.Errorf("ExportFile wrong error for Pointer.Add: %v", err)
	}
}

// TestAtomicDeferSynctyp: deferred and spawned atomic ops lower to
// defer/gostmt nodes whose funclit bodies perform the syncop (restored
// RV-P0 lowering) — not to methodref calls against the real bodies.
func TestAtomicDeferSynctyp(t *testing.T) {
	src := `package main

import "sync/atomic"

func main() {
	var i atomic.Int64
	defer i.Store(1)
	var p atomic.Pointer[string]
	s := "x"
	defer p.Store(&s)
	go i.Add(2)
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with deferred atomic ops: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("decoding export JSON: %v", err)
	}
	var mainBody any
	for _, f := range doc["funcs"].([]any) {
		fields := f.(map[string]any)["fields"].(map[string]any)
		if fields["name"] == "main" {
			mainBody = fields["body"]
		}
	}
	syncops := 0
	walkJSONNodes(mainBody, func(m map[string]any) {
		if m["kind"] == "syncop" {
			syncops++
		}
	})
	// defer i.Store, defer p.Store, go i.Add.
	if syncops != 3 {
		t.Errorf("want 3 syncop nodes in main body (2 deferred + 1 spawned), got %d", syncops)
	}
	if mainBodyHasMethodRef(t, src) {
		t.Error("main body contains a methodref: deferred/spawned wrapper calls must lower to syncop")
	}
}
