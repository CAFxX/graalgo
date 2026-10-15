package main

// PKG-034 regression: a bare `return` in a pair-transformed user
// multi-result function must pack the named results into the pair.
// Before the fix the exporter emitted a bare return (empty results list),
// the backend returned the zeroed pair slot, and every named result read
// back as its zero value — the real encoding/ascii85 and encoding/base32
// streaming codecs produced empty output (Write returned 0, Decode
// returned nsrc 0).
//
// Red-green: pre-fix, the "return" wire nodes below carry an empty
// "results" list; post-fix they carry a single keyed pair complit built
// from the named-result idents.

import (
	"strings"
	"testing"
)

const mrBareReturnSrc = `package main

type buf struct{ n int }

func namedBare(p []byte) (n int, err error) {
	n = len(p)
	return
}

func (b *buf) methodBare(p []byte) (n int, err error) {
	n = len(p) + b.n
	return
}

var litBare = func(p []byte) (n int, err error) {
	n = len(p)
	return
}

func outerWithNestedLit(p []byte) (n int, err error) {
	f := func() (x int) {
		x = 3
		return
	}
	n = f() + len(p)
	return
}

func main() {
	_, _ = namedBare(nil)
	b := &buf{n: 1}
	_, _ = b.methodBare(nil)
	_, _ = litBare(nil)
	_, _ = outerWithNestedLit(nil)
}
`

// mrFuncDecl finds the wire funcdecl with the given name.
func mrFuncDecl(t *testing.T, doc map[string]any, name string) map[string]any {
	t.Helper()
	for _, f := range doc["funcs"].([]any) {
		fm, _ := f.(map[string]any)
		fl, _ := fm["fields"].(map[string]any)
		if fl["name"] == name {
			return fm
		}
	}
	t.Fatalf("no funcdecl %q in export", name)
	return nil
}

// mrBlockStmts returns the stmts of a block node.
func mrBlockStmts(t *testing.T, blk map[string]any) []any {
	t.Helper()
	if blk["kind"] != "block" {
		t.Fatalf("node kind %q, want block", blk["kind"])
	}
	stmts, _ := blk["fields"].(map[string]any)["stmts"].([]any)
	return stmts
}

// mrOwnReturns collects the "return" nodes belonging to the given
// funcdecl/funclit node: it walks the node but does not descend into
// nested funclit children (their returns belong to the literal).
func mrOwnReturns(root map[string]any) []map[string]any {
	var out []map[string]any
	var rec func(x any, isRoot bool)
	rec = func(x any, isRoot bool) {
		switch v := x.(type) {
		case map[string]any:
			kind, _ := v["kind"].(string)
			if kind == "funclit" && !isRoot {
				return
			}
			if kind == "return" {
				out = append(out, v)
				return
			}
			for _, c := range v {
				rec(c, false)
			}
		case []any:
			for _, c := range v {
				rec(c, false)
			}
		}
	}
	rec(root, true)
	return out
}

// mrReturnResults returns the results list of a return node.
func mrReturnResults(t *testing.T, r map[string]any) []any {
	t.Helper()
	res, _ := r["fields"].(map[string]any)["results"].([]any)
	return res
}

// mrPackedReturn asserts the return packs the named results into a keyed
// pair complit: fieldkeys == wantKeys and values are idents with wantNames.
func mrPackedReturn(t *testing.T, r map[string]any, wantKeys, wantNames []string) {
	t.Helper()
	res := mrReturnResults(t, r)
	if len(res) != 1 {
		t.Fatalf("bare return has %d results, want 1 packed pair", len(res))
	}
	pair, _ := res[0].(map[string]any)
	if pair["kind"] != "complit" {
		t.Fatalf("return result kind %q, want complit", pair["kind"])
	}
	pf := pair["fields"].(map[string]any)
	if keyed, _ := pf["keyed"].(bool); !keyed {
		t.Fatalf("pair complit is not keyed")
	}
	var keys []string
	for _, k := range pf["fieldkeys"].([]any) {
		keys = append(keys, k.(string))
	}
	if strings.Join(keys, ",") != strings.Join(wantKeys, ",") {
		t.Fatalf("pair fieldkeys %v, want %v", keys, wantKeys)
	}
	vals, _ := pf["values"].([]any)
	if len(vals) != len(wantNames) {
		t.Fatalf("pair has %d values, want %d", len(vals), len(wantNames))
	}
	for i, v := range vals {
		vm, _ := v.(map[string]any)
		if vm["kind"] != "ident" {
			t.Fatalf("pair value %d kind %q, want ident", i, vm["kind"])
		}
		name, _ := vm["fields"].(map[string]any)["name"].(string)
		if name != wantNames[i] {
			t.Fatalf("pair value %d ident %q, want %q", i, name, wantNames[i])
		}
	}
}

// mrNamedDecls asserts the body block opens with vardecls for wantNames.
func mrNamedDecls(t *testing.T, body map[string]any, wantNames []string) {
	t.Helper()
	stmts := mrBlockStmts(t, body)
	if len(stmts) < len(wantNames) {
		t.Fatalf("body has %d stmts, want at least %d named decls", len(stmts), len(wantNames))
	}
	for i, want := range wantNames {
		sm, _ := stmts[i].(map[string]any)
		if sm["kind"] != "vardecl" {
			t.Fatalf("body stmt %d kind %q, want vardecl (named result %q)", i, sm["kind"], want)
		}
		names, _ := sm["fields"].(map[string]any)["names"].([]any)
		if len(names) != 1 || names[0].(string) != want {
			t.Fatalf("body stmt %d vardecl names %v, want [%q]", i, names, want)
		}
	}
}

func TestMRBareReturnPacksNamedResults(t *testing.T) {
	doc := decodeExport(t, mustExport(t, mrBareReturnSrc))

	// Named function: bare return packs (n, err); locals declared.
	fd := mrFuncDecl(t, doc, "namedBare")
	body, _ := fd["fields"].(map[string]any)["body"].(map[string]any)
	mrNamedDecls(t, body, []string{"n", "err"})
	rets := mrOwnReturns(fd)
	if len(rets) != 1 {
		t.Fatalf("namedBare has %d returns, want 1", len(rets))
	}
	mrPackedReturn(t, rets[0], []string{"f0", "f1"}, []string{"n", "err"})

	// Method: same treatment (receiver + named results + bare return).
	md := mrFuncDecl(t, doc, "methodBare")
	mbody, _ := md["fields"].(map[string]any)["body"].(map[string]any)
	mrNamedDecls(t, mbody, []string{"n", "err"})
	mrets := mrOwnReturns(md)
	if len(mrets) != 1 {
		t.Fatalf("methodBare has %d returns, want 1", len(mrets))
	}
	mrPackedReturn(t, mrets[0], []string{"f0", "f1"}, []string{"n", "err"})
}

// TestMRBareReturnFuncLit covers the MR-closure-var path: a multi-result
// function literal's bare return packs its named results, and a bare
// return inside a nested single-result literal does NOT inherit the outer
// pair transform.
func TestMRBareReturnFuncLit(t *testing.T) {
	doc := decodeExport(t, mustExport(t, mrBareReturnSrc))

	// Package-level literal: named results declared, bare return packed.
	// (The nested single-result literal's return is bare, so the packed
	// one is litBare's.)
	var litNode map[string]any
	for _, fl := range findAllKind(doc, "funclit") {
		if rets := mrOwnReturns(fl); len(rets) == 1 {
			if res := mrReturnResults(t, rets[0]); len(res) == 1 {
				litNode = fl
			}
		}
	}
	if litNode == nil {
		t.Fatal("no packed-return funclit found (litBare)")
	}
	lbody, _ := litNode["fields"].(map[string]any)["body"].(map[string]any)
	mrNamedDecls(t, lbody, []string{"n", "err"})
	lrets := mrOwnReturns(litNode)
	mrPackedReturn(t, lrets[0], []string{"f0", "f1"}, []string{"n", "err"})

	// Nested single-result literal inside an MR function: its bare return
	// must stay bare (outer transform must not leak in).
	od := mrFuncDecl(t, doc, "outerWithNestedLit")
	var nested map[string]any
	var findNested func(n any)
	findNested = func(n any) {
		switch v := n.(type) {
		case map[string]any:
			if v["kind"] == "funclit" && nested == nil {
				nested = v
				return
			}
			for _, c := range v {
				findNested(c)
			}
		case []any:
			for _, c := range v {
				findNested(c)
			}
		}
	}
	findNested(od)
	if nested == nil {
		t.Fatal("no nested funclit found in outerWithNestedLit")
	}
	nrets := mrOwnReturns(nested)
	if len(nrets) != 1 {
		t.Fatalf("nested literal has %d returns, want 1", len(nrets))
	}
	if res := mrReturnResults(t, nrets[0]); len(res) != 0 {
		t.Fatalf("nested literal return has %d results, want 0 (bare, no outer pack)", len(res))
	}
	// The outer function's own bare return still packs.
	orets := mrOwnReturns(od)
	if len(orets) != 1 {
		t.Fatalf("outerWithNestedLit has %d own returns, want 1", len(orets))
	}
	mrPackedReturn(t, orets[0], []string{"f0", "f1"}, []string{"n", "err"})
}

// mustExport exports src or fails the test.
func mustExport(t *testing.T, src string) string {
	t.Helper()
	out, err := ExportFile(writeIfaceProg(t, src))
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	return out
}
