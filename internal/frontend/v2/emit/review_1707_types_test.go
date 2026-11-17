// Regression tests for the review-1707-p0-v2-types remediation lane
// (independent codebase review, 2026-11-17, P0-1).
//
// describeType silently misdescribed generics, aliases, unions, and type
// params: the default arm emitted {"kind":"basic","detail":{"name":...,
// "class":"unknown"}}, Named never read type args, Interface hardcoded
// empty methods/embedded, and struct fields hardcoded "offset": 0. The
// backend (GoKind.fromBasic) throws on class "unknown", so any v2 program
// using generics — or even `any`, which is an alias — failed at backend
// load, and the methods/offset entries were live lies on the wire.
// These tests pin the fixed shapes. Each fails on the pre-fix emitter:
// the Tag test fails because Tag[int] and Tag[string] emit identical
// named entries, the alias/union/typeparam tests fail on the
// basic/unknown entries, and the nil test pins the contextual-nil
// representation (no type id on the nil ident) instead of an error.
package emit

import (
	"reflect"
	"testing"
)

// typeDetail returns the "detail" map of a type-table entry.
func typeDetail(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	d, ok := m["detail"].(map[string]any)
	if !ok {
		t.Fatalf("type entry %v has no detail map", m)
	}
	return d
}

// typeByID returns the type-table entry with the given id.
func typeByID(t *testing.T, doc *Document, id int) map[string]any {
	t.Helper()
	if id < 0 || id >= len(doc.Types) {
		t.Fatalf("type id %d out of range (0..%d)", id, len(doc.Types)-1)
	}
	m, ok := doc.Types[id].(map[string]any)
	if !ok {
		t.Fatalf("type %d is not a map: %T", id, doc.Types[id])
	}
	return m
}

// intList converts a []any of ints (type ids in the document) to []int.
func intList(t *testing.T, v any) []int {
	t.Helper()
	l, ok := v.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", v)
	}
	out := make([]int, len(l))
	for i, e := range l {
		n, ok := e.(int)
		if !ok {
			t.Fatalf("element %d: expected int, got %T", i, e)
		}
		out[i] = n
	}
	return out
}

// namedEntries returns every named type-table entry with the given name.
func namedEntries(t *testing.T, doc *Document, name string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, tv := range doc.Types {
		m, ok := tv.(map[string]any)
		if !ok || m["kind"] != "named" {
			continue
		}
		if typeDetail(t, m)["name"] == name {
			out = append(out, m)
		}
	}
	return out
}

// typeArgsOf returns the "typeargs" type ids of a named entry.
func typeArgsOf(t *testing.T, m map[string]any) []int {
	t.Helper()
	return intList(t, typeDetail(t, m)["typeargs"])
}

// typeArgNames resolves a named entry's type args to their basic names
// (or kind for non-basic args).
func typeArgNames(t *testing.T, doc *Document, m map[string]any) []string {
	t.Helper()
	var out []string
	for _, id := range typeArgsOf(t, m) {
		e := typeByID(t, doc, id)
		d := typeDetail(t, e)
		if n, ok := d["name"].(string); ok {
			out = append(out, n)
		} else {
			out = append(out, e["kind"].(string))
		}
	}
	return out
}

// assertNoUnknownClass fails if any basic entry carries class "unknown":
// the backend throws on it, so it must never be emitted.
func assertNoUnknownClass(t *testing.T, doc *Document) {
	t.Helper()
	for i, tv := range doc.Types {
		m, ok := tv.(map[string]any)
		if !ok || m["kind"] != "basic" {
			continue
		}
		if typeDetail(t, m)["class"] == "unknown" {
			t.Errorf("type %d: basic entry with class \"unknown\": %v", i, m)
		}
	}
}

// resolveAlias follows alias entries to their right-hand side: a
// constraint (or any other type) recorded as `any` points at the alias
// entry, whose rhs is the empty interface.
func resolveAlias(t *testing.T, doc *Document, m map[string]any) map[string]any {
	t.Helper()
	for m["kind"] == "alias" {
		m = typeByID(t, doc, typeDetail(t, m)["rhs"].(int))
	}
	return m
}

// methodNames returns the names of the methods listed on a named or
// interface entry.
func methodNames(t *testing.T, m map[string]any) []string {
	t.Helper()
	ms, ok := typeDetail(t, m)["methods"].([]any)
	if !ok {
		t.Fatalf("entry %v has no methods list", m)
	}
	var out []string
	for _, e := range ms {
		em, ok := e.(map[string]any)
		if !ok {
			t.Fatalf("method entry is not a map: %T", e)
		}
		n, ok := em["name"].(string)
		if !ok {
			t.Fatalf("method entry has no name: %v", em)
		}
		out = append(out, n)
	}
	return out
}

// methodResultNames resolves each listed method's signature result types
// to their basic names (or kind for non-basic results).
func methodResultNames(t *testing.T, doc *Document, m map[string]any) map[string][]string {
	t.Helper()
	ms, ok := typeDetail(t, m)["methods"].([]any)
	if !ok {
		t.Fatalf("entry %v has no methods list", m)
	}
	out := map[string][]string{}
	for _, e := range ms {
		em := e.(map[string]any)
		sig := typeByID(t, doc, em["type"].(int))
		results, ok := typeDetail(t, sig)["results"].([]any)
		if !ok {
			t.Fatalf("signature %v has no results list", sig)
		}
		var names []string
		for _, r := range results {
			rm := r.(map[string]any)
			re := typeByID(t, doc, rm["type"].(int))
			rd := typeDetail(t, re)
			if n, ok := rd["name"].(string); ok {
				names = append(names, n)
			} else {
				names = append(names, re["kind"].(string))
			}
		}
		out[em["name"].(string)] = names
	}
	return out
}

const genericListSrc = `package main

type List[T any] struct { items []T }

func (l List[T]) Get(i int) T { return l.items[i] }

func Use() {
	var a List[int]
	var b List[string]
	_, _ = a, b
}
`

// TestGenericTagInstantiationDiffers is the collision proof: Tag's
// underlying struct never mentions T, so pre-fix Tag[int] and Tag[string]
// emitted byte-identical named entries (same name, same underlying,
// same empty methods). Post-fix the type args distinguish them.
func TestGenericTagInstantiationDiffers(t *testing.T) {
	doc := emitTestSrc(t, `package main

type Tag[T any] struct{ n int }

func Use() {
	var a Tag[int]
	var b Tag[string]
	_, _ = a, b
}
`)
	entries := namedEntries(t, doc, "main.Tag")
	if len(entries) != 2 {
		t.Fatalf("expected 2 named entries for main.Tag, got %d", len(entries))
	}
	if reflect.DeepEqual(entries[0], entries[1]) {
		t.Fatalf("Tag[int] and Tag[string] emit identical named entries: %v", entries[0])
	}
	names := map[string]bool{}
	for _, e := range entries {
		for _, n := range typeArgNames(t, doc, e) {
			names[n] = true
		}
	}
	if !names["int"] || !names["string"] {
		t.Errorf("expected type args int and string across Tag entries, got %v", names)
	}
	assertNoUnknownClass(t, doc)
}

// TestGenericNamedTypeArgsAndMethods pins the full generic named shape:
// the origin carries its type params, each instantiation carries its
// type args, the underlying struct is substituted per instantiation,
// and the declared methods are real (with substituted result types).
func TestGenericNamedTypeArgsAndMethods(t *testing.T) {
	doc := emitTestSrc(t, genericListSrc)
	entries := namedEntries(t, doc, "main.List")
	if len(entries) != 3 {
		t.Fatalf("expected 3 named entries for main.List (origin + 2 instantiations), got %d", len(entries))
	}
	var origin, instInt, instString map[string]any
	for _, e := range entries {
		args := typeArgNames(t, doc, e)
		switch {
		case len(args) == 1 && args[0] == "T":
			// The declaration form, reached via the method receiver:
			// go/types represents it as List[T], an instantiation
			// whose type arg is the type param itself.
			if origin != nil {
				t.Fatalf("two origin entries for main.List")
			}
			origin = e
		case len(args) == 1 && args[0] == "int":
			instInt = e
		case len(args) == 1 && args[0] == "string":
			instString = e
		default:
			t.Errorf("unexpected type args %v on entry %v", args, e)
		}
	}
	if origin == nil || instInt == nil || instString == nil {
		t.Fatalf("missing origin/int/string entries: origin=%v int=%v string=%v", origin != nil, instInt != nil, instString != nil)
	}
	if reflect.DeepEqual(instInt, instString) {
		t.Fatalf("List[int] and List[string] emit identical named entries: %v", instInt)
	}

	// Origin: one type param T constrained by any (an interface).
	tps, ok := typeDetail(t, origin)["typeparams"].([]any)
	if !ok || len(tps) != 1 {
		t.Fatalf("origin typeparams = %v, want 1 entry", typeDetail(t, origin)["typeparams"])
	}
	tp0 := tps[0].(map[string]any)
	if tp0["name"] != "T" {
		t.Errorf("origin type param name = %v, want T", tp0["name"])
	}
	tpEntry := resolveAlias(t, doc, typeByID(t, doc, tp0["constraint"].(int)))
	if tpEntry["kind"] != "interface" {
		t.Errorf("constraint of T = kind %v, want interface (any)", tpEntry["kind"])
	}

	// Origin methods: Get, returning the type param T.
	if got := methodNames(t, origin); !reflect.DeepEqual(got, []string{"Get"}) {
		t.Errorf("origin methods = %v, want [Get]", got)
	}
	if got := methodResultNames(t, doc, origin)["Get"]; !reflect.DeepEqual(got, []string{"T"}) {
		t.Errorf("origin Get results = %v, want [T]", got)
	}
	// Instantiated methods: Get returning the substituted arg.
	if got := methodResultNames(t, doc, instInt)["Get"]; !reflect.DeepEqual(got, []string{"int"}) {
		t.Errorf("List[int] Get results = %v, want [int]", got)
	}
	if got := methodResultNames(t, doc, instString)["Get"]; !reflect.DeepEqual(got, []string{"string"}) {
		t.Errorf("List[string] Get results = %v, want [string]", got)
	}

	// Underlying structs: origin items []T, instantiations []int/[]string.
	structFieldElem := func(e map[string]any) map[string]any {
		u := typeByID(t, doc, typeDetail(t, e)["underlying"].(int))
		if u["kind"] != "struct" {
			t.Fatalf("underlying is %v, want struct", u["kind"])
		}
		fields := typeDetail(t, u)["fields"].([]any)
		if len(fields) != 1 {
			t.Fatalf("struct fields = %d, want 1", len(fields))
		}
		f := fields[0].(map[string]any)
		if f["name"] != "items" {
			t.Fatalf("field name = %v, want items", f["name"])
		}
		slice := typeByID(t, doc, f["type"].(int))
		if slice["kind"] != "slice" {
			t.Fatalf("items type kind = %v, want slice", slice["kind"])
		}
		return typeByID(t, doc, typeDetail(t, slice)["elem"].(int))
	}
	if got := structFieldElem(origin); got["kind"] != "typeparam" {
		t.Errorf("origin items elem kind = %v, want typeparam", got["kind"])
	}
	if got := structFieldElem(instInt); typeDetail(t, got)["name"] != "int" {
		t.Errorf("List[int] items elem = %v, want int", got)
	}
	if got := structFieldElem(instString); typeDetail(t, got)["name"] != "string" {
		t.Errorf("List[string] items elem = %v, want string", got)
	}
	assertNoUnknownClass(t, doc)
}

// TestTypeParamDescribesConstraint pins the typeparam entry itself: the
// origin's struct field of type T resolves to a "typeparam" entry whose
// constraint is the interface `any` — not a basic/unknown.
func TestTypeParamDescribesConstraint(t *testing.T) {
	doc := emitTestSrc(t, genericListSrc)
	var found bool
	for _, tv := range doc.Types {
		m, ok := tv.(map[string]any)
		if !ok || m["kind"] != "typeparam" {
			continue
		}
		found = true
		d := typeDetail(t, m)
		if d["name"] != "T" {
			t.Errorf("typeparam name = %v, want T", d["name"])
		}
		c := resolveAlias(t, doc, typeByID(t, doc, d["constraint"].(int)))
		if c["kind"] != "interface" {
			t.Errorf("typeparam T constraint kind = %v, want interface", c["kind"])
		}
	}
	if !found {
		t.Fatalf("no typeparam entry in type table")
	}
	assertNoUnknownClass(t, doc)
}

// TestAliasDescribesRhs pins alias transparency: MyInt describes as an
// alias of int, and `any` — itself an alias for interface{} — describes
// as an alias of the empty interface, never as basic/unknown.
func TestAliasDescribesRhs(t *testing.T) {
	doc := emitTestSrc(t, `package main

type MyInt = int

func Use() {
	var c MyInt
	var f any
	_, _ = c, f
}
`)
	byName := map[string]map[string]any{}
	for _, tv := range doc.Types {
		m, ok := tv.(map[string]any)
		if !ok || m["kind"] != "alias" {
			continue
		}
		byName[typeDetail(t, m)["name"].(string)] = m
	}
	myInt, ok := byName["main.MyInt"]
	if !ok {
		t.Fatalf("no alias entry for main.MyInt; aliases: %v", byName)
	}
	if rhs := typeByID(t, doc, typeDetail(t, myInt)["rhs"].(int)); typeDetail(t, rhs)["name"] != "int" {
		t.Errorf("MyInt rhs = %v, want basic int", rhs)
	}
	anyAlias, ok := byName["any"]
	if !ok {
		t.Fatalf("no alias entry for any; aliases: %v", byName)
	}
	if rhs := typeByID(t, doc, typeDetail(t, anyAlias)["rhs"].(int)); rhs["kind"] != "interface" {
		t.Errorf("any rhs kind = %v, want interface", rhs["kind"])
	}
	assertNoUnknownClass(t, doc)
}

// TestUnionInterfaceDescribesTerms pins union fidelity: a constraint
// interface's embedded type set is a real union entry with its terms
// (type + tilde), reachable through the type param it constrains.
func TestUnionInterfaceDescribesTerms(t *testing.T) {
	doc := emitTestSrc(t, `package main

type C interface { ~int | ~string }

func Use[T C](x T) T { return x }

func main() {}
`)
	entries := namedEntries(t, doc, "main.C")
	if len(entries) != 1 {
		t.Fatalf("expected 1 named entry for main.C, got %d", len(entries))
	}
	iface := typeByID(t, doc, typeDetail(t, entries[0])["underlying"].(int))
	if iface["kind"] != "interface" {
		t.Fatalf("C underlying kind = %v, want interface", iface["kind"])
	}
	embedded := intList(t, typeDetail(t, iface)["embedded"])
	if len(embedded) != 1 {
		t.Fatalf("C embedded = %d entries, want 1 (the union)", len(embedded))
	}
	union := typeByID(t, doc, embedded[0])
	if union["kind"] != "union" {
		t.Fatalf("C embedded kind = %v, want union", union["kind"])
	}
	terms, ok := typeDetail(t, union)["terms"].([]any)
	if !ok || len(terms) != 2 {
		t.Fatalf("union terms = %v, want 2", typeDetail(t, union)["terms"])
	}
	var got []string
	for _, term := range terms {
		tm := term.(map[string]any)
		te := typeByID(t, doc, tm["type"].(int))
		tilde, _ := tm["tilde"].(bool)
		got = append(got, typeDetail(t, te)["name"].(string))
		if !tilde {
			t.Errorf("union term %v has tilde=false, want true", tm)
		}
	}
	if !reflect.DeepEqual(got, []string{"int", "string"}) {
		t.Errorf("union terms = %v, want [int string]", got)
	}
	// The type param T of Use is constrained by C.
	var found bool
	for _, tv := range doc.Types {
		m, ok := tv.(map[string]any)
		if !ok || m["kind"] != "typeparam" {
			continue
		}
		found = true
		c := typeByID(t, doc, typeDetail(t, m)["constraint"].(int))
		if c["kind"] != "named" || typeDetail(t, c)["name"] != "main.C" {
			t.Errorf("typeparam T constraint = %v, want named main.C", c)
		}
	}
	if !found {
		t.Fatalf("no typeparam entry for T")
	}
	assertNoUnknownClass(t, doc)
}

// TestInterfaceMethodsAndEmbeddedReal pins real interface content: the
// declared methods with their names, and the embedded interfaces as
// type ids — not the hardcoded empty lists.
func TestInterfaceMethodsAndEmbeddedReal(t *testing.T) {
	doc := emitTestSrc(t, `package main

type R interface { Read() int }
type N interface { M() int; R }

func Use() {
	var n N
	_ = n
}
`)
	ns := namedEntries(t, doc, "main.N")
	if len(ns) != 1 {
		t.Fatalf("expected 1 named entry for main.N, got %d", len(ns))
	}
	nIface := typeByID(t, doc, typeDetail(t, ns[0])["underlying"].(int))
	if got := methodNames(t, nIface); !reflect.DeepEqual(got, []string{"M"}) {
		t.Errorf("N methods = %v, want [M]", got)
	}
	embedded := intList(t, typeDetail(t, nIface)["embedded"])
	if len(embedded) != 1 {
		t.Fatalf("N embedded = %d entries, want 1 (R)", len(embedded))
	}
	rEntry := typeByID(t, doc, embedded[0])
	if rEntry["kind"] != "named" || typeDetail(t, rEntry)["name"] != "main.R" {
		t.Errorf("N embedded[0] = %v, want named main.R", rEntry)
	}
	rIface := typeByID(t, doc, typeDetail(t, rEntry)["underlying"].(int))
	if got := methodNames(t, rIface); !reflect.DeepEqual(got, []string{"Read"}) {
		t.Errorf("R methods = %v, want [Read]", got)
	}
	assertNoUnknownClass(t, doc)
}

// TestStructFieldsOmitOffset pins the removal of the hardcoded
// "offset": 0 lie: no v2 target layout is defined (the JVM layout is
// the backend's), so struct fields carry no offset until one is real.
func TestStructFieldsOmitOffset(t *testing.T) {
	doc := emitTestSrc(t, `package main

type Point struct { X int; Y string }

func Use() {
	var p Point
	_ = p
}
`)
	points := namedEntries(t, doc, "main.Point")
	if len(points) != 1 {
		t.Fatalf("expected 1 named entry for main.Point, got %d", len(points))
	}
	s := typeByID(t, doc, typeDetail(t, points[0])["underlying"].(int))
	if s["kind"] != "struct" {
		t.Fatalf("Point underlying kind = %v, want struct", s["kind"])
	}
	fields := typeDetail(t, s)["fields"].([]any)
	if len(fields) != 2 {
		t.Fatalf("Point fields = %d, want 2", len(fields))
	}
	for _, f := range fields {
		fm := f.(map[string]any)
		if _, ok := fm["offset"]; ok {
			t.Errorf("struct field %v carries offset %v (hardcoded lie)", fm["name"], fm["offset"])
		}
		if _, ok := fm["name"]; !ok {
			t.Errorf("struct field has no name: %v", fm)
		}
		if _, ok := fm["type"]; !ok {
			t.Errorf("struct field has no type: %v", fm)
		}
	}
}

// TestUntypedNilHasNoTypeID pins the nil literal's representation: its
// type is untyped nil, which has no wire entry — the type is contextual
// (*int in `var p *int = nil`, interface{} in F(nil)) and the object
// table already says kind "nil". The ident node therefore carries no
// type id, and the type table contains no basic/unknown. (The old
// emitter described it as basic/unknown, which the backend could only
// crash on; failing Emit instead would reject `p == nil`, so the
// absence of a claim — not an error — is the honest representation.)
func TestUntypedNilHasNoTypeID(t *testing.T) {
	doc := emitTestSrc(t, `package main

func F(x any) {}

func main() {
	var p *int = (nil)
	_ = p
	F(nil)
	if p == nil {
		println(1)
	}
}
`)
	// Every nil ident node has a null type.
	idents := nodesOf(t, doc, "ident")
	var nils int
	for _, n := range idents {
		if fieldsOf(n)["name"] == "nil" {
			nils++
			if fieldsOf(n)["type"] != nil {
				t.Errorf("nil ident has type %v, want null (no type id)", fieldsOf(n)["type"])
			}
		}
	}
	if nils != 3 {
		t.Fatalf("nil ident nodes = %d, want 3 (parenthesised, F(nil), p == nil)", nils)
	}
	assertNoUnknownClass(t, doc)
}

// TestTupleCallTypeDescribesElems pins the tuple entry: go/types gives
// a call expression the tuple of its results — the empty tuple for a
// void call, the tuple for a multi-result call. Both must be real
// entries (the old default arm misdescribed them as basic/unknown,
// which crashed any void or multi-result call at backend load).
func TestTupleCallTypeDescribesElems(t *testing.T) {
	doc := emitTestSrc(t, `package main

func F() (int, string) { return 1, "x" }
func G() {}

func main() {
	a, b := F()
	_, _ = a, b
	G()
}
`)
	var tuples []map[string]any
	for _, tv := range doc.Types {
		m, ok := tv.(map[string]any)
		if ok && m["kind"] == "tuple" {
			tuples = append(tuples, m)
		}
	}
	if len(tuples) != 2 {
		t.Fatalf("expected 2 tuple entries (F's results, G's void), got %d: %v", len(tuples), doc.Types)
	}
	var void, multi map[string]any
	for _, tu := range tuples {
		elems := intList(t, typeDetail(t, tu)["elems"])
		switch len(elems) {
		case 0:
			void = tu
		case 2:
			multi = tu
		default:
			t.Errorf("unexpected tuple arity %d: %v", len(elems), tu)
		}
	}
	if void == nil {
		t.Errorf("no empty-tuple entry for the void call G()")
	}
	if multi == nil {
		t.Fatalf("no 2-tuple entry for the multi-result call F()")
	}
	elems := intList(t, typeDetail(t, multi)["elems"])
	var got []string
	for _, id := range elems {
		got = append(got, typeDetail(t, typeByID(t, doc, id))["name"].(string))
	}
	if !reflect.DeepEqual(got, []string{"int", "string"}) {
		t.Errorf("F() tuple elems = %v, want [int string]", got)
	}
	assertNoUnknownClass(t, doc)
}
