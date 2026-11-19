package main

// F2 (REVIEW-2026-11-19-big-scan): deferred writes to named results of
// pair-transformed multi-result functions were silently lost — the pair
// was packed at the `return` statement, before deferred functions ran,
// and the backend reads the pair slot after defers. fmt.Sscanf dropped
// every Scanner error because doScanf's `defer errorHandler(&err)`
// writes the named `err` result from a deferred function.
//
// The fix has two halves, both pinned here at the export shape (export
// success alone is not discriminating: the pre-fix tree also exported
// these functions fine and lost the values only at runtime):
//   1. In a deferring pair function, explicit returns assign to the
//      named-result locals first (via __mrret_ temps, so each value
//      expression is evaluated exactly once, in order), and a synthetic
//      deferred repack closure — registered first, so LIFO runs it last —
//      re-packs the locals into the pair after the user's defers run.
//   2. A function with no own-level defer, or with a single result, is
//      unchanged: no repack, no temps.

import (
	"encoding/json"
	"strings"
	"testing"
)

// f2FuncDeclBody returns the body stmt list of the wire funcdecl named
// name, or fails the test.
func f2FuncDeclBody(t *testing.T, doc string, name string) []any {
	t.Helper()
	raw := map[string]any{}
	if err := json.Unmarshal([]byte(doc), &raw); err != nil {
		t.Fatalf("unmarshal doc: %v", err)
	}
	for _, f := range findAllKind(raw, "funcdecl") {
		fl, _ := f["fields"].(map[string]any)
		if fl["name"] != name {
			continue
		}
		body, _ := fl["body"].(map[string]any)
		bf, _ := body["fields"].(map[string]any)
		stmts, _ := bf["stmts"].([]any)
		return stmts
	}
	t.Fatalf("no funcdecl %s in export", name)
	return nil
}

// f2RepackPresent reports whether stmts contain the synthetic repack
// defer: a defer whose funclit body assigns to the pair result — the
// assign's lhs ident has the pair var's empty name, which no user code
// can produce.
func f2RepackPresent(t *testing.T, stmts []any) bool {
	t.Helper()
	for _, s := range stmts {
		m, _ := s.(map[string]any)
		if m == nil || m["kind"] != "defer" {
			continue
		}
		if f2IsRepackDefer(m) {
			return true
		}
	}
	return false
}

func f2IsRepackDefer(deferNode map[string]any) bool {
	f, _ := deferNode["fields"].(map[string]any)
	call, _ := f["call"].(map[string]any)
	if call == nil || call["kind"] != "call" {
		return false
	}
	cf, _ := call["fields"].(map[string]any)
	fun, _ := cf["fun"].(map[string]any)
	if fun == nil || fun["kind"] != "funclit" {
		return false
	}
	ff, _ := fun["fields"].(map[string]any)
	body, _ := ff["body"].(map[string]any)
	bf, _ := body["fields"].(map[string]any)
	bstmts, _ := bf["stmts"].([]any)
	for _, bs := range bstmts {
		bm, _ := bs.(map[string]any)
		if bm == nil || bm["kind"] != "assign" {
			continue
		}
		af, _ := bm["fields"].(map[string]any)
		lhs, _ := af["lhs"].([]any)
		if len(lhs) == 0 {
			continue
		}
		lm, _ := lhs[0].(map[string]any)
		if lm == nil || lm["kind"] != "ident" {
			continue
		}
		lf, _ := lm["fields"].(map[string]any)
		if lf["name"] == "" {
			return true
		}
	}
	return false
}

// f2WalkTopLevel walks n without descending into funclit/defer subtrees
// (user deferred closures live there; the step-1 temps and assigns live
// at the function's top level, in the block wrapping the return).
func f2WalkTopLevel(n any, visit func(m map[string]any)) {
	switch v := n.(type) {
	case map[string]any:
		if v["kind"] == "funclit" || v["kind"] == "defer" {
			return
		}
		visit(v)
		for _, c := range v {
			f2WalkTopLevel(c, visit)
		}
	case []any:
		for _, c := range v {
			f2WalkTopLevel(c, visit)
		}
	}
}

// f2HasMRRetTemp reports whether the funcdecl (outside deferred closures)
// contains a __mrret_ temp vardecl — the F2 step-1 hoist.
func f2HasMRRetTemp(t *testing.T, stmts []any) bool {
	t.Helper()
	found := false
	f2WalkTopLevel(stmts, func(m map[string]any) {
		if m["kind"] != "vardecl" {
			return
		}
		f, _ := m["fields"].(map[string]any)
		names, _ := f["names"].([]any)
		for _, nm := range names {
			if ns, ok := nm.(string); ok && strings.HasPrefix(ns, "__mrret_") {
				found = true
			}
		}
	})
	return found
}

// f2HasTopLevelAssignTo reports whether the funcdecl (outside deferred
// closures) assigns to the named result ident name.
func f2HasTopLevelAssignTo(t *testing.T, stmts []any, name string) bool {
	t.Helper()
	found := false
	f2WalkTopLevel(stmts, func(m map[string]any) {
		if m["kind"] != "assign" {
			return
		}
		f, _ := m["fields"].(map[string]any)
		lhs, _ := f["lhs"].([]any)
		for _, l := range lhs {
			lm, _ := l.(map[string]any)
			if lm == nil || lm["kind"] != "ident" {
				continue
			}
			lf, _ := lm["fields"].(map[string]any)
			if lf["name"] == name {
				found = true
			}
		}
	})
	return found
}

const f2DeferExplicitSrc = `package main

import "errors"

func f(n int) (m int, err error) {
	defer func() { _ = errors.New("defer") }()
	return n + 1, nil
}

func main() {
	_, _ = f(1)
}
`

// TestF2RepackExplicitReturn: a deferring pair function with an explicit
// return must carry the repack defer (first defer in the body, after the
// named-result vardecls) and the step-1 temps + assigns to the locals.
// Fails on the pre-fix tree: no repack defer, no __mrret_ temps, and the
// return packs the pair directly from the value expressions.
func TestF2RepackExplicitReturn(t *testing.T) {
	doc := exportSrc(t, f2DeferExplicitSrc)
	stmts := f2FuncDeclBody(t, doc, "f")
	if len(stmts) < 3 {
		t.Fatalf("func f body has %d stmts, want >= 3 (vardecls + repack)", len(stmts))
	}
	if !f2RepackPresent(t, stmts) {
		t.Fatal("no repack defer in deferring pair function f")
	}
	// The repack is the first defer: stmts[0..1] are the named-result
	// vardecls, stmts[2] is the repack.
	if stmts[2].(map[string]any)["kind"] != "defer" || !f2IsRepackDefer(stmts[2].(map[string]any)) {
		t.Fatal("repack defer is not the third body statement (after the named-result vardecls)")
	}
	if !f2HasMRRetTemp(t, stmts) {
		t.Fatal("no __mrret_ temp in deferring pair function f (explicit return did not hoist values)")
	}
	if !f2HasTopLevelAssignTo(t, stmts, "err") {
		t.Fatal("no top-level assign to named result err in f (return values did not land in the locals)")
	}
}

const f2DeferBareSrc = `package main

import "errors"

func f() (n int, err error) {
	defer func() { err = errors.New("defer") }()
	n = 7
	return
}

func main() {
	_, _ = f()
}
`

// TestF2RepackBareReturn: a deferring pair function with a bare return
// must carry the repack defer (the bare return packs the locals, but a
// later defer may still rewrite them). No __mrret_ temps are needed:
// a bare return has no value expressions to hoist.
func TestF2RepackBareReturn(t *testing.T) {
	doc := exportSrc(t, f2DeferBareSrc)
	stmts := f2FuncDeclBody(t, doc, "f")
	if !f2RepackPresent(t, stmts) {
		t.Fatal("no repack defer in deferring pair function f (bare return)")
	}
	if f2HasMRRetTemp(t, stmts) {
		t.Fatal("unexpected __mrret_ temp in bare-return function f")
	}
}

const f2NoDeferSrc = `package main

func f(n int) (m int, err error) {
	return n + 1, nil
}

func main() {
	_, _ = f(1)
}
`

// TestF2NoRepackWithoutDefer: a pair function with no own-level defer is
// unchanged — no repack, no temps.
func TestF2NoRepackWithoutDefer(t *testing.T) {
	doc := exportSrc(t, f2NoDeferSrc)
	stmts := f2FuncDeclBody(t, doc, "f")
	if f2RepackPresent(t, stmts) {
		t.Fatal("unexpected repack defer in pair function f with no defer")
	}
	if f2HasMRRetTemp(t, stmts) {
		t.Fatal("unexpected __mrret_ temp in pair function f with no defer")
	}
}

const f2SingleResultSrc = `package main

import "errors"

func f() (err error) {
	defer func() { err = errors.New("defer") }()
	return nil
}

func main() {
	_ = f()
}
`

// TestF2NoRepackSingleResult: a single-result function is never
// pair-transformed, so no repack exists even with a defer — its result
// slot IS the named-result slot, and deferred writes already land.
func TestF2NoRepackSingleResult(t *testing.T) {
	doc := exportSrc(t, f2SingleResultSrc)
	stmts := f2FuncDeclBody(t, doc, "f")
	if f2RepackPresent(t, stmts) {
		t.Fatal("unexpected repack defer in single-result function f")
	}
	if f2HasMRRetTemp(t, stmts) {
		t.Fatal("unexpected __mrret_ temp in single-result function f")
	}
}

const f2NestedDeferSrc = `package main

import "errors"

func f() (n int, err error) {
	if true {
		defer func() { err = errors.New("defer") }()
	}
	return 14, nil
}

func g() (n int, err error) {
	h := func() {
		defer func() { err = errors.New("inner") }()
	}
	h()
	n = 15
	return
}

func main() {
	_, _ = f()
	_, _ = g()
}
`

// TestF2NestedDeferPositions: a defer in a nested block still registers
// on the function's own chain (repack present), while a defer inside a
// nested func literal belongs to the literal (no repack for the outer
// function).
func TestF2NestedDeferPositions(t *testing.T) {
	doc := exportSrc(t, f2NestedDeferSrc)
	stmts := f2FuncDeclBody(t, doc, "f")
	if !f2RepackPresent(t, stmts) {
		t.Fatal("no repack defer in f (defer in nested if-block is own-level)")
	}
	stmts = f2FuncDeclBody(t, doc, "g")
	if f2RepackPresent(t, stmts) {
		t.Fatal("unexpected repack defer in g (its only defer is inside a nested func literal)")
	}
}

const f2MRCallReturnSrc = `package main

import "errors"

func inner() (int, error) { return 13, nil }

func f() (n int, err error) {
	defer func() { err = errors.New("defer") }()
	return inner()
}

func main() {
	_, _ = f()
}
`

// TestF2MRCallReturn: a multi-result call in return position in a
// deferring pair function must assign the unpacked fields to the named
// locals (so the repack reads them), not just pack the pair.
func TestF2MRCallReturn(t *testing.T) {
	doc := exportSrc(t, f2MRCallReturnSrc)
	stmts := f2FuncDeclBody(t, doc, "f")
	if !f2RepackPresent(t, stmts) {
		t.Fatal("no repack defer in f (MR-call return with defer)")
	}
	if !f2HasTopLevelAssignTo(t, stmts, "err") {
		t.Fatal("no top-level assign to named result err in f (MR-call return values did not land in the locals)")
	}
}
