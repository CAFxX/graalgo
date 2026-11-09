// Regression tests for the review-20-e-1 remediation lane
// (independent codebase review REVIEW-2026-11-09-20, slice E).
//
// describeType silently dropped data at four sites: Named types emitted
// "methods": [] unconditionally, interfaces emitted empty methods/embedded
// while honestly reporting empty:false, struct fields carried a hardcoded
// offset 0, and signatures recorded neither TypeParams nor Recv. Each
// test below fails on the pre-fix emitter (verified by running them
// against master@a0df481b) and passes after the fix.
package emit

import (
	"go/token"
	"go/types"
	"testing"
)

// namedTypeDetail returns the detail map of the named type with the
// given (unqualified) name, or fails the test.
func namedTypeDetail(t *testing.T, doc *Document, name string) map[string]any {
	t.Helper()
	for _, tv := range doc.Types {
		m, ok := tv.(map[string]any)
		if !ok || m["kind"] != "named" {
			continue
		}
		d, ok := m["detail"].(map[string]any)
		if !ok {
			continue
		}
		if n, _ := d["name"].(string); n == name || len(n) > len(name)+1 && n[len(n)-len(name)-1:] == "."+name {
			return d
		}
	}
	t.Fatalf("no named type %q in type table (%d entries)", name, len(doc.Types))
	return nil
}

// typeEntry returns the type-table entry with the given id.
func typeEntry(t *testing.T, doc *Document, id int) map[string]any {
	t.Helper()
	if id < 0 || id >= len(doc.Types) {
		t.Fatalf("type id %d out of range (table has %d entries)", id, len(doc.Types))
	}
	m, ok := doc.Types[id].(map[string]any)
	if !ok {
		t.Fatalf("type id %d is not a map: %#v", id, doc.Types[id])
	}
	return m
}

// detailOf returns the detail map of the type-table entry with the given id.
func detailOf(t *testing.T, doc *Document, id int) map[string]any {
	t.Helper()
	d, ok := typeEntry(t, doc, id)["detail"].(map[string]any)
	if !ok {
		t.Fatalf("type id %d has no detail map: %#v", id, doc.Types[id])
	}
	return d
}

// methodNames returns the set of method names recorded in a methods list.
func methodNames(t *testing.T, methods any) map[string]map[string]any {
	t.Helper()
	list, ok := methods.([]any)
	if !ok {
		t.Fatalf("methods is not a list: %#v", methods)
	}
	out := map[string]map[string]any{}
	for _, m := range list {
		mm, ok := m.(map[string]any)
		if !ok {
			t.Fatalf("method entry is not a map: %#v", m)
		}
		name, _ := mm["name"].(string)
		if name == "" {
			t.Fatalf("method entry has no name: %#v", mm)
		}
		out[name] = mm
	}
	return out
}

// funcSignatureID returns the type-table id of the signature of the
// function object with the given name.
func funcSignatureID(t *testing.T, doc *Document, name string) int {
	t.Helper()
	for _, o := range doc.Objects {
		m, ok := o.(map[string]any)
		if !ok || m["kind"] != "func" || m["name"] != name {
			continue
		}
		id, ok := m["type"].(int)
		if !ok {
			t.Fatalf("func %s has no int type id: %#v", name, m)
		}
		return id
	}
	t.Fatalf("no func object %q", name)
	return -1
}

// TestNamedTypeMethodsRoundTrip: a named type with methods round-trips
// its method set. Before the fix every named type emitted methods: [].
func TestNamedTypeMethodsRoundTrip(t *testing.T) {
	doc := emitTestSrc(t, `package main

type Counter struct{ N int }

func (c Counter) Inc()       { c.N++ }
func (c Counter) Value() int { return c.N }
func (c *Counter) Reset()    { c.N = 0 }

func main() {
	c := Counter{N: 1}
	c.Inc()
	c.Reset()
	println(c.Value())
}
`)
	d := namedTypeDetail(t, doc, "Counter")
	methods := methodNames(t, d["methods"])
	if len(methods) != 3 {
		t.Fatalf("Counter methods = %v, want Inc, Value, Reset", keys(methods))
	}
	for _, want := range []string{"Inc", "Value", "Reset"} {
		m, ok := methods[want]
		if !ok {
			t.Errorf("Counter methods lack %s: %v", want, keys(methods))
			continue
		}
		sigID, ok := m["type"].(int)
		if !ok {
			t.Errorf("method %s has no int signature type id: %#v", want, m)
			continue
		}
		sig := typeEntry(t, doc, sigID)
		if sig["kind"] != "signature" {
			t.Errorf("method %s type id %d is kind %v, want signature", want, sigID, sig["kind"])
		}
	}
	// The method signatures carry their receiver, pointing back at Counter.
	incSig := detailOf(t, doc, methodNames(t, d["methods"])["Inc"]["type"].(int))
	recv, ok := incSig["recv"].(map[string]any)
	if !ok {
		t.Fatalf("Inc signature recv = %#v, want a receiver record", incSig["recv"])
	}
	if recv["name"] != "c" {
		t.Errorf("Inc recv name = %v, want c", recv["name"])
	}
	recvType, ok := recv["type"].(int)
	if !ok {
		t.Fatalf("Inc recv has no int type id: %#v", recv)
	}
	if got := typeEntry(t, doc, recvType)["kind"]; got != "named" {
		t.Errorf("Inc recv type id %d is kind %v, want named (Counter)", recvType, got)
	}
	// Value returns one int.
	valSig := detailOf(t, doc, methodNames(t, d["methods"])["Value"]["type"].(int))
	results, _ := valSig["results"].([]any)
	if len(results) != 1 {
		t.Errorf("Value signature results = %d, want 1", len(results))
	}
}

func keys(m map[string]map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// asInt64 normalizes the integer representations a detail map may
// carry (int for a literal 0, int64 for a computed offset).
func asInt64(t *testing.T, v any) int64 {
	t.Helper()
	switch n := v.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case int32:
		return int64(n)
	default:
		t.Fatalf("value %#v (type %T) is not an integer", v, v)
		return 0
	}
}

// TestInterfaceMethodsAndEmbeddedRoundTrip: a non-empty interface
// round-trips its methods and its embedded types. Before the fix every
// interface emitted methods: [] and embedded: [] while honestly
// reporting empty: false.
func TestInterfaceMethodsAndEmbeddedRoundTrip(t *testing.T) {
	doc := emitTestSrc(t, `package main

type Reader interface {
	Read(p []byte) (n int, err error)
}

type ReadWriter interface {
	Reader
	Write(p []byte) (n int, err error)
}

var r Reader
var rw ReadWriter

func main() {
	println(r == nil, rw == nil)
}
`)
	// Reader: one method, no embedded types, not empty.
	rd := namedTypeDetail(t, doc, "Reader")
	rIface := detailOf(t, doc, rd["underlying"].(int))
	if rIface["empty"] != false {
		t.Errorf("Reader empty = %v, want false", rIface["empty"])
	}
	rMethods := methodNames(t, rIface["methods"])
	if len(rMethods) != 1 || rMethods["Read"] == nil {
		t.Errorf("Reader methods = %v, want [Read]", keys(rMethods))
	}
	if emb, _ := rIface["embedded"].([]any); len(emb) != 0 {
		t.Errorf("Reader embedded = %v, want []", emb)
	}
	// ReadWriter: the complete method set (NumMethods) — Write declared
	// here plus Read from the embedded Reader — and the embedded
	// Reader itself, so the consumer sees both the dispatch set and
	// where each method comes from.
	rwd := namedTypeDetail(t, doc, "ReadWriter")
	rwIface := detailOf(t, doc, rwd["underlying"].(int))
	if rwIface["empty"] != false {
		t.Errorf("ReadWriter empty = %v, want false", rwIface["empty"])
	}
	rwMethods := methodNames(t, rwIface["methods"])
	if len(rwMethods) != 2 || rwMethods["Write"] == nil || rwMethods["Read"] == nil {
		t.Errorf("ReadWriter methods = %v, want [Read Write]", keys(rwMethods))
	}
	emb, ok := rwIface["embedded"].([]any)
	if !ok || len(emb) != 1 {
		t.Fatalf("ReadWriter embedded = %#v, want 1 entry (Reader)", rwIface["embedded"])
	}
	embID, ok := emb[0].(int)
	if !ok {
		t.Fatalf("ReadWriter embedded[0] is not a type id: %#v", emb[0])
	}
	embType := typeEntry(t, doc, embID)
	if embType["kind"] != "named" {
		t.Errorf("ReadWriter embedded type is kind %v, want named", embType["kind"])
	} else if d, _ := embType["detail"].(map[string]any); d["name"] != "main.Reader" {
		t.Errorf("ReadWriter embedded type name = %v, want main.Reader", d["name"])
	}
	// The Read method signature records its parameter and results.
	readSig := detailOf(t, doc, rMethods["Read"]["type"].(int))
	params, _ := readSig["params"].([]any)
	if len(params) != 1 {
		t.Errorf("Read params = %d, want 1", len(params))
	}
	results, _ := readSig["results"].([]any)
	if len(results) != 2 {
		t.Errorf("Read results = %d, want 2", len(results))
	}
	// An interface method's signature carries a receiver after all:
	// go/types gives it an unnamed receiver var whose type is the
	// interface's named type (main.Reader here), not nil.
	recv, ok := readSig["recv"].(map[string]any)
	if !ok {
		t.Fatalf("Read recv = %#v, want an unnamed receiver record", readSig["recv"])
	}
	if recv["name"] != "" {
		t.Errorf("Read recv name = %v, want \"\" (unnamed)", recv["name"])
	}
	recvID, ok := recv["type"].(int)
	if !ok {
		t.Fatalf("Read recv has no int type id: %#v", recv)
	}
	recvType := typeEntry(t, doc, recvID)
	if recvType["kind"] != "named" {
		t.Errorf("Read recv type id %d is kind %v, want named (main.Reader)", recvID, recvType["kind"])
	} else if d, _ := recvType["detail"].(map[string]any); d["name"] != "main.Reader" {
		t.Errorf("Read recv type name = %v, want main.Reader", d["name"])
	}
}

// TestStructOffsetsMatchGoTypesSizes: struct field offsets match
// go/types sizes for the gc/amd64 target. Before the fix every field
// carried offset 0. The layout (bool, int64, bool) pins padding: the
// second bool must sit at offset 16, not 9.
func TestStructOffsetsMatchGoTypesSizes(t *testing.T) {
	doc := emitTestSrc(t, `package main

type Padded struct {
	A bool
	B int64
	C bool
}

type Plain struct {
	X int
	Y string
	Z bool
}

func main() {
	p := Padded{A: true, B: 2, C: false}
	q := Plain{X: 1, Y: "s", Z: true}
	println(p.B, q.X, q.Y, q.Z)
}
`)
	sizes := types.SizesFor("gc", "amd64")
	if sizes == nil {
		t.Fatal("no gc/amd64 sizes")
	}
	for _, tc := range []struct {
		name   string
		fields []struct {
			name string
			typ  types.Type
		}
	}{
		{"Padded", []struct {
			name string
			typ  types.Type
		}{
			{"A", types.Typ[types.Bool]},
			{"B", types.Typ[types.Int64]},
			{"C", types.Typ[types.Bool]},
		}},
		{"Plain", []struct {
			name string
			typ  types.Type
		}{
			{"X", types.Typ[types.Int]},
			{"Y", types.Typ[types.String]},
			{"Z", types.Typ[types.Bool]},
		}},
	} {
		vars := make([]*types.Var, len(tc.fields))
		for i, f := range tc.fields {
			vars[i] = types.NewVar(token.NoPos, nil, f.name, f.typ)
		}
		want := sizes.Offsetsof(vars)
		d := namedTypeDetail(t, doc, tc.name)
		st := detailOf(t, doc, d["underlying"].(int))
		fields, ok := st["fields"].([]any)
		if !ok || len(fields) != len(tc.fields) {
			t.Fatalf("%s fields = %#v, want %d fields", tc.name, st["fields"], len(tc.fields))
		}
		for i, fv := range fields {
			fm, ok := fv.(map[string]any)
			if !ok {
				t.Fatalf("%s field %d is not a map: %#v", tc.name, i, fv)
			}
			if fm["name"] != tc.fields[i].name {
				t.Errorf("%s field %d name = %v, want %s", tc.name, i, fm["name"], tc.fields[i].name)
			}
			off := asInt64(t, fm["offset"])
			if off != want[i] {
				t.Errorf("%s field %s offset = %d, want %d (go/types gc/amd64)", tc.name, tc.fields[i].name, off, want[i])
			}
		}
	}
}

// TestGenericFuncSignatureCarriesTypeParams: a generic function's
// signature carries its type parameters (names + constraint type ids)
// and a nil receiver; a method on a generic type carries its receiver.
// Before the fix both were silently dropped from the type table.
func TestGenericFuncSignatureCarriesTypeParams(t *testing.T) {
	doc := emitTestSrc(t, `package main

func Map[T any](x T) T { return x }

func Pair[K comparable, V any](k K, v V) (K, V) { return k, v }

// Stringify pins a non-alias constraint: an explicit interface, so the
// recorded constraint type id must resolve to an interface with String.
func Stringify[T interface{ String() string }](x T) string { return x.String() }

type Stack[T any] struct{ items []T }

func (s Stack[T]) Push(x T) { s.items = append(s.items, x) }

var ints Stack[int]

func main() {
	// No generic instantiations in expression position here:
	// IndexListExpr calls (Map[int](1)) are a separate, loudly-rejected
	// gap (R20-E-17). The declarations above are enough: every top-level
	// func is declared into the object table, so its signature — with
	// type params — lands in the type table.
	println(1)
	ints.Push(3)
}
`)
	// Map[T any]: one type param named T, constraint the empty interface.
	mapSig := detailOf(t, doc, funcSignatureID(t, doc, "Map"))
	tps, ok := mapSig["typeparams"].([]any)
	if !ok || len(tps) != 1 {
		t.Fatalf("Map typeparams = %#v, want 1 entry", mapSig["typeparams"])
	}
	tp0, ok := tps[0].(map[string]any)
	if !ok {
		t.Fatalf("Map typeparams[0] is not a map: %#v", tps[0])
	}
	if tp0["name"] != "T" {
		t.Errorf("Map type param name = %v, want T", tp0["name"])
	}
	// The constraint id is recorded (an int into the type table); its
	// kind is NOT asserted here: `any` is an alias, and aliases reach
	// the R17-P1-A default case (basic/unknown) — that lane's
	// territory, not this one's. The Stringify case below pins a real
	// interface constraint instead.
	if _, ok := tp0["constraint"].(int); !ok {
		t.Fatalf("Map type param has no int constraint id: %#v", tp0)
	}
	if mapSig["recv"] != nil {
		t.Errorf("Map recv = %v, want nil (plain func)", mapSig["recv"])
	}
	// Stringify[T interface{ String() string }]: the constraint is a
	// real interface, so its type id must resolve to one with String.
	strSig := detailOf(t, doc, funcSignatureID(t, doc, "Stringify"))
	stps, ok := strSig["typeparams"].([]any)
	if !ok || len(stps) != 1 {
		t.Fatalf("Stringify typeparams = %#v, want 1 entry", strSig["typeparams"])
	}
	scID, ok := stps[0].(map[string]any)["constraint"].(int)
	if !ok {
		t.Fatalf("Stringify type param has no int constraint id: %#v", stps[0])
	}
	scEntry := typeEntry(t, doc, scID)
	if scEntry["kind"] != "interface" {
		t.Fatalf("Stringify constraint type id %d is kind %v, want interface", scID, scEntry["kind"])
	}
	scMethods := methodNames(t, detailOf(t, doc, scID)["methods"])
	if len(scMethods) != 1 || scMethods["String"] == nil {
		t.Errorf("Stringify constraint methods = %v, want [String]", keys(scMethods))
	}
	// Pair[K comparable, V any]: two type params in order.
	pairSig := detailOf(t, doc, funcSignatureID(t, doc, "Pair"))
	tps, ok = pairSig["typeparams"].([]any)
	if !ok || len(tps) != 2 {
		t.Fatalf("Pair typeparams = %#v, want 2 entries", pairSig["typeparams"])
	}
	if tps[0].(map[string]any)["name"] != "K" || tps[1].(map[string]any)["name"] != "V" {
		t.Errorf("Pair type param names = %v/%v, want K/V", tps[0].(map[string]any)["name"], tps[1].(map[string]any)["name"])
	}
	// Stack[T any]: the named type records its type param and its method.
	stack := namedTypeDetail(t, doc, "Stack")
	stkps, ok := stack["typeparams"].([]any)
	if !ok || len(stkps) != 1 {
		t.Fatalf("Stack typeparams = %#v, want 1 entry", stack["typeparams"])
	}
	if stkps[0].(map[string]any)["name"] != "T" {
		t.Errorf("Stack type param name = %v, want T", stkps[0].(map[string]any)["name"])
	}
	sMethods := methodNames(t, stack["methods"])
	push, ok := sMethods["Push"]
	if !ok {
		t.Fatalf("Stack methods = %v, want Push", keys(sMethods))
	}
	pushSig := detailOf(t, doc, push["type"].(int))
	recv, ok := pushSig["recv"].(map[string]any)
	if !ok {
		t.Fatalf("Push signature recv = %#v, want a receiver record", pushSig["recv"])
	}
	if recv["name"] != "s" {
		t.Errorf("Push recv name = %v, want s", recv["name"])
	}
	// The instantiated Stack[int] records its type argument.
	found := false
	for _, tv := range doc.Types {
		m, ok := tv.(map[string]any)
		if !ok || m["kind"] != "named" {
			continue
		}
		d, _ := m["detail"].(map[string]any)
		if d == nil || d["name"] != "main.Stack" {
			continue
		}
		args, _ := d["typeargs"].([]any)
		if len(args) != 1 {
			continue
		}
		argID, ok := args[0].(int)
		if !ok {
			t.Fatalf("Stack typeargs[0] is not a type id: %#v", args[0])
		}
		// The origin Stack[T] is also a named entry with one type arg
		// — its own parameter T. Only the instantiation carries int.
		if got := detailOf(t, doc, argID)["name"]; got != "int" {
			continue
		}
		found = true
	}
	if !found {
		t.Errorf("no instantiated Stack[int] named type with 1 type arg in the table")
	}
}
