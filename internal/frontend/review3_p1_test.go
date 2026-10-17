package main

// P1-F1/P1-F2 regression tests (lane review3-frontend, independent
// codebase review 2026-10-17).
//
// P1-F1: a top-level `var x = pkg.F` where F is multi-result discarded
// its alias maps at ExportFiles (`, _ = etaExpandMRFuncValues`), so the
// declaration was removed from the AST but call sites through x could
// not desugar: `variable "lstat" used before declaration` on legal Go.
// After the fix the maps are absorbed into the exporter and the call
// desugars exactly like the direct pkg.F call.
//
// P1-F2: atomic.LoadPointer/StorePointer lowered to plain deref
// read/write nodes, silently dropping gc's ordering/visibility
// guarantees (the backend DerefNode/DerefWriteNode are non-volatile).
// After the fix they lower through the atomicaddr+syncop machinery
// (synctyp "atomic.UnsafePointer", cell "ptr64") like the word ops.

import (
	"testing"
)

// funcBodyByName returns the body node of the named funcdecl in the
// decoded doc (searches doc["funcs"]).
func funcBodyByName(t *testing.T, doc map[string]any, name string) map[string]any {
	t.Helper()
	funcs, _ := doc["funcs"].([]any)
	for _, f := range funcs {
		fm, _ := f.(map[string]any)
		fields, _ := fm["fields"].(map[string]any)
		if fields["name"] == name {
			body, _ := fields["body"].(map[string]any)
			return body
		}
	}
	t.Fatalf("no funcdecl %q in wire doc", name)
	return nil
}

// TestTopLevelVarFuncAliasDesugars pins P1-F1: `var lstat = os.Lstat`
// at package level in the user's own package must export, and the call
// through the alias must desugar through the __lstat disk helper with a
// __p5mr_N pair temp — exactly like the direct os.Lstat call.
func TestTopLevelVarFuncAliasDesugars(t *testing.T) {
	doc := exportFuncValueProg(t, `package main

import "os"

var lstat = os.Lstat

func main() {
	fi, err := lstat("/tmp")
	println(fi.Name(), err == nil)
}
`)
	body := funcBodyByName(t, doc, "main")
	// The call desugars: a __p5mr_N temp holds the __lstat helper call,
	// then field extractions assign fi and err.
	foundHelper := false
	for _, n := range findAllKind(body, "vardecl") {
		fields, _ := n["fields"].(map[string]any)
		names, _ := fields["names"].([]any)
		if len(names) != 1 {
			continue
		}
		name, _ := names[0].(string)
		if len(name) < 7 || name[:7] != "__p5mr_" {
			continue
		}
		vals, _ := fields["values"].([]any)
		if len(vals) != 1 {
			continue
		}
		call, _ := vals[0].(map[string]any)
		if call["kind"] != "call" {
			continue
		}
		fun, _ := call["fields"].(map[string]any)["fun"].(map[string]any)
		if fun == nil || fun["kind"] != "ident" {
			continue
		}
		if fun["fields"].(map[string]any)["name"] != "__lstat" {
			continue
		}
		foundHelper = true
	}
	if !foundHelper {
		t.Fatal("alias call did not desugar through the __lstat helper " +
			"(no __p5mr_N := __lstat(...) pair-temp vardecl)")
	}
	// The removed declaration must leave no dangling references: no
	// ident node may still name the alias variable.
	for _, n := range findAllKind(body, "ident") {
		if n["fields"].(map[string]any)["name"] == "lstat" {
			t.Fatal("dangling ident reference to removed alias var \"lstat\"")
		}
	}
}

// TestTopLevelVarFuncAliasUserMR pins P1-F1 for the pair-transformed
// (userMRFuncs) alias route: `var cut = strings.Cut` must export, and
// the call through the alias must desugar exactly like the direct
// strings.Cut call. The importer shares package objects across
// typechecks, so the aliased Func is the same object the direct call
// resolves to.
func TestTopLevelVarFuncAliasUserMR(t *testing.T) {
	doc := exportFuncValueProg(t, `package main

import "strings"

var cut = strings.Cut

func main() {
	before, after, found := cut("a:b", ":")
	println(before, after, found)
}
`)
	body := funcBodyByName(t, doc, "main")
	foundPair := false
	for _, n := range findAllKind(body, "vardecl") {
		fields, _ := n["fields"].(map[string]any)
		names, _ := fields["names"].([]any)
		if len(names) != 1 {
			continue
		}
		name, _ := names[0].(string)
		if len(name) < 7 || name[:7] != "__p5mr_" {
			continue
		}
		vals, _ := fields["values"].([]any)
		if len(vals) != 1 {
			continue
		}
		call, _ := vals[0].(map[string]any)
		if call["kind"] != "call" {
			continue
		}
		fun, _ := call["fields"].(map[string]any)["fun"].(map[string]any)
		if fun == nil || fun["kind"] != "ident" {
			continue
		}
		// The pair-transformed strings.Cut funcdecl is named "Cut" in
		// the wire (the alias call must lower to it directly, exactly
		// like the direct strings.Cut call).
		if fun["fields"].(map[string]any)["name"] != "Cut" {
			continue
		}
		foundPair = true
	}
	if !foundPair {
		t.Fatal("alias call did not desugar to the pair-transformed Cut " +
			"(no __p5mr_N := Cut(...) pair-temp vardecl)")
	}
	for _, n := range findAllKind(body, "ident") {
		if n["fields"].(map[string]any)["name"] == "cut" {
			t.Fatal("dangling ident reference to removed alias var \"cut\"")
		}
	}
}

// TestAtomicPointerTypedBodiesLowerToSyncop pins the P1-F2 bonus fix:
// the instantiated (*atomic.Pointer[T]).Load/Store bodies must lower
// through atomicaddr+syncop (synctyp "atomic.UnsafePointer") — before
// the fix they emitted plain calls to the bodyless unimplemented
// funcdecls and panicked at runtime.
func TestAtomicPointerTypedBodiesLowerToSyncop(t *testing.T) {
	doc := exportFuncValueProg(t, `package main

import "sync/atomic"

func main() {
	var slot atomic.Pointer[int]
	y := 7
	slot.Store(&y)
	z := slot.Load()
	println(*z)
}
`)
	funcs, _ := doc["funcs"].([]any)
	want := map[string]string{
		"*sync/atomic.Pointer[int].Load":  "Load",
		"*sync/atomic.Pointer[int].Store": "Store",
	}
	seen := map[string]bool{}
	for _, f := range funcs {
		fm, _ := f.(map[string]any)
		name, _ := fm["fields"].(map[string]any)["name"].(string)
		wantOp, ok := want[name]
		if !ok {
			continue
		}
		seen[name] = true
		body, _ := fm["fields"].(map[string]any)["body"].(map[string]any)
		syncops := syncopNodesByOp(body, wantOp, "atomic.UnsafePointer")
		if len(syncops) != 1 {
			t.Errorf("%s: want 1 syncop %s/atomic.UnsafePointer, got %d",
				name, wantOp, len(syncops))
			continue
		}
		checkPointerSyncop(t, syncops[0])
		for _, n := range findAllKind(body, "call") {
			t.Errorf("%s: plain call node remains in body: %v",
				name, n["fields"])
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("instantiated body %q not found in wire doc", name)
		}
	}
}

// syncopNodesByOp returns the syncop nodes in body with the given op and
// synctyp.
func syncopNodesByOp(body map[string]any, op, synctyp string) []map[string]any {
	var out []map[string]any
	for _, n := range findAllKind(body, "syncop") {
		fields, _ := n["fields"].(map[string]any)
		if fields["op"] == op && fields["synctyp"] == synctyp {
			out = append(out, n)
		}
	}
	return out
}

// checkPointerSyncop asserts a syncop node uses the atomicaddr cell
// machinery: recv is an atomicaddr node with cell "ptr64".
func checkPointerSyncop(t *testing.T, n map[string]any) {
	t.Helper()
	fields, _ := n["fields"].(map[string]any)
	recv, _ := fields["recv"].(map[string]any)
	if recv["kind"] != "atomicaddr" {
		t.Fatalf("syncop recv is %q, want atomicaddr", recv["kind"])
	}
	if cell, _ := recv["fields"].(map[string]any)["cell"].(string); cell != "ptr64" {
		t.Fatalf("atomicaddr cell is %q, want ptr64", cell)
	}
}

// TestAtomicPointerOpsLowerToSyncop pins P1-F2: package-level
// atomic.LoadPointer/StorePointer must lower through atomicaddr+syncop
// (synctyp "atomic.UnsafePointer"), never to plain deref nodes.
func TestAtomicPointerOpsLowerToSyncop(t *testing.T) {
	doc := exportFuncValueProg(t, `package main

import (
	"sync/atomic"
	"unsafe"
)

var p unsafe.Pointer

func main() {
	var x int
	atomic.StorePointer(&p, unsafe.Pointer(&x))
	q := atomic.LoadPointer(&p)
	_ = q
}
`)
	body := funcBodyByName(t, doc, "main")
	loads := syncopNodesByOp(body, "Load", "atomic.UnsafePointer")
	if len(loads) != 1 {
		t.Fatalf("want 1 syncop Load/atomic.UnsafePointer, got %d", len(loads))
	}
	checkPointerSyncop(t, loads[0])
	stores := syncopNodesByOp(body, "Store", "atomic.UnsafePointer")
	if len(stores) != 1 {
		t.Fatalf("want 1 syncop Store/atomic.UnsafePointer, got %d", len(stores))
	}
	checkPointerSyncop(t, stores[0])
	// The old lowering emitted plain deref reads (Load) and
	// assign-to-deref writes (Store); none may remain in main.
	for _, n := range findAllKind(body, "deref") {
		t.Fatalf("plain deref node remains for atomic pointer op: %v", n["fields"])
	}
}

// TestFuncLocalVarFuncAliasNotShadowed pins the independent-review
// VETO fix on review3-frontend: a FUNCTION-LOCAL `var lstat = os.Lstat`
// must NOT be recorded as a var-func alias. Alias recording is
// package-level only; the alias path hardwires the call to the
// originally-aliased func (here __lstat) and silently ignores a later
// reassignment (lstat = os.Stat), which gc honors. Before the fix the
// call desugared through the __lstat alias; after the fix it goes
// through the variable (var-link path), matching base/gc behavior.
func TestFuncLocalVarFuncAliasNotShadowed(t *testing.T) {
	doc := exportFuncValueProg(t, `package main

import "os"

func main() {
	var lstat = os.Lstat
	lstat = os.Stat
	fi, err := lstat("/tmp")
	println(fi.Name(), err == nil)
}
`)
	body := funcBodyByName(t, doc, "main")
	// The alias path hardwires the call to the __lstat helper. That call
	// fun ident must not appear anywhere in main: the call must go
	// through the (reassigned) variable.
	foundVarCall := false
	for _, n := range findAllKind(body, "call") {
		fun, _ := n["fields"].(map[string]any)["fun"].(map[string]any)
		if fun == nil || fun["kind"] != "ident" {
			continue
		}
		name, _ := fun["fields"].(map[string]any)["name"].(string)
		if name == "__lstat" {
			t.Fatal("call desugared through the __lstat alias, shadowing " +
				"the reassigned function-local variable (silent miscompile)")
		}
		if name == "lstat" {
			foundVarCall = true
		}
	}
	if !foundVarCall {
		t.Fatal("no call through the lstat variable: reassignment not respected")
	}
	// The local variable must still be declared (var-link path, like
	// base) — the alias machinery must not have consumed it.
	foundLocal := false
	for _, n := range findAllKind(body, "vardecl") {
		for _, nm := range n["fields"].(map[string]any)["names"].([]any) {
			if nm == "lstat" {
				foundLocal = true
			}
		}
	}
	if !foundLocal {
		t.Fatal("no local vardecl for lstat: the alias machinery consumed " +
			"the function-local declaration")
	}
}

// TestAtomicPointerOpsRenamedImport pins that the P1-F2 lowering also
// applies under a renamed sync/atomic import (resolved-object
// discrimination, R2-P1-1).
func TestAtomicPointerOpsRenamedImport(t *testing.T) {
	doc := exportFuncValueProg(t, `package main

import (
	atom "sync/atomic"
	"unsafe"
)

var p unsafe.Pointer

func main() {
	var x int
	atom.StorePointer(&p, unsafe.Pointer(&x))
	q := atom.LoadPointer(&p)
	_ = q
}
`)
	body := funcBodyByName(t, doc, "main")
	if loads := syncopNodesByOp(body, "Load", "atomic.UnsafePointer"); len(loads) != 1 {
		t.Fatalf("renamed import: want 1 Load/atomic.UnsafePointer, got %d", len(loads))
	}
	if stores := syncopNodesByOp(body, "Store", "atomic.UnsafePointer"); len(stores) != 1 {
		t.Fatalf("renamed import: want 1 Store/atomic.UnsafePointer, got %d", len(stores))
	}
	for _, n := range findAllKind(body, "deref") {
		t.Fatalf("renamed import: plain deref node remains: %v", n["fields"])
	}
}
