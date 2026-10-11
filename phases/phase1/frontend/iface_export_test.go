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

// TestToolVersionBump: ADR-0004 §D1 — gofrontend 0.4.0 → 0.5.0;
// ADR-0005 §D3 — gofrontend 0.5.0 → 0.6.0 (synthetic time/context,
// timenew/timeop/timec/timenow/timesleep/timeafter/timetick/timeafterfunc
// wire nodes, chanclose marker).
// ADR-0005 Appendix C — gofrontend 0.6.0 → 0.7.0 (channel-ops lowering:
// makechan/send/recv/chanclose/select/selectcase wire nodes; the
// chanclose marker is retired, unary "<-" replaced by recv).
func TestToolVersionBump(t *testing.T) {
	if toolVersion != "0.7.0" {
		t.Fatalf("toolVersion = %q, want 0.7.0 (ADR-0005 Appendix C)", toolVersion)
	}
	out, err := ExportFile(writeIfaceProg(t, "package main\n\nfunc main() {}\n"))
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	if !strings.Contains(out, `"version": "0.7.0"`) {
		t.Fatal("export generator stanza does not stamp version 0.7.0")
	}
}

// TestIfaceMethodTablePromotedIndex: promoted methods carry the go/types
// Selection.Index() field path in the wire "index" field (P3-M3 receiver
// adaptation); direct methods carry an empty path.
func TestIfaceMethodTablePromotedIndex(t *testing.T) {
	const src = `package main

type Writer interface{ Write(p []byte) int }

type Buf struct{ n int }

func (b *Buf) Write(p []byte) int { b.n += len(p); return len(p) }

type MyWriter struct{ *Buf }

func main() {
	mw := &MyWriter{}
	var w Writer = mw
	println(w.Write([]byte("hi")))
}
`
	out, err := ExportFile(writeIfaceProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	doc := decodeExport(t, out)
	entries := typeEntries(t, doc)

	indexOf := func(typeName string, method string) []any {
		t.Helper()
		id, _ := findNamedEntry(t, entries, typeName)
		for _, e := range entries {
			em, _ := e.(map[string]any)
			if em["kind"] != "pointer" {
				continue
			}
			d, _ := em["detail"].(map[string]any)
			if elem, _ := d["elem"].(float64); int(elem) != id {
				continue
			}
			methods, _ := d["methods"].([]any)
			for _, m := range methods {
				mm, _ := m.(map[string]any)
				if mm["name"] == method {
					idx, ok := mm["index"].([]any)
					if !ok {
						t.Fatalf("%s.%s: no wire \"index\" field", typeName, method)
					}
					return idx
				}
			}
			// Duplicate structural pointer entries can exist (one minted
			// while exporting the method's own receiver, before funcByObj
			// registration); the launcher merges by descriptor name.
			// Keep scanning for the entry that carries the method.
		}
		t.Fatalf("no pointer entry for %s carries method %s", typeName, method)
		return nil
	}

	// *Buf.Write is direct: empty path.
	if idx := indexOf("main.Buf", "Write"); len(idx) != 0 {
		t.Fatalf("*Buf.Write index = %v, want empty (direct method)", idx)
	}
	// *MyWriter.Write is promoted via embedded field 0: path [0].
	idx := indexOf("main.MyWriter", "Write")
	if len(idx) != 1 {
		t.Fatalf("*MyWriter.Write index = %v, want [0] (promoted via field 0)", idx)
	}
	if f, _ := idx[0].(float64); f != 0 {
		t.Fatalf("*MyWriter.Write index = %v, want [0]", idx)
	}
}

// TestIfaceMethodTableValueEmbedAddrOf (P3-M9): a pointer-receiver method
// promoted via a VALUE-embedded struct field is marked supported in the
// wire ("index": [0], "addrOf": true) — the dispatcher takes the address
// of the promoted field value (gc's implicit &). Struct fields are read
// by reference, so this is sound.
func TestIfaceMethodTableValueEmbedAddrOf(t *testing.T) {
	const src = `package main

type Writer interface{ Write(p []byte) int }

type Buf struct{ n int }

func (b *Buf) Write(p []byte) int { b.n += len(p); return len(p) }

type V struct{ Buf }

func main() {
	v := &V{}
	var w Writer = v
	println(w.Write([]byte("hi")))
}
`
	out, err := ExportFile(writeIfaceProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile: export must succeed (unsupported is lazy), got %v", err)
	}
	doc := decodeExport(t, out)
	entries := typeEntries(t, doc)
	id, _ := findNamedEntry(t, entries, "main.V")
	found := false
	for _, e := range entries {
		em, _ := e.(map[string]any)
		if em["kind"] != "pointer" {
			continue
		}
		d, _ := em["detail"].(map[string]any)
		if elem, _ := d["elem"].(float64); int(elem) != id {
			continue
		}
		methods, _ := d["methods"].([]any)
		for _, m := range methods {
			mm, _ := m.(map[string]any)
			if mm["name"] != "Write" {
				continue
			}
			found = true
			idx, ok := mm["index"].([]any)
			if !ok || len(idx) != 1 {
				t.Fatalf("*V.Write index = %v (%T), want [0] (supported with addrOf)", mm["index"], mm["index"])
			}
			if ao, ok := mm["addrOf"].(bool); !ok || !ao {
				t.Fatalf("*V.Write addrOf = %v (%T), want true", mm["addrOf"], mm["addrOf"])
			}
		}
	}
	if !found {
		t.Fatal("no *V pointer entry carries Write")
	}
}

// TestIfaceMethodCallAliasReceiver (R1 P1-1): a method call through an
// alias of an interface type (type W = Writer) exports an "ifacemethod"
// wire node — the alias unwraps to the named interface, whose underlying
// interface entry is the "iface" id. Pre-fix this failed export with
// "receiver static type main.W is not an interface (exporter bug)".
func TestIfaceMethodCallAliasReceiver(t *testing.T) {
	const src = `package main

type Writer interface{ Write(p []byte) int }
type W = Writer

type Buf struct{ n int }

func (b *Buf) Write(p []byte) int { b.n += len(p); return len(p) }

func main() {
	var w W = &Buf{}
	println(w.Write([]byte("hi")))
}
`
	out, err := ExportFile(writeIfaceProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with alias-of-interface receiver: %v", err)
	}
	doc := decodeExport(t, out)
	nodes := findAllKind(doc, "ifacemethod")
	if len(nodes) != 1 {
		t.Fatalf("ifacemethod nodes = %d, want 1", len(nodes))
	}
	f := fieldsOf(nodes[0])
	if f["method"] != "Write" {
		t.Fatalf("method = %v, want Write", f["method"])
	}
	// The iface id is the underlying interface entry of main.Writer
	// (the alias resolves to the named type; the named type's
	// underlying is the interface entry).
	entries := typeEntries(t, doc)
	_, wdetail := findNamedEntry(t, entries, "main.Writer")
	under, _ := wdetail["underlying"].(float64)
	if f["iface"] != under {
		t.Fatalf("iface id = %v, want main.Writer's underlying interface id %v", f["iface"], under)
	}
}

// TestIfaceImplKey (R1 P0-1): method-table entries and interface method
// entries carry "implkey" (method name + structural signature, aliases
// expanded). Write([]byte) [interface] and Write(s B) with `type B =
// []byte` [method] share an implkey; Write(string) keys differently —
// the runtime implements check compares these, not bare names.
func TestIfaceImplKey(t *testing.T) {
	const src = `package main

type Writer interface{ Write(p []byte) int }

type B = []byte

type Good struct{}

func (g Good) Write(s B) int { return len(s) }

type Bad struct{}

func (b Bad) Write(s string) int { return 1 }

func main() {
	var w Writer = Good{}
	println(w.Write(nil))
	var _ = Bad{}
}
`
	out, err := ExportFile(writeIfaceProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	doc := decodeExport(t, out)
	entries := typeEntries(t, doc)
	implkeys := map[string]string{} // "iface.Write" / "main.Good.Write" / ...
	for _, e := range entries {
		em, _ := e.(map[string]any)
		kind, _ := em["kind"].(string)
		d, _ := em["detail"].(map[string]any)
		if kind != "interface" && kind != "named" {
			continue
		}
		name, _ := d["name"].(string)
		if kind == "named" && name != "main.Good" && name != "main.Bad" {
			continue
		}
		methods, _ := d["methods"].([]any)
		for _, m := range methods {
			mm, _ := m.(map[string]any)
			mn, _ := mm["name"].(string)
			ik, ok := mm["implkey"].(string)
			if !ok || ik == "" {
				t.Fatalf("%s.%s: missing implkey (want method-identity key)", name, mn)
			}
			if kind == "interface" {
				implkeys["iface."+mn] = ik
			} else {
				implkeys[name+"."+mn] = ik
			}
		}
	}
	ifaceWrite, ok := implkeys["iface.Write"]
	if !ok {
		t.Fatal("no implkey for interface Writer.Write")
	}
	// Alias-expanded: Good.Write(s B) with B = []byte shares the
	// interface's implkey.
	if goodWrite, ok := implkeys["main.Good.Write"]; !ok {
		t.Fatal("no implkey for main.Good.Write")
	} else if goodWrite != ifaceWrite {
		t.Fatalf("main.Good.Write implkey = %q, want the interface's %q (alias not expanded)",
			goodWrite, ifaceWrite)
	}
	// Wrong signature: Bad.Write(string) keys differently.
	if badWrite, ok := implkeys["main.Bad.Write"]; !ok {
		t.Fatal("no implkey for main.Bad.Write")
	} else if badWrite == ifaceWrite {
		t.Fatalf("main.Bad.Write implkey = %q, must differ from the interface's (name-only check would collide)",
			badWrite)
	}
}

// TestIfaceMultiResultScopeError (R2 F4): an interface method with two
// results (e.g. Read(p []byte) (int, error)) is a named frontend/v1 scope
// error — the dispatch mechanics for multi-result methods are untested,
// so the shape must fail fast, never silently miscompile.
func TestIfaceMultiResultScopeError(t *testing.T) {
	const src = `package main

type Reader interface{ Read(p []byte) (int, error) }

type B struct{}

func (b B) Read(p []byte) (int, error) { return 0, nil }

func main() {
	var r Reader = B{}
	n, _ := r.Read(nil)
	println(n)
}
`
	_, err := ExportFile(writeIfaceProg(t, src))
	if err == nil {
		t.Fatal("expected multi-result scope error, got success")
	}
	const want = "at most one result is supported in frontend/v1 scope (WS D)"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err, want)
	}
}

// TestIfaceInstantiationMethodTable (P3-M9 instantiation method tables):
// the named entry for a generic instantiation with methods (Box[int])
// carries a "methods" list with one entry per template method —
// monomorphized, not skipped — and NO "templateMethodsSkipped" marker.
// The entry's "obj" references the emitted instantiation object, so
// interface dispatch (itab) targets real code. (This supersedes the
// R1 P1-2 P3-M3 scope marker: boxing Box[int] as a non-empty interface
// is now supported.)
func TestIfaceInstantiationMethodTable(t *testing.T) {
	const src = `package main

type Getter interface{ Get() int }

type Box[T any] struct{ v T }

func (b Box[T]) Get() int { return 42 }

func main() {
	var g Getter = Box[int]{v: 7}
	println(g.Get())
}
`
	out, err := ExportFile(writeIfaceProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	doc := decodeExport(t, out)
	entries := typeEntries(t, doc)
	_, detail := findNamedEntry(t, entries, "main.Box[int]")
	methods, _ := detail["methods"].([]any)
	if len(methods) != 1 {
		t.Fatalf("main.Box[int] methods = %d entries, want 1 (monomorphized Get)", len(methods))
	}
	mentry, _ := methods[0].(map[string]any)
	if mentry["name"] != "Get" {
		t.Fatalf("main.Box[int] method name = %v, want Get", mentry["name"])
	}
	if _, has := detail["templateMethodsSkipped"]; has {
		t.Fatal("main.Box[int] unexpectedly carries templateMethodsSkipped")
	}
	// The entry's obj must reference an emitted instantiation object.
	objID, _ := mentry["obj"].(float64)
	objects, _ := doc["objects"].([]any)
	if int(objID) < 0 || int(objID) >= len(objects) {
		t.Fatalf("method obj %v out of range (objects=%d)", objID, len(objects))
	}
	obj, _ := objects[int(objID)].(map[string]any)
	if obj["kind"] != "inst" {
		t.Fatalf("method obj kind = %v, want inst (monomorphized)", obj["kind"])
	}
	// A non-generic named type with methods must NOT carry the marker.
	_, bdetail := findNamedEntry(t, entries, "main.Getter")
	if _, has := bdetail["templateMethodsSkipped"]; has {
		t.Fatal("main.Getter unexpectedly carries templateMethodsSkipped")
	}
}

// TestFreshSigIDKeepsTypeListParallel (R2 P2-3): freshSigID appends to
// e.types, so it must append a (nil) placeholder to e.typeList to keep
// the two parallel — recomputeMethodTables indexes e.types by e.typeList
// position, and a future pre-recompute caller would otherwise silently
// misalign method tables. Red-green: fails before the placeholder
// (len 1 vs 0), passes after.
func TestFreshSigIDKeepsTypeListParallel(t *testing.T) {
	e := &exporter{}
	if _, err := e.freshSigID(nil, types.NewTuple(), types.NewTuple(), false); err != nil {
		t.Fatalf("freshSigID: %v", err)
	}
	if len(e.types) != len(e.typeList) {
		t.Fatalf("len(types)=%d != len(typeList)=%d after freshSigID", len(e.types), len(e.typeList))
	}
	if e.typeList[0] != nil {
		t.Fatalf("typeList placeholder = %v, want nil (signature entries have no go/types type)", e.typeList[0])
	}
}

// TestMentionsTypeParamAliasUnion (R2 P2-4): mentionsTypeParam must see
// through *types.Alias and *types.Union — an alias could otherwise
// smuggle a type parameter past the P3-M3 template-method fail-fast.
func TestMentionsTypeParamAliasUnion(t *testing.T) {
	tp := types.NewTypeParam(types.NewTypeName(0, nil, "T", nil), nil)
	// Alias to a bare type parameter.
	a := types.NewAlias(types.NewTypeName(0, nil, "A", nil), tp)
	if !mentionsTypeParam(a) {
		t.Fatal("mentionsTypeParam(alias to TypeParam) = false, want true")
	}
	// Union with a type-parameter term.
	u := types.NewUnion([]*types.Term{types.NewTerm(false, tp), types.NewTerm(false, types.Typ[types.Int])})
	if !mentionsTypeParam(u) {
		t.Fatal("mentionsTypeParam(union with TypeParam term) = false, want true")
	}
	// Negatives: alias/union without type parameters.
	ai := types.NewAlias(types.NewTypeName(0, nil, "AI", nil), types.Typ[types.Int])
	if mentionsTypeParam(ai) {
		t.Fatal("mentionsTypeParam(alias to int) = true, want false")
	}
	ui := types.NewUnion([]*types.Term{types.NewTerm(false, types.Typ[types.Int])})
	if mentionsTypeParam(ui) {
		t.Fatal("mentionsTypeParam(union of int) = true, want false")
	}
}

// TestIfaceMethodTableValueEmbedPrimitiveUnsupported (P3-M9): a
// pointer-receiver method promoted via a VALUE-embedded non-struct field
// (e.g. a defined int type) is still marked unsupported in the wire
// ("index": null) — taking the address would need writeback for the
// primitive field value (out of scope). The mark is lazy: the export
// succeeds, and dispatch fails fast with a named P3-M3 scope error only
// if the method is actually dispatched.
func TestIfaceMethodTableValueEmbedPrimitiveUnsupported(t *testing.T) {
	const src = `package main

type Counter int

func (c *Counter) Inc() { println(1) }

type V struct{ Counter }

func main() {
	v := &V{Counter: 41}
	var i interface{ Inc() } = v
	i.Inc()
}
`
	out, err := ExportFile(writeIfaceProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile: export must succeed (unsupported is lazy), got %v", err)
	}
	doc := decodeExport(t, out)
	entries := typeEntries(t, doc)
	id, _ := findNamedEntry(t, entries, "main.V")
	found := false
	for _, e := range entries {
		em, _ := e.(map[string]any)
		if em["kind"] != "pointer" {
			continue
		}
		d, _ := em["detail"].(map[string]any)
		if elem, _ := d["elem"].(float64); int(elem) != id {
			continue
		}
		methods, _ := d["methods"].([]any)
		for _, m := range methods {
			mm, _ := m.(map[string]any)
			if mm["name"] != "Inc" {
				continue
			}
			found = true
			if idx, ok := mm["index"]; !ok || idx != nil {
				t.Fatalf("*V.Inc index = %v (%T), want JSON null (unsupported)", mm["index"], mm["index"])
			}
			if ao, ok := mm["addrOf"].(bool); !ok || ao {
				t.Fatalf("*V.Inc addrOf = %v (%T), want false", mm["addrOf"], mm["addrOf"])
			}
		}
	}
	if !found {
		t.Fatal("no *V pointer entry carries Inc")
	}
}
