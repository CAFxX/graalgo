package main

// P3-M3 (WS D, ADR-0003): exporter tests for interface method calls and
// method tables on type-table entries.
// Red-green: these fail before the exporter change (every interface method
// call is a scope error; "methods" stays empty; toolVersion is 0.3.0) and
// pass after.

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeIfaceProg writes src to a temp .go file and returns its path.
func writeIfaceProg(t *testing.T, src string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "prog.go")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

// findAllKind walks the decoded export JSON and returns every node with the
// given kind.
func findAllKind(n any, kind string) []map[string]any {
	var out []map[string]any
	var rec func(n any)
	rec = func(n any) {
		switch v := n.(type) {
		case map[string]any:
			if v["kind"] == kind {
				out = append(out, v)
			}
			for _, c := range v {
				rec(c)
			}
		case []any:
			for _, c := range v {
				rec(c)
			}
		}
	}
	rec(n)
	return out
}

// decodeExport unmarshals an export JSON document.
func decodeExport(t *testing.T, out string) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("export is not JSON: %v", err)
	}
	return doc
}

// typeEntries returns the decoded "types" array.
func typeEntries(t *testing.T, doc map[string]any) []any {
	t.Helper()
	types, ok := doc["types"].([]any)
	if !ok {
		t.Fatal("export has no types array")
	}
	return types
}

// findNamedEntry returns the type-table id and detail of the "named" entry
// with the given qualified name.
func findNamedEntry(t *testing.T, entries []any, name string) (int, map[string]any) {
	t.Helper()
	for i, e := range entries {
		em, ok := e.(map[string]any)
		if !ok || em["kind"] != "named" {
			continue
		}
		d, _ := em["detail"].(map[string]any)
		if d["name"] == name {
			return i, d
		}
	}
	t.Fatalf("no named type-table entry for %s", name)
	return -1, nil
}

const ifaceProbeSrc = `package main

type Writer interface {
	Write(p []byte) int
	Reset()
}

type Buf struct{ n int }

func (b *Buf) Write(p []byte) int {
	b.n += len(p)
	return len(p)
}

func (b *Buf) Reset() { b.n = 0 }

func (b Buf) Size() int { return b.n }

func main() {
	var w Writer = &Buf{}
	m := w.Write([]byte("hi"))
	println(m)
	w.Reset()
	var bv Buf
	println(bv.Size())
}
`

// TestIfaceMethodCallExports: w.Write(...) on an interface-typed receiver
// exports an "ifacemethod" wire node (ADR-0003 §2.1), not a scope error.
func TestIfaceMethodCallExports(t *testing.T) {
	out, err := ExportFile(writeIfaceProg(t, ifaceProbeSrc))
	if err != nil {
		t.Fatalf("ExportFile with interface method call: %v", err)
	}
	doc := decodeExport(t, out)
	nodes := findAllKind(doc, "ifacemethod")
	if len(nodes) != 2 {
		t.Fatalf("ifacemethod nodes = %d, want 2 (Write, Reset)", len(nodes))
	}
	byMethod := map[string]map[string]any{}
	for _, n := range nodes {
		f := fieldsOf(n)
		m, _ := f["method"].(string)
		byMethod[m] = f
	}

	// Write: single int result.
	wf := byMethod["Write"]
	if wf == nil {
		t.Fatal("no ifacemethod node for Write")
	}
	if _, ok := wf["recv"].(map[string]any); !ok {
		t.Fatalf("Write recv = %T, want a node", wf["recv"])
	}
	args, _ := wf["args"].([]any)
	if len(args) != 1 {
		t.Fatalf("Write args = %d, want 1 (receiver not included)", len(args))
	}
	ifaceID, ok := wf["iface"].(float64)
	if !ok {
		t.Fatalf("Write iface = %T, want a type-table id", wf["iface"])
	}
	sigID, ok := wf["sig"].(float64)
	if !ok {
		t.Fatalf("Write sig = %T, want a type-table id", wf["sig"])
	}
	if _, ok := wf["type"].(float64); !ok {
		t.Fatalf("Write type = %v (%T), want a result type id", wf["type"], wf["type"])
	}

	// Reset: no results → type null.
	rf := byMethod["Reset"]
	if rf == nil {
		t.Fatal("no ifacemethod node for Reset")
	}
	if rf["type"] != nil {
		t.Fatalf("Reset type = %v, want null (no results)", rf["type"])
	}

	// The iface id resolves through the named entry to the interface
	// entry, whose method list is the itab's method-name source.
	entries := typeEntries(t, doc)
	_, wdetail := findNamedEntry(t, entries, "main.Writer")
	under, _ := wdetail["underlying"].(float64)
	ientry, _ := entries[int(under)].(map[string]any)
	if ientry["kind"] != "interface" {
		t.Fatalf("Writer underlying kind = %v, want interface", ientry["kind"])
	}
	idetail, _ := ientry["detail"].(map[string]any)
	methods, _ := idetail["methods"].([]any)
	names := map[string]bool{}
	for _, m := range methods {
		mm, _ := m.(map[string]any)
		if n, ok := mm["name"].(string); ok {
			names[n] = true
		}
	}
	if !names["Write"] || !names["Reset"] {
		t.Fatalf("interface method list = %v, want Write and Reset", names)
	}

	// The sig id is the method signature with no receiver: one param,
	// one result, non-variadic.
	sentry, _ := entries[int(sigID)].(map[string]any)
	if sentry["kind"] != "signature" {
		t.Fatalf("sig entry kind = %v, want signature", sentry["kind"])
	}
	sdetail, _ := sentry["detail"].(map[string]any)
	if params, _ := sdetail["params"].([]any); len(params) != 1 {
		t.Fatalf("sig params = %d, want 1 (no receiver)", len(params))
	}
	if results, _ := sdetail["results"].([]any); len(results) != 1 {
		t.Fatalf("sig results = %d, want 1", len(results))
	}
	if sdetail["variadic"] != false {
		t.Fatalf("sig variadic = %v, want false", sdetail["variadic"])
	}

	// The iface id is the *interface* type-table id (ADR-0003 §2.1 wire:
	// "iface" is the interface type-table id, the method list source) —
	// the underlying of the main.Writer named entry, not the named id.
	// (Regression: the exporter used to emit the named id, which the
	// Java consumer rejects — "iface id is not an interface type".)
	if int(ifaceID) != int(under) {
		t.Fatalf("iface id = %v, want the underlying interface entry id %v", ifaceID, under)
	}
	if ientry["kind"] != "interface" {
		t.Fatalf("iface entry kind = %v, want interface", ientry["kind"])
	}
}

func namedIDOf(t *testing.T, entries []any, name string) int {
	t.Helper()
	id, _ := findNamedEntry(t, entries, name)
	return id
}

// TestIfaceMethodTable: named entries carry the populated "methods" list
// (ADR-0003 §D2 — the WS D hook), and pointer entries carry the full *T
// method set.
func TestIfaceMethodTable(t *testing.T) {
	out, err := ExportFile(writeIfaceProg(t, ifaceProbeSrc))
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	doc := decodeExport(t, out)
	entries := typeEntries(t, doc)
	objects, _ := doc["objects"].([]any)

	// main.Buf: value-receiver Size only (Write/Reset need *Buf).
	bufID, bdetail := findNamedEntry(t, entries, "main.Buf")
	methods, _ := bdetail["methods"].([]any)
	got := map[string]map[string]any{}
	for _, m := range methods {
		mm, _ := m.(map[string]any)
		name, _ := mm["name"].(string)
		got[name] = mm
	}
	want := map[string]string{"Size": "value"}
	if len(got) != len(want) {
		t.Fatalf("Buf methods = %v, want exactly %v", keysOf(got), keysOfStr(want))
	}
	for name, recv := range want {
		mm := got[name]
		if mm == nil {
			t.Fatalf("Buf methods missing %s", name)
		}
		if mm["recv"] != recv {
			t.Fatalf("Buf.%s recv = %v, want %s", name, mm["recv"], recv)
		}
		objID, ok := mm["obj"].(float64)
		if !ok {
			t.Fatalf("Buf.%s obj = %T, want an object id", name, mm["obj"])
		}
		obj, _ := objects[int(objID)].(map[string]any)
		if obj["kind"] != "func" || obj["name"] != name {
			t.Fatalf("Buf.%s obj %v resolves to %v/%v, want func/%s",
				name, objID, obj["kind"], obj["name"], name)
		}
	}

	// *Buf: the full method set — value AND pointer receivers.
	var ptrDetail map[string]any
	found := false
	for _, e := range entries {
		em, ok := e.(map[string]any)
		if !ok || em["kind"] != "pointer" {
			continue
		}
		d, _ := em["detail"].(map[string]any)
		if elem, _ := d["elem"].(float64); int(elem) == bufID {
			ptrDetail = d
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no pointer type-table entry for *Buf")
	}
	pmethods, _ := ptrDetail["methods"].([]any)
	pgot := map[string]string{}
	for _, m := range pmethods {
		mm, _ := m.(map[string]any)
		name, _ := mm["name"].(string)
		recv, _ := mm["recv"].(string)
		pgot[name] = recv
		objID, ok := mm["obj"].(float64)
		if !ok {
			t.Fatalf("*Buf.%s obj = %T, want an object id", name, mm["obj"])
		}
		obj, _ := objects[int(objID)].(map[string]any)
		if obj["kind"] != "func" || obj["name"] != name {
			t.Fatalf("*Buf.%s obj %v resolves to %v/%v, want func/%s",
				name, objID, obj["kind"], obj["name"], name)
		}
	}
	pwant := map[string]string{"Reset": "pointer", "Size": "value", "Write": "pointer"}
	if len(pgot) != len(pwant) {
		t.Fatalf("*Buf methods = %v, want %v", pgot, pwant)
	}
	for name, recv := range pwant {
		if pgot[name] != recv {
			t.Fatalf("*Buf.%s recv = %q, want %q", name, pgot[name], recv)
		}
	}
}

func keysOf(m map[string]map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func keysOfStr(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

const ifaceMethodValueSrc = `package main

type Writer interface{ Write(p []byte) int }

type Buf struct{ n int }

func (b *Buf) Write(p []byte) int {
	b.n += len(p)
	return len(p)
}

func main() {
	var w Writer = &Buf{}
	f := w.Write
	_ = f
}
`

// TestIfaceMethodValueScopeError: a method value on an interface receiver
// keeps failing, but with a named P3-M3 scope error (greppable re-test
// trigger), not the confusing WS H one.
func TestIfaceMethodValueScopeError(t *testing.T) {
	_, err := ExportFile(writeIfaceProg(t, ifaceMethodValueSrc))
	if err == nil {
		t.Fatal("expected method-value scope error, got success")
	}
	const want = "method value via interface (P3-M3 scope)"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err, want)
	}
}

const ifaceMethodExprSrc = `package main

type Writer interface{ Write(p []byte) int }

type Buf struct{ n int }

func (b *Buf) Write(p []byte) int {
	b.n += len(p)
	return len(p)
}

func main() {
	var w Writer = &Buf{}
	Writer.Write(w, []byte("hi"))
}
`

const ifaceMethodExprValueSrc = `package main

type Writer interface{ Write(p []byte) int }

type Buf struct{ n int }

func (b *Buf) Write(p []byte) int {
	b.n += len(p)
	return len(p)
}

func main() {
	var w Writer = &Buf{}
	f := Writer.Write
	_ = f
	_ = w
}
`

// TestIfaceMethodExprScopeError: a method expression on an interface type
// (call and value positions) keeps failing, but with a named P3-M3 scope
// error (greppable re-test trigger).
func TestIfaceMethodExprScopeError(t *testing.T) {
	for _, src := range []string{ifaceMethodExprSrc, ifaceMethodExprValueSrc} {
		_, err := ExportFile(writeIfaceProg(t, src))
		if err == nil {
			t.Fatal("expected method-expression scope error, got success")
		}
		const want = "method expression via interface (P3-M3 scope)"
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err, want)
		}
	}
}

// TestIfaceGenericMethodScopeError: a generic (template) method reached via
// an interface method set is a named P3-M3 scope error. go/types rejects
// generic interface methods in source ("interface method must have no type
// parameters"), so the interface is built synthetically around a real
// template method Func from a typechecked snippet.
func TestIfaceGenericMethodScopeError(t *testing.T) {
	const src = `package main

type Box[T any] struct{ v T }

func (b Box[T]) Get() T { return b.v }

func main() {}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "prog.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{Defs: make(map[*ast.Ident]types.Object)}
	conf := types.Config{}
	pkg, err := conf.Check("main", fset, []*ast.File{f}, info)
	if err != nil {
		t.Fatalf("typecheck: %v", err)
	}
	box := pkg.Scope().Lookup("Box").Type().(*types.Named)
	mobj, ok := types.NewMethodSet(box).At(0).Obj().(*types.Func)
	if !ok {
		t.Fatal("no method on Box")
	}
	iface := types.NewInterfaceType([]*types.Func{mobj}, nil)
	iface.Complete()
	e := &exporter{pkg: pkg}
	_, err = e.resolveIfaceMethod(iface, "Get")
	if err == nil {
		t.Fatal("expected generic-method scope error, got success")
	}
	const want = "generic method via interface (P3-M3 scope)"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err, want)
	}
}

// TestToolVersionBump: ADR-0003 §D3 — gofrontend 0.3.0 → 0.4.0.
func TestToolVersionBump(t *testing.T) {
	if toolVersion != "0.4.0" {
		t.Fatalf("toolVersion = %q, want 0.4.0 (ADR-0003)", toolVersion)
	}
	out, err := ExportFile(writeIfaceProg(t, "package main\n\nfunc main() {}\n"))
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	if !strings.Contains(out, `"version": "0.4.0"`) {
		t.Fatal("export generator stanza does not stamp version 0.4.0")
	}
}
