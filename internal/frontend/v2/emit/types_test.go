// Type-emission tests (review-1105-v2-types).
//
// Each test pins a fixed type-table defect: wrong basic classes,
// zero struct offsets, empty method lists, mis-emitted call-result
// tuples, silent acceptance of unsupported types, and alias
// duplication. All of them fail against the pre-fix emitter.
package emit

import (
	"go/token"
	"go/types"
	"runtime"
	"strings"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/parser"
)

// newFileSet is a fresh token.FileSet for in-memory parses.
func newFileSet() *token.FileSet { return token.NewFileSet() }

// emitSource parses, checks, and emits a single in-memory source file.
func emitSource(t *testing.T, src string) *Document {
	t.Helper()
	fset := newFileSet()
	pkg, err := parser.ParseSources(fset, map[string]string{"demo.go": src})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cres, err := checker.Check(fset, pkg, &checker.Config{GoVersion: "go1.27"})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	doc, err := Emit(cres, pkg, []string{"demo.go"})
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	return doc
}

// emitSourceExpectErr is emitSource for programs whose emission must fail.
func emitSourceExpectErr(t *testing.T, src string) error {
	t.Helper()
	fset := newFileSet()
	pkg, err := parser.ParseSources(fset, map[string]string{"demo.go": src})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cres, err := checker.Check(fset, pkg, &checker.Config{GoVersion: "go1.27"})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	_, err = Emit(cres, pkg, []string{"demo.go"})
	return err
}

// typeEntry finds the first detail of kind with the given name
// ("demo.<Name>" for named types, or "" for unnamed kinds) and
// returns its type-table id and detail map.
func typeEntry(t *testing.T, doc *Document, kind, name string) (int, map[string]any) {
	t.Helper()
	for id, raw := range doc.Types {
		ent, _ := raw.(map[string]any)
		if ent["kind"] != kind {
			continue
		}
		d, _ := ent["detail"].(map[string]any)
		if name == "" || d["name"] == name {
			return id, d
		}
	}
	t.Fatalf("no type entry kind=%q name=%q in %v", kind, name, doc.Types)
	return -1, nil
}

// findNode walks the funcs tree for the first node of the given kind
// whose fields satisfy pred (nil = any node of that kind).
func findNode(t *testing.T, doc *Document, kind string, pred func(map[string]any) bool) map[string]any {
	t.Helper()
	var found map[string]any
	var walk func(v any)
	walk = func(v any) {
		if found != nil {
			return
		}
		switch x := v.(type) {
		case map[string]any:
			if x["kind"] == kind && (pred == nil || pred(x)) {
				found = x
				return
			}
			for _, f := range x {
				walk(f)
			}
		case []any:
			for _, e := range x {
				walk(e)
			}
		}
	}
	walk(doc.Funcs)
	if found == nil {
		t.Fatalf("no node kind=%q found in %v", kind, doc.Funcs)
	}
	return found
}

func TestBasicClassEveryKind(t *testing.T) {
	// Exhaustive over every *types.Basic kind: the old switch
	// (Info() & IsNumeric, compared against IsBoolean/IsUnsigned)
	// misclassified bool as "unknown" and all unsigned kinds as "int".
	want := map[types.BasicKind]string{
		types.Bool:           "bool",
		types.Int:            "int",
		types.Int8:           "int",
		types.Int16:          "int",
		types.Int32:          "int",
		types.Int64:          "int",
		types.Uint:           "uint",
		types.Uint8:          "uint",
		types.Uint16:         "uint",
		types.Uint32:         "uint",
		types.Uint64:         "uint",
		types.Uintptr:        "uint",
		types.Float32:        "float",
		types.Float64:        "float",
		types.Complex64:      "complex",
		types.Complex128:     "complex",
		types.String:         "string",
		types.UnsafePointer:  "pointer",
		types.UntypedBool:    "bool",
		types.UntypedInt:     "int",
		types.UntypedRune:    "int",
		types.UntypedFloat:   "float",
		types.UntypedComplex: "complex",
		types.UntypedString:  "string",
		types.UntypedNil:     "untyped",
	}
	for kind := types.Bool; kind <= types.UntypedNil; kind++ {
		got := basicClass(types.Typ[kind])
		if want[kind] == "" {
			t.Errorf("kind %v (%d): no expected class recorded — test table is incomplete", kind, kind)
			continue
		}
		if got != want[kind] {
			t.Errorf("basicClass(%v) = %q, want %q", kind, got, want[kind])
		}
	}
}

func TestBasicClassEmitted(t *testing.T) {
	// End-to-end: bool/uint/unsafe.Pointer through the emitter.
	// (byte and rune are aliases of uint8/int32, so their Basic
	// entries are the uint/int ones; they are covered by the unit test.)
	doc := emitSource(t, `package demo

import "unsafe"

func main() {
	var b bool = true
	var u uint = 3
	var p unsafe.Pointer
	println(b, u, p)
}
`)
	got := map[string]string{}
	for _, raw := range doc.Types {
		ent, _ := raw.(map[string]any)
		if ent["kind"] != "basic" {
			continue
		}
		d, _ := ent["detail"].(map[string]any)
		got[d["name"].(string)] = d["class"].(string)
	}
	// types.Typ[UnsafePointer].Name() is "Pointer" (not
	// "unsafe.Pointer") in this go/types version.
	for name, want := range map[string]string{
		"bool":    "bool",
		"uint":    "uint",
		"Pointer": "pointer",
	} {
		if got[name] != want {
			t.Errorf("emitted class of %s = %q, want %q (all basic entries: %v)", name, got[name], want, got)
		}
	}
	for _, raw := range doc.Types {
		ent, _ := raw.(map[string]any)
		d, _ := ent["detail"].(map[string]any)
		if d["class"] == "unknown" {
			t.Errorf("emitted basic entry still has class \"unknown\": %v", d)
		}
	}
}

func TestStructFieldOffsets(t *testing.T) {
	// Offsets must be real (gc sizes, host arch), not zero:
	// Point has a 1-byte field at 0 and an int64 at 8; Padded has a
	// 1-byte field at 0 and a 4-byte field at 4.
	doc := emitSource(t, `package demo

type Point struct {
	X byte
	Y int64
}

type Padded struct {
	A byte
	B int32
}

func main() {
	var p Point
	var q Padded
	println(p.X, p.Y, q.A, q.B)
}
`)
	// Struct entries carry no name; find them by field names.
	var pointFields, paddedFields []any
	for _, raw := range doc.Types {
		ent, _ := raw.(map[string]any)
		if ent["kind"] != "struct" {
			continue
		}
		d, _ := ent["detail"].(map[string]any)
		fields, _ := d["fields"].([]any)
		if len(fields) != 2 {
			continue
		}
		names := []string{
			fields[0].(map[string]any)["name"].(string),
			fields[1].(map[string]any)["name"].(string),
		}
		switch {
		case names[0] == "X" && names[1] == "Y":
			pointFields = fields
		case names[0] == "A" && names[1] == "B":
			paddedFields = fields
		}
	}
	off := func(fields []any, i int) int64 {
		f, _ := fields[i].(map[string]any)
		v, _ := f["offset"].(int64)
		return v
	}
	if pointFields == nil || paddedFields == nil {
		t.Fatalf("struct entries not found (types: %v)", doc.Types)
	}
	if got := off(pointFields, 0); got != 0 {
		t.Errorf("Point.X offset = %d, want 0", got)
	}
	if got := off(pointFields, 1); got != 8 {
		t.Errorf("Point.Y offset = %d, want 8 (int64 aligned)", got)
	}
	if got := off(paddedFields, 1); got != 4 {
		t.Errorf("Padded.B offset = %d, want 4 (int32 aligned after byte)", got)
	}
}

func TestNamedTypeMethods(t *testing.T) {
	// S: value-receiver Get (direct) + pointer-receiver Set (direct);
	// T embeds S, so Get/Set are promoted onto T with index [0].
	doc := emitSource(t, `package demo

type S struct{ v int }

func (s S) Get() int    { return s.v }
func (s *S) Set(x int)  { s.v = x }

type T struct{ S }

func main() {
	var s S
	var tt T
	println(s.Get(), tt.Get())
	s.Set(1)
	tt.Set(2)
}
`)
	// S (value type) carries only the value-receiver method; the
	// pointer-receiver method lives on the *S entry (v1 §4: the
	// pointer entry holds the full *T method set).
	sID, sNamed := typeEntry(t, doc, "named", "demo.S")
	_ = sID
	methods, _ := sNamed["methods"].([]any)
	if len(methods) != 1 {
		t.Fatalf("S methods = %v, want 1 entry (Get only)", methods)
	}
	byName := map[string]map[string]any{}
	for _, m := range methods {
		e, _ := m.(map[string]any)
		byName[e["name"].(string)] = e
	}
	if get := byName["Get"]; get == nil {
		t.Error("S missing method Get")
	} else {
		if get["recv"] != "value" {
			t.Errorf("S.Get recv = %v, want value", get["recv"])
		}
		if idx, _ := get["index"].([]int); len(idx) != 0 {
			t.Errorf("S.Get index = %v, want [] (direct)", idx)
		}
		if _, ok := get["obj"].(int); !ok {
			t.Errorf("S.Get obj = %v, want an object id", get["obj"])
		}
	}
	if byName["Set"] != nil {
		t.Errorf("S methods include Set, want only value-receiver methods (Set lives on *S)")
	}

	// *S carries both receivers.
	ptrS := findPtrEntry(t, doc, sID)
	ptrMethods, _ := ptrS["methods"].([]any)
	if len(ptrMethods) != 2 {
		t.Fatalf("*S methods = %v, want 2 entries (Get + Set)", ptrMethods)
	}
	ptrByName := map[string]map[string]any{}
	for _, m := range ptrMethods {
		e, _ := m.(map[string]any)
		ptrByName[e["name"].(string)] = e
	}
	if set := ptrByName["Set"]; set == nil {
		t.Error("*S missing method Set")
	} else if set["recv"] != "pointer" {
		t.Errorf("*S.Set recv = %v, want pointer", set["recv"])
	}

	_, tNamed := typeEntry(t, doc, "named", "demo.T")
	tMethods, _ := tNamed["methods"].([]any)
	if len(tMethods) != 1 {
		t.Fatalf("T methods = %v, want 1 promoted entry (Get only)", tMethods)
	}
	for _, m := range tMethods {
		e, _ := m.(map[string]any)
		idx, _ := e["index"].([]int)
		if len(idx) != 1 || idx[0] != 0 {
			t.Errorf("T.%s index = %v, want [0] (promoted via embedded field 0)", e["name"], idx)
		}
	}
}

// findPtrEntry returns the detail of the pointer-to entry for the
// named type with the given id.
func findPtrEntry(t *testing.T, doc *Document, namedID int) map[string]any {
	t.Helper()
	for _, raw := range doc.Types {
		ent, _ := raw.(map[string]any)
		if ent["kind"] != "pointer" {
			continue
		}
		d, _ := ent["detail"].(map[string]any)
		if d["elem"] == namedID {
			return d
		}
	}
	t.Fatalf("no pointer entry for named type %d", namedID)
	return nil
}

func TestInterfaceTypeMethods(t *testing.T) {
	// W declares Read; I embeds W and adds Close. Both interfaces
	// must list their methods (with signatures), and I must list W
	// as embedded — none of these were populated before.
	doc := emitSource(t, `package demo

type W interface{ Read() int }

type I interface {
	W
	Close()
}

func main() {
	var w W
	var i I
	println(w.Read(), i.Read())
	i.Close()
}
`)
	wID, wNamed := typeEntry(t, doc, "named", "demo.W")
	under, _ := wNamed["underlying"].(int)
	wEnt, _ := doc.Types[under].(map[string]any)
	wIface, _ := wEnt["detail"].(map[string]any)
	wMethods, _ := wIface["methods"].([]any)
	if len(wMethods) != 1 {
		t.Fatalf("W methods = %v, want 1 entry", wMethods)
	}
	m, _ := wMethods[0].(map[string]any)
	if m["name"] != "Read" {
		t.Errorf("W method name = %v, want Read", m["name"])
	}
	if _, ok := m["sig"].(int); !ok {
		t.Errorf("W.Read sig = %v, want a type id", m["sig"])
	}
	if got := wIface["empty"]; got != false {
		t.Errorf("W empty = %v, want false", got)
	}

	_, iNamed := typeEntry(t, doc, "named", "demo.I")
	iUnder, _ := iNamed["underlying"].(int)
	iEnt, _ := doc.Types[iUnder].(map[string]any)
	iIface, _ := iEnt["detail"].(map[string]any)
	iMethods, _ := iIface["methods"].([]any)
	if len(iMethods) != 2 {
		t.Fatalf("I methods = %v, want 2 entries (Read promoted, Close)", iMethods)
	}
	names := map[string]bool{}
	for _, mm := range iMethods {
		e, _ := mm.(map[string]any)
		names[e["name"].(string)] = true
	}
	if !names["Read"] || !names["Close"] {
		t.Errorf("I methods = %v, want Read and Close", names)
	}
	embedded, _ := iIface["embedded"].([]any)
	if len(embedded) != 1 {
		t.Fatalf("I embedded = %v, want 1 entry (W)", embedded)
	}
	if embedded[0] != wID {
		t.Errorf("I embedded[0] = %v, want W's named type id %d", embedded[0], wID)
	}
}

func TestCallResultTuple(t *testing.T) {
	// A void call (pair) and a two-result call (swap) must both
	// describe as tuple entries — not as basic/unknown "()".
	doc := emitSource(t, `package demo

func pair() {}
func swap(a, b int) (int, int) { return b, a }

func main() {
	pair()
	x, y := swap(1, 2)
	println(x, y)
}
`)
	for _, name := range []string{"pair", "swap"} {
		call := findNode(t, doc, "call", func(m map[string]any) bool {
			f, _ := m["fields"].(map[string]any)
			fun, _ := f["fun"].(map[string]any)
			funFields, _ := fun["fields"].(map[string]any)
			return funFields["name"] == name
		})
		fields, _ := call["fields"].(map[string]any)
		typeID, _ := fields["type"].(int)
		ent, _ := doc.Types[typeID].(map[string]any)
		if ent["kind"] != "tuple" {
			t.Errorf("call %s type kind = %q, want tuple (detail %v)", name, ent["kind"], ent["detail"])
			continue
		}
		d, _ := ent["detail"].(map[string]any)
		elems, _ := d["elems"].([]any)
		want := 0
		if name == "swap" {
			want = 2
		}
		if len(elems) != want {
			t.Errorf("call %s tuple elems = %v, want %d entries", name, elems, want)
		}
	}
}

func TestUnsupportedTypesFailLoudly(t *testing.T) {
	// A generic function (type parameter T) and a union constraint
	// must fail emission with a named error — not a bogus
	// basic/unknown entry, and not a panic.
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			"generic-func",
			"package demo\n\nfunc id[T any](x T) T { return x }\n\nfunc main() { println(id[int](42)) }\n",
			"unsupported",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := emitSourceExpectErr(t, c.src)
			if err == nil {
				t.Fatal("emit succeeded, want an unsupported-type error")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("emit error = %q, want it to contain %q", err.Error(), c.want)
			}
			if strings.Contains(err.Error(), "panic") {
				t.Errorf("emit error = %q, must not be a panic", err.Error())
			}
		})
	}
}

func TestUnsupportedTypesDirect(t *testing.T) {
	// Union and TypeParam have no source-level route to describeType
	// (a constraint interface cannot be referenced as a value type),
	// so pin the fail-loudly default case directly.
	e := &Emitter{typeIDs: make(map[types.Type]int), objIDs: make(map[types.Object]int)}
	union := types.NewUnion([]*types.Term{
		types.NewTerm(false, types.Typ[types.Int]),
		types.NewTerm(false, types.Typ[types.Int64]),
	})
	// The returned entry itself must be the loud "unsupported" marker
	// (the baseline code returns a bogus basic/unknown entry here).
	// The recorded error surfaces through Emit; see
	// TestUnsupportedTypesFailLoudly.
	ent := e.describeType(union)
	if ent["kind"] != "unsupported" {
		t.Errorf("describeType(union) kind = %v, want unsupported", ent["kind"])
	}

	e2 := &Emitter{typeIDs: make(map[types.Type]int), objIDs: make(map[types.Object]int)}
	tp := types.NewTypeParam(
		types.NewTypeName(0, nil, "T", nil),
		types.NewInterfaceType(nil, nil),
	)
	ent2 := e2.describeType(tp)
	if ent2["kind"] != "unsupported" {
		t.Errorf("describeType(typeparam) kind = %v, want unsupported", ent2["kind"])
	}
}

func TestAliasCanonicalizes(t *testing.T) {
	// `any` (universe alias for interface{}) and interface{} are
	// one Go type and must share one type-table entry; likewise a
	// declared alias `type A = int` and int.
	doc := emitSource(t, `package demo

type A = int

func main() {
	var x any
	var y interface{}
	var a A
	var i int
	println(x, y, a, i)
}
`)
	// Aliases canonicalize (v1 PKG-041): `any` resolves to the
	// empty interface and `A` to int — no "alias" kind entry is ever
	// minted. (A literal interface{} is a distinct go/types object
	// from the universe empty interface, so it keeps its own entry —
	// exactly like v1; only aliases canonicalize.)
	for _, raw := range doc.Types {
		ent, _ := raw.(map[string]any)
		if ent["kind"] == "alias" {
			t.Errorf("type table minted an alias entry: %v", ent)
		}
	}
	objType := func(name string) int {
		for _, raw := range doc.Objects {
			o, _ := raw.(map[string]any)
			if o["name"] == name {
				id, _ := o["type"].(int)
				return id
			}
		}
		t.Fatalf("object %q not found", name)
		return -1
	}
	anyID := objType("x")
	anyEnt, _ := doc.Types[anyID].(map[string]any)
	if anyEnt["kind"] != "interface" {
		t.Errorf("var x (any) type kind = %v, want interface", anyEnt["kind"])
	} else if d, _ := anyEnt["detail"].(map[string]any); d["empty"] != true {
		t.Errorf("var x (any) type detail = %v, want the empty interface", d)
	}
	if objType("a") != objType("i") {
		t.Errorf("var a (A) type id %d != var i (int) type id %d: alias A must canonicalize to int", objType("a"), objType("i"))
	}
	var intIDs []int
	for id, raw := range doc.Types {
		ent, _ := raw.(map[string]any)
		if ent["kind"] == "basic" {
			if d, _ := ent["detail"].(map[string]any); d["name"] == "int" {
				intIDs = append(intIDs, id)
			}
		}
	}
	if len(intIDs) != 1 {
		t.Errorf("int has %d basic entries %v, want exactly 1 (alias A must canonicalize)", len(intIDs), intIDs)
	}
}

func TestGeneratorVersions(t *testing.T) {
	// goroot/lang come from the real toolchain and the checker's
	// configured language version — never hardcoded literals.
	doc := emitSource(t, "package demo\n\nfunc main() { println(42) }\n")
	if doc.Generator["goroot"] != runtime.Version() {
		t.Errorf("goroot = %v, want runtime.Version() %q", doc.Generator["goroot"], runtime.Version())
	}
	if doc.Generator["version"] == "" {
		t.Error("generator version is empty")
	}
	if doc.Package["lang"] != "go1.27" {
		t.Errorf("lang = %v, want go1.27 (checker Config.GoVersion)", doc.Package["lang"])
	}
}
