package main

// P3-M10 (PKG-035): exporter tests for multi-result function values in
// value position.
//
// Red-green: before the fix, a multi-result named function used as a
// value is a scope error — for synthetic table-driven functions (e.g.
// syscall.Accept) "in value position is not supported in frontend/v1
// scope (M5): multi-result function values require v2"; for user
// functions the value forms but a call through the variable fails with
// "multi-valued call result is not supported in frontend/v1 scope
// (WS D)". After the fix, the value is the pair-returning implementation
// (the __-helper for table-driven functions, the transformed funcdecl
// for user functions) and call sites through a variable holding it
// desugar exactly like direct calls (userMRVars linking).
//
//  1. TestMRFuncValueSyntheticAccept pins the exact reported construct:
//     var AcceptFunc func(int) (int, syscall.Sockaddr, error) =
//     syscall.Accept — the initializer lowers to an ident referencing
//     the __accept helper's funcdecl.
//  2. TestMRFuncValueUserPackageVar pins a package-level
//     var F func() (int, error) = two plus a, err := F(): the call
//     desugars through a __p5mr_N pair temp.
//  3. TestMRFuncValueUserDefine pins the same for F := two in main.
//  4. TestMRFuncValueFailFastKept pins the boundary: an M5-kind
//     multi-result function with no drop-in __-helper value form
//     (time.ParseDuration) in value position keeps the honest M5
//     compile-time error, exactly as before.
//
// The load-bearing semantic — "the value is the pair-returning
// implementation" — is pinned by obj-id binding assertions (the
// initializer ident must resolve to the very object the transformed
// funcdecl declares, and that funcdecl's wire signature must be the
// single-pair-result form), plus assertions that the a/err extraction
// assignments select the pair fields in result order. A wrong binding
// (e.g. an untransformed duplicate funcdecl) or a scrambled extraction
// fails these tests.

import (
	"fmt"
	"strings"
	"testing"
)

// exportFuncValueProg exports src, failing the test on export error, and
// returns the decoded wire doc.
func exportFuncValueProg(t *testing.T, src string) map[string]any {
	t.Helper()
	out, err := ExportFile(writeIfaceProg(t, src))
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	return decodeExport(t, out)
}

// findGlobalInitByName returns the init node of the package-level global
// whose object-table name matches (synthetic package vars land in the
// doc's "globals" array, not as vardecl nodes).
func findGlobalInitByName(t *testing.T, doc map[string]any, name string) map[string]any {
	t.Helper()
	objects, _ := doc["objects"].([]any)
	globals, _ := doc["globals"].([]any)
	for _, g := range globals {
		gm, _ := g.(map[string]any)
		oidF, _ := gm["obj"].(float64)
		oid := int(oidF)
		if oid < 0 || oid >= len(objects) {
			continue
		}
		om, _ := objects[oid].(map[string]any)
		if om["name"] == name {
			init, _ := gm["init"].(map[string]any)
			if init == nil {
				t.Fatalf("global %q has no init", name)
			}
			return init
		}
	}
	t.Fatalf("no global named %q", name)
	return nil
}

// findLocalVarInit returns the init value node of the vardecl defining
// the named local variable.
func findLocalVarInit(t *testing.T, doc map[string]any, name string) map[string]any {
	t.Helper()
	for _, n := range findAllKind(doc, "vardecl") {
		names, _ := n["fields"].(map[string]any)["names"].([]any)
		for _, nm := range names {
			if s, _ := nm.(string); s != name {
				continue
			}
			vals, _ := n["fields"].(map[string]any)["values"].([]any)
			if len(vals) == 0 {
				t.Fatalf("vardecl %q has no init value", name)
			}
			init, _ := vals[0].(map[string]any)
			if init == nil {
				t.Fatalf("vardecl %q init is not a node", name)
			}
			return init
		}
	}
	t.Fatalf("no vardecl defining %q", name)
	return nil
}

// findPairTempCall locates the desugared __p5mr_N := <funcName>() pair-temp
// vardecl — the call through a func-valued variable — and returns the
// vardecl node, the temp name, and the temp's obj id.
func findPairTempCall(t *testing.T, doc map[string]any, funcName string) (node map[string]any, tmpName string, tmpID float64) {
	t.Helper()
	for _, n := range findAllKind(doc, "vardecl") {
		names, _ := n["fields"].(map[string]any)["names"].([]any)
		if len(names) != 1 {
			continue
		}
		name, _ := names[0].(string)
		if !strings.HasPrefix(name, "__p5mr_") {
			continue
		}
		vals, _ := n["fields"].(map[string]any)["values"].([]any)
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
		if fun["fields"].(map[string]any)["name"] != funcName {
			continue
		}
		objs, _ := n["fields"].(map[string]any)["objs"].([]any)
		if len(objs) == 1 {
			tmpID, _ = objs[0].(float64)
		}
		return n, name, tmpID
	}
	t.Fatalf("no desugared __p5mr_N := %s() pair-temp vardecl found", funcName)
	return nil, "", -1
}

// funcValueBinding asserts the load-bearing semantic of a multi-result
// function value: the initializer ident binds (via its obj id) to the
// funcdecl of the pair-returning implementation — the same object the
// funcdecl node declares. A wrong binding (e.g. the ident pointing at an
// untransformed duplicate or a different object) fails here. Returns the
// shared obj id and the funcdecl node for further checks.
func funcValueBinding(t *testing.T, doc map[string]any, init map[string]any, wantFuncName string) (float64, map[string]any) {
	t.Helper()
	if init["kind"] != "ident" {
		t.Fatalf("initializer kind = %q, want ident (function value)", init["kind"])
	}
	fields, _ := init["fields"].(map[string]any)
	if fields == nil {
		t.Fatal("initializer ident has no fields")
	}
	oid, ok := fields["obj"].(float64)
	if !ok {
		t.Fatalf("initializer ident %q has no obj id", fields["name"])
	}
	objects, _ := doc["objects"].([]any)
	if int(oid) < 0 || int(oid) >= len(objects) {
		t.Fatalf("initializer ident obj id %v out of range (%d objects)", oid, len(objects))
	}
	obj, _ := objects[int(oid)].(map[string]any)
	if obj["kind"] != "func" {
		t.Fatalf("initializer binds to object kind %q, want func", obj["kind"])
	}
	if obj["name"] != wantFuncName {
		t.Fatalf("initializer binds to func %q, want %q", obj["name"], wantFuncName)
	}
	// Same-object binding: exactly one funcdecl node must declare this
	// very object id.
	var fd map[string]any
	for _, f := range findAllKind(doc, "funcdecl") {
		ff, _ := f["fields"].(map[string]any)
		if ff == nil {
			continue
		}
		if foid, _ := ff["obj"].(float64); foid == oid {
			if fd != nil {
				t.Fatalf("two funcdecls declare obj id %v", oid)
			}
			fd = f
		}
	}
	if fd == nil {
		t.Fatalf("no funcdecl declares obj id %v (%q)", oid, wantFuncName)
	}
	if fd["fields"].(map[string]any)["name"] != wantFuncName {
		t.Fatalf("funcdecl with obj id %v is named %q, want %q",
			oid, fd["fields"].(map[string]any)["name"], wantFuncName)
	}
	return oid, fd
}

// assertPairReturningSig asserts the funcdecl's wire signature is the
// pair-returning form: exactly one result whose type resolves (through
// named entries) to a struct with the expected pair field names in
// result order. An untransformed funcdecl (N wire results) fails here.
func assertPairReturningSig(t *testing.T, doc map[string]any, fd map[string]any, wantFields []string) {
	t.Helper()
	fields, _ := fd["fields"].(map[string]any)
	sigID, ok := fields["sig"].(float64)
	if !ok {
		t.Fatal("funcdecl has no sig type id")
	}
	entries := typeEntries(t, doc)
	if int(sigID) < 0 || int(sigID) >= len(entries) {
		t.Fatalf("sig id %v out of range (%d type entries)", sigID, len(entries))
	}
	sentry, _ := entries[int(sigID)].(map[string]any)
	if sentry["kind"] != "signature" {
		t.Fatalf("sig entry kind = %v, want signature", sentry["kind"])
	}
	sdetail, _ := sentry["detail"].(map[string]any)
	results, _ := sdetail["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("funcdecl has %d wire results, want 1 (pair-returning form)", len(results))
	}
	rid, _ := results[0].(float64)
	for {
		if int(rid) < 0 || int(rid) >= len(entries) {
			t.Fatalf("result type id %v out of range", rid)
		}
		rent, _ := entries[int(rid)].(map[string]any)
		switch rent["kind"] {
		case "struct":
			rdetail, _ := rent["detail"].(map[string]any)
			sfields, _ := rdetail["fields"].([]any)
			if len(sfields) != len(wantFields) {
				t.Fatalf("pair struct has %d fields, want %d (%v)",
					len(sfields), len(wantFields), wantFields)
			}
			for i, f := range sfields {
				fm, _ := f.(map[string]any)
				if fm["name"] != wantFields[i] {
					t.Fatalf("pair struct field %d = %q, want %q (result order)",
						i, fm["name"], wantFields[i])
				}
			}
			return
		case "named":
			rdetail, _ := rent["detail"].(map[string]any)
			rid, _ = rdetail["underlying"].(float64)
		default:
			t.Fatalf("pair result type kind = %v, want struct (via named)", rent["kind"])
		}
	}
}

// assertPairExtraction asserts the pair temp's fields land in the named
// targets: one vardecl per target, each a selector on the temp ident for
// the expected pair field (f0, f1, ...) in result order. A scrambled or
// missing extraction fails here.
func assertPairExtraction(t *testing.T, doc map[string]any, tmpName string, tmpID float64, targets []string) {
	t.Helper()
	for i, name := range targets {
		var found map[string]any
		for _, n := range findAllKind(doc, "vardecl") {
			names, _ := n["fields"].(map[string]any)["names"].([]any)
			if len(names) != 1 {
				continue
			}
			if s, _ := names[0].(string); s != name {
				continue
			}
			found = n
			break
		}
		if found == nil {
			t.Fatalf("no vardecl assigning extraction target %q", name)
		}
		vals, _ := found["fields"].(map[string]any)["values"].([]any)
		if len(vals) != 1 {
			t.Fatalf("vardecl %q has %d values, want 1", name, len(vals))
		}
		sel, _ := vals[0].(map[string]any)
		if sel["kind"] != "selector" {
			t.Fatalf("vardecl %q value kind = %q, want selector (pair-field extraction)",
				name, sel["kind"])
		}
		sfields, _ := sel["fields"].(map[string]any)
		wantField := fmt.Sprintf("f%d", i)
		if sfields["field"] != wantField {
			t.Fatalf("vardecl %q extracts field %v, want %q (result order)",
				name, sfields["field"], wantField)
		}
		x, _ := sfields["x"].(map[string]any)
		if x["kind"] != "ident" {
			t.Fatalf("vardecl %q extraction selector x kind = %q, want ident", name, x["kind"])
		}
		xfields, _ := x["fields"].(map[string]any)
		if xfields["name"] != tmpName {
			t.Fatalf("vardecl %q extracts from %q, want the %s pair temp",
				name, xfields["name"], tmpName)
		}
		if xoid, _ := xfields["obj"].(float64); xoid != tmpID {
			t.Fatalf("vardecl %q extraction temp obj id = %v, want %v", name, xoid, tmpID)
		}
	}
}

func TestMRFuncValueSyntheticAccept(t *testing.T) {
	doc := exportFuncValueProg(t, `package main

import "syscall"

var AcceptFunc func(int) (int, syscall.Sockaddr, error) = syscall.Accept

func main() {}
`)
	// The var's initializer is an ident referencing the __accept
	// helper's funcdecl — the pair-returning implementation a direct
	// syscall.Accept(fd) call desugars to.
	init := findGlobalInitByName(t, doc, "AcceptFunc")
	_, fd := funcValueBinding(t, doc, init, "__accept")
	assertPairReturningSig(t, doc, fd, []string{"fd", "sa", "err"})
}

func TestMRFuncValueUserPackageVar(t *testing.T) {
	doc := exportFuncValueProg(t, `package main

func two() (int, error) { return 1, nil }

var F func() (int, error) = two

func main() {
	a, err := F()
	println(a, err == nil)
}
`)
	// The initializer binds to the pair-returning implementation of two
	// (the transformed funcdecl), not to anything else.
	init := findGlobalInitByName(t, doc, "F")
	_, fd := funcValueBinding(t, doc, init, "two")
	assertPairReturningSig(t, doc, fd, []string{"f0", "f1"})
	// The call through F desugars: a __p5mr_N temp holds the pair call,
	// then field extractions assign a and err.
	_, tmpName, tmpID := findPairTempCall(t, doc, "F")
	assertPairExtraction(t, doc, tmpName, tmpID, []string{"a", "err"})
}

func TestMRFuncValueUserDefine(t *testing.T) {
	doc := exportFuncValueProg(t, `package main

func two() (int, error) { return 2, nil }

func main() {
	F := two
	a, err := F()
	println(a, err == nil)
}
`)
	// Same binding contract as the package-level case: F's initializer
	// resolves to the transformed pair-returning funcdecl of two.
	init := findLocalVarInit(t, doc, "F")
	_, fd := funcValueBinding(t, doc, init, "two")
	assertPairReturningSig(t, doc, fd, []string{"f0", "f1"})
	// The call through F desugars through the same pair temp, with the
	// same a/err extraction assignments.
	_, tmpName, tmpID := findPairTempCall(t, doc, "F")
	assertPairExtraction(t, doc, tmpName, tmpID, []string{"a", "err"})
}

func TestMRFuncValueFailFastKept(t *testing.T) {
	// time.ParseDuration is an M5-kind multi-result function whose
	// __-helper is not a drop-in value form (and it is a fail-fast stub
	// in the value-position tables): value position keeps the honest
	// compile-time M5 error, exactly as before this change.
	_, err := ExportFile(writeIfaceProg(t, `package main

import "time"

var F func(string) (time.Duration, error) = time.ParseDuration

func main() {}
`))
	if err == nil {
		t.Fatal("expected M5 scope error for time.ParseDuration in value position, got nil")
	}
	if !strings.Contains(err.Error(), "multi-result function values require v2") {
		t.Fatalf("error = %q, want the M5 value-position scope error", err.Error())
	}
}
