// Golden tests for emitter metadata honesty (review 2026-10-30-21, v2 emitter).
//
// Each test pins a silent-metadata-loss bug: the emitter used to drop
// information while still returning success, so a consumer could not
// tell the dropped case apart from a genuinely simpler program. Every
// test is demonstrably capable of failing: reverting the corresponding
// fix reintroduces the identical/empty/nil emission it asserts against.
//
// The tests are structural, never name-keyed to a fixture: they assert
// on the emitted shapes (receiver presence, parameter counts, selection
// indices, method lists), not on the particular identifiers used.
package emit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/parser"
)

// emitSourceDoc runs the full pipeline (parse → check → emit) on one
// source file and returns the emitted document.
func emitSourceDoc(t *testing.T, src string) *Document {
	t.Helper()
	dir := t.TempDir()
	fn := filepath.Join(dir, "src.go")
	if err := os.WriteFile(fn, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	pres, err := parser.ParseFiles([]string{fn})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cres, err := checker.Check(pres.Fset, pres.Pkg, &checker.Config{GoVersion: "go1.27"})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	doc, err := Emit(cres, pres.Pkg, pres.Files)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	return doc
}

// funcDeclFields returns the fields maps of all emitted funcdecls with
// the given name, in emission order.
func funcDeclFields(doc *Document, name string) []map[string]any {
	var out []map[string]any
	for _, f := range doc.Funcs {
		m, ok := f.(map[string]any)
		if !ok {
			continue
		}
		fields, _ := m["fields"].(map[string]any)
		if fields == nil {
			continue
		}
		if n, _ := fields["name"].(string); n == name {
			out = append(out, fields)
		}
	}
	return out
}

// typeDetail returns the detail map of the type-table entry with the
// given id.
func typeDetail(doc *Document, id any) map[string]any {
	i, ok := id.(int)
	if !ok || i < 0 || i >= len(doc.Types) {
		return nil
	}
	m, _ := doc.Types[i].(map[string]any)
	if m == nil {
		return nil
	}
	d, _ := m["detail"].(map[string]any)
	return d
}

// typeKind returns the kind string of the type-table entry with the
// given id.
func typeKind(doc *Document, id any) string {
	i, ok := id.(int)
	if !ok || i < 0 || i >= len(doc.Types) {
		return ""
	}
	m, _ := doc.Types[i].(map[string]any)
	if m == nil {
		return ""
	}
	k, _ := m["kind"].(string)
	return k
}

// collectNodes walks an emitted value and returns the fields maps of
// every node with the given kind.
func collectNodes(v any, kind string) []map[string]any {
	var out []map[string]any
	switch n := v.(type) {
	case map[string]any:
		if k, _ := n["kind"].(string); k == kind {
			if fields, ok := n["fields"].(map[string]any); ok {
				out = append(out, fields)
			}
		}
		for _, val := range n {
			out = append(out, collectNodes(val, kind)...)
		}
	case []any:
		for _, val := range n {
			out = append(out, collectNodes(val, kind)...)
		}
	}
	return out
}

// namedTypeDetail finds the detail map of the named type whose name is
// exactly name or ends in "."+name (pkgpath-qualified).
func namedTypeDetail(doc *Document, name string) map[string]any {
	for _, tv := range doc.Types {
		m, _ := tv.(map[string]any)
		if m == nil || m["kind"] != "named" {
			continue
		}
		d, _ := m["detail"].(map[string]any)
		if d == nil {
			continue
		}
		if n, _ := d["name"].(string); n == name || strings.HasSuffix(n, "."+name) {
			return d
		}
	}
	return nil
}

func TestEmitMethodReceiver(t *testing.T) {
	doc := emitSourceDoc(t, `package demo

type A struct{ X int }
type B struct{ Y int }

func M() int { return 0 }
func (a A) M() int { return a.X }
func (b B) M() int { return b.Y }
`)
	decls := funcDeclFields(doc, "M")
	if len(decls) != 3 {
		t.Fatalf("got %d funcdecls named M, want 3", len(decls))
	}
	// The plain function and the two methods, in source order.
	plain, aMeth, bMeth := decls[0], decls[1], decls[2]
	if plain["recv"] != nil {
		t.Errorf("plain func M: recv = %v, want nil", plain["recv"])
	}
	recvA, _ := aMeth["recv"].(map[string]any)
	recvB, _ := bMeth["recv"].(map[string]any)
	if recvA == nil || recvB == nil {
		t.Fatalf("method recvs: A=%v B=%v, want both present", aMeth["recv"], bMeth["recv"])
	}
	if n, _ := recvA["name"].(string); n != "a" {
		t.Errorf("A.M recv name = %q, want %q", n, "a")
	}
	if n, _ := recvB["name"].(string); n != "b" {
		t.Errorf("B.M recv name = %q, want %q", n, "b")
	}
	if recvA["type"] == recvB["type"] {
		t.Errorf("A.M and B.M recv types are both %v: same-named methods on different types emit identically", recvA["type"])
	}
	for _, tc := range []struct {
		recv map[string]any
		want string
	}{
		{recvA, "A"},
		{recvB, "B"},
	} {
		d := typeDetail(doc, tc.recv["type"])
		if d == nil || typeKind(doc, tc.recv["type"]) != "named" {
			t.Errorf("recv type %v is not a named type", tc.recv["type"])
			continue
		}
		if n, _ := d["name"].(string); n != tc.want && !strings.HasSuffix(n, "."+tc.want) {
			t.Errorf("recv type name = %q, want %q", n, tc.want)
		}
	}
	// The signature encoding must carry Recv too, and the plain
	// function's signature must not.
	for _, tc := range []struct {
		decl   map[string]any
		isMeth bool
	}{
		{plain, false},
		{aMeth, true},
		{bMeth, true},
	} {
		d := typeDetail(doc, tc.decl["sig"])
		if d == nil {
			t.Errorf("sig %v has no type entry", tc.decl["sig"])
			continue
		}
		if tc.isMeth && d["recv"] == nil {
			t.Errorf("method signature detail has no recv")
		}
		if !tc.isMeth && d["recv"] != nil {
			t.Errorf("plain function signature detail has recv %v, want nil", d["recv"])
		}
	}
	// Regression core: the two method declarations must not be identical.
	ab, _ := json.Marshal(aMeth)
	bb, _ := json.Marshal(bMeth)
	if string(ab) == string(bb) {
		t.Errorf("A.M and B.M funcdecls marshal identically: %s", ab)
	}
	// Func object entries: exactly the two methods carry recv.
	var funcObjs []map[string]any
	for _, o := range doc.Objects {
		m, _ := o.(map[string]any)
		if m != nil && m["kind"] == "func" && m["name"] == "M" {
			funcObjs = append(funcObjs, m)
		}
	}
	if len(funcObjs) != 3 {
		t.Fatalf("got %d func objects named M, want 3", len(funcObjs))
	}
	withRecv := 0
	for _, o := range funcObjs {
		if o["recv"] != nil {
			withRecv++
		}
	}
	if withRecv != 2 {
		t.Errorf("%d of 3 func objects named M carry recv, want 2", withRecv)
	}
}

func TestEmitUnnamedParams(t *testing.T) {
	doc := emitSourceDoc(t, `package demo

func F(int, string) int { return 0 }
`)
	decls := funcDeclFields(doc, "F")
	if len(decls) != 1 {
		t.Fatalf("got %d funcdecls named F, want 1", len(decls))
	}
	params, _ := decls[0]["params"].([]any)
	if len(params) != 2 {
		t.Fatalf("funcdecl params = %d entries, want 2 (unnamed params must not be dropped)", len(params))
	}
	for i, p := range params {
		m, _ := p.(map[string]any)
		if m == nil {
			t.Fatalf("param %d is not a node", i)
		}
		fields, _ := m["fields"].(map[string]any)
		if fields == nil {
			t.Fatalf("param %d has no fields", i)
		}
		if n, _ := fields["name"].(string); n != "" {
			t.Errorf("param %d name = %q, want empty (unnamed)", i, n)
		}
	}
	// The two parameters must have distinct types, int then string.
	typeName := func(p any) string {
		m, _ := p.(map[string]any)
		fields, _ := m["fields"].(map[string]any)
		d := typeDetail(doc, fields["type"])
		if d == nil {
			return ""
		}
		n, _ := d["name"].(string)
		return n
	}
	if got := typeName(params[0]); got != "int" {
		t.Errorf("param 0 type = %q, want int", got)
	}
	if got := typeName(params[1]); got != "string" {
		t.Errorf("param 1 type = %q, want string", got)
	}
	// Signature encoding agrees.
	d := typeDetail(doc, decls[0]["sig"])
	sigParams, _ := d["params"].([]any)
	if len(sigParams) != 2 {
		t.Fatalf("signature params = %d entries, want 2", len(sigParams))
	}
}

func TestEmitRangeTargets(t *testing.T) {
	doc := emitSourceDoc(t, `package demo

type S struct{ F int }

func Assign(xs []int, s S) int {
	total := 0
	for s.F = range xs {
		total = total + s.F
	}
	return total
}

func Plain(xs []int) int {
	total := 0
	for k, v := range xs {
		total = total + k + v
	}
	return total
}

func Reuse(xs []int, k int) int {
	total := 0
	for k = range xs {
		total = total + k
	}
	return total
}
`)
	// Non-identifier target: emitted as a general expression, not nil.
	assign := funcDeclFields(doc, "Assign")
	if len(assign) != 1 {
		t.Fatalf("got %d funcdecls named Assign, want 1", len(assign))
	}
	ranges := collectNodes(assign[0]["body"], "range")
	if len(ranges) != 1 {
		t.Fatalf("got %d range nodes in Assign, want 1", len(ranges))
	}
	key, ok := ranges[0]["key"].(map[string]any)
	if !ok {
		t.Fatalf("Assign range key = %v (%T), want a selector node, not nil", ranges[0]["key"], ranges[0]["key"])
	}
	if k, _ := key["kind"].(string); k != "selector" {
		t.Errorf("Assign range key kind = %q, want selector", k)
	}
	if fields, _ := key["fields"].(map[string]any); fields != nil {
		if s, _ := fields["sel"].(string); s != "F" {
			t.Errorf("Assign range key sel = %q, want F", s)
		}
	}
	// Identifier targets keep the historical object-id shape.
	plain := funcDeclFields(doc, "Plain")
	ranges = collectNodes(plain[0]["body"], "range")
	if len(ranges) != 1 {
		t.Fatalf("got %d range nodes in Plain, want 1", len(ranges))
	}
	if _, ok := ranges[0]["key"].(int); !ok {
		t.Errorf("Plain range key = %v (%T), want an object id", ranges[0]["key"], ranges[0]["key"])
	}
	if _, ok := ranges[0]["value"].(int); !ok {
		t.Errorf("Plain range value = %v (%T), want an object id", ranges[0]["value"], ranges[0]["value"])
	}
	// Pre-existing variable assigned with = (Uses, not Defs) also works.
	reuse := funcDeclFields(doc, "Reuse")
	ranges = collectNodes(reuse[0]["body"], "range")
	if len(ranges) != 1 {
		t.Fatalf("got %d range nodes in Reuse, want 1", len(ranges))
	}
	if _, ok := ranges[0]["key"].(int); !ok {
		t.Errorf("Reuse range key = %v (%T), want an object id", ranges[0]["key"], ranges[0]["key"])
	}
}

func TestEmitPromotedSelector(t *testing.T) {
	doc := emitSourceDoc(t, `package demo

type Inner struct{ X int }

func (in Inner) M() int { return in.X }

type Outer struct{ Inner }

func Use(o Outer) int { return o.M() + o.X }
`)
	use := funcDeclFields(doc, "Use")
	if len(use) != 1 {
		t.Fatalf("got %d funcdecls named Use, want 1", len(use))
	}
	sels := collectNodes(use[0]["body"], "selector")
	byName := map[string]map[string]any{}
	for _, s := range sels {
		if n, _ := s["sel"].(string); n != "" {
			byName[n] = s
		}
	}
	meth, ok := byName["M"]
	if !ok {
		t.Fatalf("no selector for M in Use; selectors found: %v", byName)
	}
	if k, _ := meth["sel_kind"].(string); k != "method" {
		t.Errorf("o.M sel_kind = %q, want method", k)
	}
	idx, _ := meth["sel_index"].([]any)
	if len(idx) != 2 {
		t.Errorf("o.M sel_index = %v, want 2 entries (promoted through Outer.Inner)", idx)
	}
	if _, ok := meth["sel_obj"].(int); !ok {
		t.Errorf("o.M sel_obj = %v (%T), want an object id", meth["sel_obj"], meth["sel_obj"])
	}
	if _, ok := meth["sel_recv"].(int); !ok {
		t.Errorf("o.M sel_recv = %v (%T), want a type id", meth["sel_recv"], meth["sel_recv"])
	}
	field, ok := byName["X"]
	if !ok {
		t.Fatalf("no selector for X in Use")
	}
	if k, _ := field["sel_kind"].(string); k != "field" {
		t.Errorf("o.X sel_kind = %q, want field", k)
	}
	fidx, _ := field["sel_index"].([]any)
	if len(fidx) != 2 {
		t.Errorf("o.X sel_index = %v, want 2 entries (promoted field)", fidx)
	}
}

func TestEmitNamedAndInterfaceMethods(t *testing.T) {
	doc := emitSourceDoc(t, `package demo

type T struct{ X int }

func (t T) M() int { return t.X }
func (t *T) N() int { return t.X }

type I interface{ M() int }

func UseI(i I) int { return i.M() }
`)
	d := namedTypeDetail(doc, "T")
	if d == nil {
		t.Fatalf("no named type T in type table")
	}
	methods, _ := d["methods"].([]any)
	names := map[string]bool{}
	for _, m := range methods {
		mm, _ := m.(map[string]any)
		if mm != nil {
			if n, _ := mm["name"].(string); n != "" {
				names[n] = true
			}
		}
	}
	if len(methods) != 2 || !names["M"] || !names["N"] {
		t.Errorf("named type T methods = %v, want [M N]", names)
	}
	// The interface's method list must not be empty either.
	var ifaceMethods []any
	for _, tv := range doc.Types {
		m, _ := tv.(map[string]any)
		if m == nil || m["kind"] != "interface" {
			continue
		}
		dd, _ := m["detail"].(map[string]any)
		if dd == nil {
			continue
		}
		if ms, _ := dd["methods"].([]any); len(ms) > 0 {
			ifaceMethods = ms
		}
	}
	if len(ifaceMethods) != 1 {
		t.Fatalf("interface methods = %d entries, want 1 (M)", len(ifaceMethods))
	}
	im, _ := ifaceMethods[0].(map[string]any)
	if n, _ := im["name"].(string); n != "M" {
		t.Errorf("interface method name = %q, want M", n)
	}
}
