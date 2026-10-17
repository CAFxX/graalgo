// Pinning tests for the P1-V1…P1-V7 remediation (review 2026-10-17).
//
// Each test is demonstrably capable of failing: it asserts the fixed
// behavior (globals emitted, receivers present, real offsets, per-spec
// types, range targets, selection info) that the pre-fix emitter got
// wrong or dropped silently.
package emit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/parser"
)

// emitDoc runs the full pipeline on src and returns the document.
func emitDoc(t *testing.T, src string) *Document {
	t.Helper()
	dir := t.TempDir()
	fn := filepath.Join(dir, "p.go")
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

// walkNodes calls fn for every node map in v (document tree walk).
func walkNodes(v any, fn func(m map[string]any)) {
	switch v := v.(type) {
	case map[string]any:
		fn(v)
		for _, val := range v {
			walkNodes(val, fn)
		}
	case []any:
		for _, e := range v {
			walkNodes(e, fn)
		}
	}
}

// nodesOfKind returns all node maps with the given kind, searching func
// bodies and globals.
func nodesOfKind(doc *Document, kind string) []map[string]any {
	var out []map[string]any
	add := func(v any) {
		walkNodes(v, func(m map[string]any) {
			if k, _ := m["kind"].(string); k == kind {
				out = append(out, m)
			}
		})
	}
	add(doc.Funcs)
	add(doc.Globals)
	return out
}

func nodeFields(m map[string]any) map[string]any {
	f, _ := m["fields"].(map[string]any)
	return f
}

// findFunc returns the funcdecl node for name, or nil.
func findFunc(doc *Document, name string) map[string]any {
	for _, f := range doc.Funcs {
		m, ok := f.(map[string]any)
		if !ok {
			continue
		}
		if n, _ := nodeFields(m)["name"].(string); n == name {
			return m
		}
	}
	return nil
}

// findType returns the first type-table entry with kind and a name
// containing substr.
func findType(doc *Document, kind, substr string) map[string]any {
	for _, t := range doc.Types {
		m, ok := t.(map[string]any)
		if !ok {
			continue
		}
		if k, _ := m["kind"].(string); k != kind {
			continue
		}
		d, _ := m["detail"].(map[string]any)
		if n, _ := d["name"].(string); strings.Contains(n, substr) {
			return m
		}
	}
	return nil
}

// findObject returns the first object-table entry with kind and name.
func findObject(doc *Document, kind, name string) map[string]any {
	for _, o := range doc.Objects {
		m, ok := o.(map[string]any)
		if !ok {
			continue
		}
		if k, _ := m["kind"].(string); k != kind {
			continue
		}
		if n, _ := m["name"].(string); n == name {
			return m
		}
	}
	return nil
}

// globalFirstName returns the first declared name of a global node.
func globalFirstName(g map[string]any) string {
	names, _ := nodeFields(g)["names"].([]any)
	if len(names) == 0 {
		return ""
	}
	nm, _ := names[0].(map[string]any)
	s, _ := nm["name"].(string)
	return s
}

// TestTopLevelGlobalsEmitted (P1-V1): package-level var/const/type
// declarations are represented — vars in Globals (initialization order),
// consts in the object table, named types in the type table. Pre-fix the
// emitter hardcoded Globals to [] and dropped every non-function decl.
func TestTopLevelGlobalsEmitted(t *testing.T) {
	src := `package main

var A = 1
var B = A + 1
var (
	C int
	D string
)
var _ = 42

const K = 7
const (
	I = iota
	J
)

type Point struct{ X, Y int }
type MyInt int

func main() {
	println(A, B, C, D, K, I, J, MyInt(0), Point{X: 1})
}
`
	doc := emitDoc(t, src)

	// Five var specs → five global nodes (A, B, C, D, blank).
	if len(doc.Globals) != 5 {
		t.Fatalf("Globals has %d entries, want 5 (one per var spec)", len(doc.Globals))
	}
	for i, g := range doc.Globals {
		m, ok := g.(map[string]any)
		if !ok || m["kind"] != "global" {
			t.Fatalf("Globals[%d] is not a global node: %#v", i, g)
		}
	}

	// Initialization order: B depends on A, so A comes first; the rest
	// follow in declaration order.
	var order []string
	for _, g := range doc.Globals {
		order = append(order, globalFirstName(g.(map[string]any)))
	}
	want := []string{"A", "B", "C", "D", "_"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("global order = %v, want %v (initialization order)", order, want)
		}
	}

	// B's initializer references A: the expr must be present, not dropped.
	var bInit bool
	for _, g := range doc.Globals {
		if globalFirstName(g.(map[string]any)) != "B" {
			continue
		}
		vals, _ := nodeFields(g.(map[string]any))["values"].([]any)
		if len(vals) != 1 {
			t.Fatalf("B values = %d, want 1", len(vals))
		}
		bInit = true
	}
	if !bInit {
		t.Fatal("no global node for B")
	}

	// Consts fold through the object table with exact values (iota included).
	k := findObject(doc, "const", "K")
	if k == nil {
		t.Fatal("const K missing from object table")
	}
	if v, _ := k["value"].(string); v != "7" {
		t.Fatalf("K value = %q, want 7", v)
	}
	j := findObject(doc, "const", "J")
	if j == nil {
		t.Fatal("const J (iota) missing from object table")
	}
	if v, _ := j["value"].(string); v != "1" {
		t.Fatalf("J value = %q, want 1 (iota)", v)
	}

	// Named types are in the type table even though nothing references
	// MyInt's declaration position directly.
	if findType(doc, "named", "Point") == nil {
		t.Fatal("named type Point missing from type table")
	}
	if findType(doc, "named", "MyInt") == nil {
		t.Fatal("named type MyInt missing from type table")
	}
}

// TestGlobalInitOrderReversed (P1-V1): a var declared textually *after* a
// var whose initializer uses it is still emitted after its dependency.
func TestGlobalInitOrderReversed(t *testing.T) {
	src := `package main

var Late = Early + 1
var Early = 41

func main() { println(Late) }
`
	doc := emitDoc(t, src)
	if len(doc.Globals) != 2 {
		t.Fatalf("Globals has %d entries, want 2", len(doc.Globals))
	}
	if got := globalFirstName(doc.Globals[0].(map[string]any)); got != "Early" {
		t.Fatalf("first global = %q, want Early (dependency first)", got)
	}
	if got := globalFirstName(doc.Globals[1].(map[string]any)); got != "Late" {
		t.Fatalf("second global = %q, want Late", got)
	}
}

// TestMethodReceiversEmitted (P1-V4): methods carry their receiver with a
// recv:true marker, and the named type's methods list is populated.
// Pre-fix, fd.Recv was never read and methods was [].
func TestMethodReceiversEmitted(t *testing.T) {
	src := `package main

type Counter struct{ N int }

func (c Counter) Get() int  { return c.N }
func (c *Counter) Inc()     { c.N++ }
func (Counter) Zero() int   { return 0 }
func Plain() int            { return 1 }

func main() {
	var c Counter
	c.Inc()
	println(c.Get(), Counter{}.Zero(), Plain())
}
`
	doc := emitDoc(t, src)

	get := findFunc(doc, "Get")
	if get == nil {
		t.Fatal("funcdecl Get missing")
	}
	recv, _ := nodeFields(get)["recv"].(map[string]any)
	if recv == nil {
		t.Fatal("Get has no recv (receiver dropped)")
	}
	if r, _ := nodeFields(recv)["recv"].(bool); !r {
		t.Fatal("Get recv marker != true")
	}
	if n, _ := nodeFields(recv)["name"].(string); n != "c" {
		t.Fatalf("Get recv name = %q, want c", n)
	}

	inc := findFunc(doc, "Inc")
	if inc == nil {
		t.Fatal("funcdecl Inc missing")
	}
	incRecv, _ := nodeFields(inc)["recv"].(map[string]any)
	if incRecv == nil {
		t.Fatal("Inc has no recv")
	}
	// Pointer receiver: the recv type must be *Counter, not Counter.
	recvTypeID, _ := nodeFields(incRecv)["type"].(int)
	rt, ok := doc.Types[recvTypeID].(map[string]any)
	if !ok {
		t.Fatalf("recv type id %d out of range", recvTypeID)
	}
	if k, _ := rt["kind"].(string); k != "pointer" {
		t.Fatalf("Inc recv type kind = %q, want pointer", k)
	}

	// Unnamed receiver still emits (name empty, obj/type present).
	zero := findFunc(doc, "Zero")
	if zero == nil {
		t.Fatal("funcdecl Zero missing")
	}
	if zr, _ := nodeFields(zero)["recv"].(map[string]any); zr == nil {
		t.Fatal("Zero (unnamed receiver) has no recv")
	}

	// Plain function: recv must be nil, not a dropped marker.
	plain := findFunc(doc, "Plain")
	if plain == nil {
		t.Fatal("funcdecl Plain missing")
	}
	if r := nodeFields(plain)["recv"]; r != nil {
		t.Fatalf("Plain recv = %v, want nil", r)
	}

	// Named type methods list populated with receiver kinds.
	ct := findType(doc, "named", "Counter")
	if ct == nil {
		t.Fatal("named type Counter missing")
	}
	methods, _ := ct["detail"].(map[string]any)["methods"].([]any)
	if len(methods) != 3 {
		t.Fatalf("Counter methods = %d, want 3", len(methods))
	}
	recvKinds := map[string]string{}
	for _, m := range methods {
		mm, _ := m.(map[string]any)
		name, _ := mm["name"].(string)
		recvKinds[name], _ = mm["recv"].(string)
		if _, ok := mm["obj"].(int); !ok {
			t.Fatalf("method %s has no obj id", name)
		}
		if _, ok := mm["sig"].(int); !ok {
			t.Fatalf("method %s has no sig id", name)
		}
	}
	if recvKinds["Get"] != "value" || recvKinds["Zero"] != "value" {
		t.Fatalf("value-receiver kinds wrong: %v", recvKinds)
	}
	if recvKinds["Inc"] != "pointer" {
		t.Fatalf("pointer-receiver kind wrong: %v", recvKinds)
	}
}

// TestStructFieldOffsets (P1-V5): struct fields carry real byte offsets
// from types.SizesFor("gc","amd64"), not zeros. Generic structs carry -1.
func TestStructFieldOffsets(t *testing.T) {
	src := `package main

type Rec struct {
	A int8
	B int64
	C string
}

type GBox[T any] struct {
	X T
	Y int
}

func main() {
	r := Rec{A: 1, B: 2, C: "x"}
	g := GBox[int]{X: 1, Y: 2}
	println(r.A, g.Y)
}
`
	doc := emitDoc(t, src)
	rec := findType(doc, "struct", "")
	_ = rec
	// Find the struct underlying Rec via the named type.
	named := findType(doc, "named", "Rec")
	if named == nil {
		t.Fatal("named type Rec missing")
	}
	uid, _ := named["detail"].(map[string]any)["underlying"].(int)
	st, ok := doc.Types[uid].(map[string]any)
	if !ok || st["kind"] != "struct" {
		t.Fatalf("Rec underlying is not a struct entry")
	}
	fields, _ := st["detail"].(map[string]any)["fields"].([]any)
	if len(fields) != 3 {
		t.Fatalf("Rec fields = %d, want 3", len(fields))
	}
	offs := map[string]int64{}
	for _, f := range fields {
		fm, _ := f.(map[string]any)
		name, _ := fm["name"].(string)
		off, _ := fm["offset"].(int64)
		offs[name] = off
	}
	// gc/amd64: int8 @0, int64 @8 (aligned), string @16.
	if offs["A"] != 0 || offs["B"] != 8 || offs["C"] != 16 {
		t.Fatalf("Rec offsets = %v, want A:0 B:8 C:16 (real layout, not zeros)", offs)
	}

	// Generic named type: underlying struct offsets are -1 (unknown).
	gnamed := findType(doc, "named", "GBox")
	if gnamed == nil {
		t.Fatal("named type GBox missing")
	}
	guid, _ := gnamed["detail"].(map[string]any)["underlying"].(int)
	gst, ok := doc.Types[guid].(map[string]any)
	if !ok || gst["kind"] != "struct" {
		t.Fatal("GBox underlying is not a struct entry")
	}
	for _, f := range gst["detail"].(map[string]any)["fields"].([]any) {
		fm, _ := f.(map[string]any)
		if off, _ := fm["offset"].(int64); off != -1 {
			t.Fatalf("generic struct field %v offset = %d, want -1 (unknown)", fm["name"], off)
		}
	}
}

// TestInterfaceMethodsEmbedded (P1-V5): interface method and embedded
// lists are populated, not empty placeholders.
func TestInterfaceMethodsEmbedded(t *testing.T) {
	src := `package main

type Reader interface {
	Read(p []byte) int
}
type ReadWriter interface {
	Reader
	Write(p []byte) int
}

func main() {
	var _ ReadWriter
}
`
	doc := emitDoc(t, src)
	rw := findType(doc, "interface", "ReadWriter")
	if rw == nil {
		// Find by scanning: named ReadWriter's underlying.
		named := findType(doc, "named", "ReadWriter")
		if named == nil {
			t.Fatal("ReadWriter missing from type table")
		}
		uid, _ := named["detail"].(map[string]any)["underlying"].(int)
		rw, _ = doc.Types[uid].(map[string]any)
	}
	if rw == nil || rw["kind"] != "interface" {
		t.Fatal("ReadWriter interface entry missing")
	}
	methods, _ := rw["detail"].(map[string]any)["methods"].([]any)
	if len(methods) != 2 {
		t.Fatalf("ReadWriter methods = %d, want 2 (Read+Write)", len(methods))
	}
	embedded, _ := rw["detail"].(map[string]any)["embedded"].([]any)
	if len(embedded) != 1 {
		t.Fatalf("ReadWriter embedded = %d, want 1", len(embedded))
	}
}

// TestVarDeclPerSpecTypes (P1-V6): var (a int; b string) gives each spec
// its own type entry. Pre-fix, the whole node shared the last spec's type.
func TestVarDeclPerSpecTypes(t *testing.T) {
	src := `package main

func main() {
	var (
		a int
		b string
	)
	println(a, b)
}
`
	doc := emitDoc(t, src)
	vds := nodesOfKind(doc, "vardecl")
	if len(vds) != 1 {
		t.Fatalf("vardecl nodes = %d, want 1", len(vds))
	}
	specs, _ := nodeFields(vds[0])["specs"].([]any)
	if len(specs) != 2 {
		t.Fatalf("vardecl specs = %d, want 2", len(specs))
	}
	s0, _ := specs[0].(map[string]any)
	s1, _ := specs[1].(map[string]any)
	t0, _ := s0["type"].(int)
	t1, _ := s1["type"].(int)
	if t0 == t1 {
		t.Fatalf("both specs share type id %d (the P1-V6 bug)", t0)
	}
	// Pin the actual types: spec 0 is int, spec 1 is string.
	isBasic := func(id int, name string) bool {
		m, ok := doc.Types[id].(map[string]any)
		if !ok {
			return false
		}
		if k, _ := m["kind"].(string); k != "basic" {
			return false
		}
		n, _ := m["detail"].(map[string]any)["name"].(string)
		return n == name
	}
	if !isBasic(t0, "int") {
		t.Fatalf("spec 0 type is not int (id %d)", t0)
	}
	if !isBasic(t1, "string") {
		t.Fatalf("spec 1 type is not string (id %d)", t1)
	}
}

// TestMakeNewTypeArgs: builtin make/new take a *type* as their first
// argument. The type goes through the type table as a "typename" node —
// the type expression must not reach emitExpr's value switch (found while
// fixing P1-V7: make(chan int, 1) failed with "unsupported expr
// *ast.ChanType", contradicting the typeExprNodes matrix comment).
func TestMakeNewTypeArgs(t *testing.T) {
	src := `package main

func main() {
	ch := make(chan int, 1)
	m := make(map[string]int)
	s := make([]int, 0, 8)
	a := new([4]byte)
	p := new(int)
	m["k"] = 2
	s = append(s, 3)
	println(a[0], p == nil, len(ch), len(m), len(s))
}
`
	doc := emitDoc(t, src)
	calls := nodesOfKind(doc, "call")
	if len(calls) == 0 {
		t.Fatal("no call nodes emitted")
	}
	typenames := nodesOfKind(doc, "typename")
	// make x3 + new x2 = 5 typename nodes.
	if len(typenames) != 5 {
		t.Fatalf("typename nodes = %d, want 5 (make/new type args)", len(typenames))
	}
	for _, tn := range typenames {
		if _, ok := nodeFields(tn)["type"].(int); !ok {
			t.Fatalf("typename node has no type id: %#v", tn)
		}
	}
	// The builtin marker is still set on make/new calls.
	var makeCalls int
	for _, c := range calls {
		if b, _ := nodeFields(c)["builtin"].(string); b == "make" || b == "new" {
			makeCalls++
		}
	}
	if makeCalls != 5 {
		t.Fatalf("make/new builtin-marked calls = %d, want 5", makeCalls)
	}
}

// TestRangeNonIdentTargets (P1-V7): for counts[i], v = range nums emits
// the index target instead of dropping it to nil. Pre-fix, key was nil.
// (The := form with non-Ident targets is rejected by go/types itself —
// "cannot declare m[k]" — so only the = form is exercised here.)
func TestRangeNonIdentTargets(t *testing.T) {
	src := `package main

func main() {
	counts := []int{0, 0}
	nums := []string{"a", "b"}
	i := 0
	v := ""
	for counts[i], v = range nums {
		println(v)
		i++
	}
	s := struct{ F int }{}
	ch := make(chan int, 1)
	for s.F = range ch {
		println(s.F)
	}
	var w int
	for w = range ch {
		println(w)
	}
}
`
	doc := emitDoc(t, src)
	ranges := nodesOfKind(doc, "range")
	if len(ranges) != 3 {
		t.Fatalf("range nodes = %d, want 3", len(ranges))
	}
	// Classify by key node kind: index (counts[i]), selector (s.F), ident (w).
	byKey := map[string]map[string]any{}
	for _, r := range ranges {
		f := nodeFields(r)
		key, _ := f["key"].(map[string]any)
		if key == nil {
			t.Fatal("a range key is nil: target silently dropped (the P1-V7 bug)")
		}
		kk, _ := key["kind"].(string)
		byKey[kk] = r
	}
	// counts[i] target: emitted as an index node, not dropped.
	idxRange, ok := byKey["index"]
	if !ok {
		t.Fatal("no range with index key target (counts[i] dropped)")
	}
	if value, _ := nodeFields(idxRange)["value"].(map[string]any); value == nil {
		t.Fatal("index-key range value is nil")
	} else if k, _ := value["kind"].(string); k != "ident" {
		t.Fatalf("index-key range value kind = %q, want ident", k)
	}
	// s.F target: emitted as a selector node, value nil.
	selRange, ok := byKey["selector"]
	if !ok {
		t.Fatal("no range with selector key target (s.F dropped)")
	}
	if v := nodeFields(selRange)["value"]; v != nil {
		t.Fatalf("selector-key range value = %v, want nil", v)
	}
	// w target: plain ident, value nil.
	identRange, ok := byKey["ident"]
	if !ok {
		t.Fatal("no range with ident key target")
	}
	if v := nodeFields(identRange)["value"]; v != nil {
		t.Fatalf("ident-key range value = %v, want nil", v)
	}
}

// TestSelectorKinds (P1-V3): selectors carry kind/index/indirect/obj from
// go/types. Pre-fix, the Selection was discarded (_ = sel).
func TestSelectorKinds(t *testing.T) {
	src := `package main

type Inner struct{ X int }
type Outer struct {
	Inner
	Y int
}

func (o Outer) M() int { return o.X }

func main() {
	var o Outer
	p := &o
	_ = o.Y
	_ = o.X
	_ = p.X
	_ = o.M()
	f := Outer.M
	_ = f
	println(o.Y)
}
`
	doc := emitDoc(t, src)
	sels := nodesOfKind(doc, "selector")
	if len(sels) == 0 {
		t.Fatal("no selector nodes emitted")
	}
	type selInfo struct {
		kind     string
		index    []int
		indirect bool
	}
	seen := map[string][]selInfo{}
	for _, s := range sels {
		f := nodeFields(s)
		name, _ := f["sel"].(string)
		kind, _ := f["kind"].(string)
		var idx []int
		for _, ix := range f["index"].([]any) {
			idx = append(idx, ix.(int))
		}
		ind, _ := f["indirect"].(bool)
		seen[name] = append(seen[name], selInfo{kind, idx, ind})
		if _, ok := f["obj"].(int); !ok {
			t.Fatalf("selector %s has no obj id", name)
		}
		if kind != "field" && kind != "methodval" && kind != "methodexpr" {
			t.Fatalf("selector %s kind = %q, want field|methodval|methodexpr", name, kind)
		}
	}

	// o.Y: direct field, index [1].
	found := false
	for _, si := range seen["Y"] {
		if si.kind == "field" && len(si.index) == 1 && si.index[0] == 1 && !si.indirect {
			found = true
		}
	}
	if !found {
		t.Fatalf("no field selector Y with index [1]: %v", seen["Y"])
	}
	// o.X: promoted through embedded Inner, index [0,0].
	found = false
	for _, si := range seen["X"] {
		if si.kind == "field" && len(si.index) == 2 && si.index[0] == 0 && si.index[1] == 0 {
			found = true
		}
	}
	if !found {
		t.Fatalf("no field selector X with index [0,0] (embedding path): %v", seen["X"])
	}
	// p.X with p *Outer: indirect (implicit dereference).
	found = false
	for _, si := range seen["X"] {
		if si.kind == "field" && si.indirect {
			found = true
		}
	}
	if !found {
		t.Fatalf("no indirect field selector X: %v", seen["X"])
	}
	// o.M(): method value.
	found = false
	for _, si := range seen["M"] {
		if si.kind == "methodval" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no methodval selector M: %v", seen["M"])
	}
	// Outer.M: method expression.
	found = false
	for _, si := range seen["M"] {
		if si.kind == "methodexpr" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no methodexpr selector M: %v", seen["M"])
	}
}
