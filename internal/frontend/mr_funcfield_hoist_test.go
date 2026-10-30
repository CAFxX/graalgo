package main

// Regression tests for the funcfieldcall wire-kind gap (review lane
// 2026-10-30): a multi-result call to a func-typed struct field used
// as a call argument (e.g. show(s.gen(21))) was exported as a bespoke
// "funcfieldcall" node by hoistMRCallArg, but no backend builder ever
// handled that kind, so AST building failed with 'unsupported
// expression kind "funcfieldcall"'. The assignment path had already
// been converted to a regular "call" node by fix-funcfield-mr, which
// missed this hoisting path. The fix converts the hoisting path to
// the same regular "call" node (selector fun, pair type), so these
// tests pin both halves: no "funcfieldcall" node anywhere in the doc,
// and a __p5mr_N pair-temp vardecl whose value is a "call" with a
// "selector" fun.
//
// Red-green: before the fix both tests fail on the funcfieldcall node
// (the hoisted call is emitted under that kind); after the fix the
// hoisted call is a regular call node and the tests pass. The tests
// are exporter-only (no backend run): the backend's selector-fun call
// path is already covered by the assignment-path differential runs.

import (
	"strings"
	"testing"
)

// hoistedFuncFieldCall finds the __p5mr_N pair-temp vardecl whose value
// is the hoisted multi-result func-field call, and returns its call
// node (or fails the test).
func hoistedFuncFieldCall(t *testing.T, doc map[string]any) map[string]any {
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
		if call == nil {
			continue
		}
		fun, _ := call["fields"].(map[string]any)["fun"].(map[string]any)
		if fun != nil && fun["kind"] == "selector" {
			return call
		}
	}
	t.Fatal("no __p5mr_N pair-temp vardecl with a selector-fun call value found")
	return nil
}

func TestMRFuncFieldCallHoistedArg(t *testing.T) {
	doc := exportFuncValueProg(t, `package main

type S struct {
	gen func(int) (int, error)
}

func show(a int, err error) {}

func main() {
	s := S{gen: func(x int) (int, error) { return x * 2, nil }}
	show(s.gen(21))
}
`)
	if bad := findAllKind(doc, "funcfieldcall"); len(bad) != 0 {
		t.Fatalf("found %d funcfieldcall node(s), want 0 (dead wire kind with no backend handler)", len(bad))
	}
	call := hoistedFuncFieldCall(t, doc)
	if call["kind"] != "call" {
		t.Fatalf("hoisted call kind = %q, want call", call["kind"])
	}
}

func TestMRFuncFieldCallHoistedArgPromoted(t *testing.T) {
	// Same gap through a promoted field on a pointer receiver: the
	// selector must carry the full selpath, which only the regular
	// "call" + selector-fun path expresses (the old funcfieldcall
	// wire carried a bare field name and no selpath at all).
	doc := exportFuncValueProg(t, `package main

type S struct {
	gen func(int) (int, string, error)
}

type P struct {
	S
}

func show3(a int, s string, err error) {}

func main() {
	p := &P{S: S{gen: func(x int) (int, string, error) { return x * 3, "ok", nil }}}
	show3(p.gen(7))
}
`)
	if bad := findAllKind(doc, "funcfieldcall"); len(bad) != 0 {
		t.Fatalf("found %d funcfieldcall node(s), want 0 (dead wire kind with no backend handler)", len(bad))
	}
	call := hoistedFuncFieldCall(t, doc)
	if call["kind"] != "call" {
		t.Fatalf("hoisted call kind = %q, want call", call["kind"])
	}
	fun, _ := call["fields"].(map[string]any)["fun"].(map[string]any)
	selpath, _ := fun["fields"].(map[string]any)["selpath"].([]any)
	if len(selpath) != 2 {
		t.Fatalf("promoted selector selpath = %v, want 2 elements (embedded S, then gen)", selpath)
	}
}
