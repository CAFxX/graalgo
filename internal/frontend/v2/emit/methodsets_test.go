// Regression tests for the review-1106-p1-v2-methodsets remediation lane
// (independent codebase review 2026-11-06, R-P1-2).
//
// The v2 emitter used to drop every method set ("methods": []) and
// hard-code every struct field offset to 0, so any consumer reasoning
// about a named type or an interface from the type table saw a methodless
// type and every field claimed offset 0. These tests pin the populated
// tables: full method sets (value vs pointer receivers, promotion,
// embedded interfaces), real gc/amd64 field offsets, method identity
// (implkeys), funcdecl/selector object ids — and the cases the factory
// cannot dispatch yet, which must fail loudly at emit time instead of
// emitting a node the factory would mis-execute.
//
// The tests are demonstrably capable of failing: each one fails on the
// pre-fix emitter (empty methods, offset 0, no selector obj).
package emit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/parser"
)

// emitTestSrcErr runs one source file through parse → check → emit and
// returns the emit error (failing the test when emission succeeds).
func emitTestSrcErr(t *testing.T, src string) error {
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
	_, err = Emit(cres, pres.Pkg, pres.Files)
	if err == nil {
		t.Fatalf("emit succeeded, want an error for:\n%s", src)
	}
	return err
}

// typeDetail returns the detail map of the type-table entry with the given
// kind and qualified name ("main.Point", "main.Shaper"), or nil.
func typeDetail(doc *Document, kind, name string) map[string]any {
	for _, tv := range doc.Types {
		m, ok := tv.(map[string]any)
		if !ok || m["kind"] != kind {
			continue
		}
		d, ok := m["detail"].(map[string]any)
		if ok && d["name"] == name {
			return d
		}
	}
	return nil
}

// typeEntry returns the type-table entry (and its id) for the given kind
// and qualified name, or (-1, nil).
func typeEntry(doc *Document, kind, name string) (int, map[string]any) {
	for i, tv := range doc.Types {
		m, ok := tv.(map[string]any)
		if !ok || m["kind"] != kind {
			continue
		}
		d, ok := m["detail"].(map[string]any)
		if ok && d["name"] == name {
			return i, m
		}
	}
	return -1, nil
}

// namedDetailForObj returns the detail map of the named type of the
// object with the given name (e.g. the type of `var b Box[int]`), or nil.
func namedDetailForObj(doc *Document, objName string) map[string]any {
	for _, o := range doc.Objects {
		m, ok := o.(map[string]any)
		if !ok || m["name"] != objName {
			continue
		}
		tid, ok := m["type"].(int)
		if !ok {
			continue
		}
		tm, ok := doc.Types[tid].(map[string]any)
		if !ok || tm["kind"] != "named" {
			continue
		}
		d, _ := tm["detail"].(map[string]any)
		return d
	}
	return nil
}
func structDetail(t *testing.T, doc *Document, name string) map[string]any {
	t.Helper()
	_, named := typeEntry(doc, "named", name)
	if named == nil {
		t.Fatalf("no named type %s", name)
		return nil
	}
	d := named["detail"].(map[string]any)
	under, ok := d["underlying"].(int)
	if !ok {
		t.Fatalf("named %s has no underlying type id", name)
		return nil
	}
	m, ok := doc.Types[under].(map[string]any)
	if !ok || m["kind"] != "struct" {
		t.Fatalf("underlying of %s is not a struct: %#v", name, doc.Types[under])
		return nil
	}
	return m["detail"].(map[string]any)
}

// methodsOf returns the methods list of a type detail map.
func methodsOf(t *testing.T, d map[string]any) []map[string]any {
	t.Helper()
	raw, ok := d["methods"].([]any)
	if !ok {
		t.Fatalf("detail has no methods list: %#v", d)
		return nil
	}
	out := make([]map[string]any, 0, len(raw))
	for _, m := range raw {
		mm, ok := m.(map[string]any)
		if !ok {
			t.Fatalf("method entry is not a map: %#v", m)
			return nil
		}
		out = append(out, mm)
	}
	return out
}

// methodNames returns the method names in table order.
func methodNames(ms []map[string]any) []string {
	names := make([]string, len(ms))
	for i, m := range ms {
		names[i] = m["name"].(string)
	}
	return names
}

// methodByName returns the method entry with the given name, or nil.
func methodByName(ms []map[string]any, name string) map[string]any {
	for _, m := range ms {
		if m["name"] == name {
			return m
		}
	}
	return nil
}

// offsetOf reads a struct field's "offset" value tolerantly (int, int64)
// so a test reports a mismatch instead of panicking on a wrong-typed
// value (e.g. the old emitter's untyped 0).
func offsetOf(t *testing.T, f map[string]any) (int64, bool) {
	t.Helper()
	switch v := f["offset"].(type) {
	case int64:
		return v, true
	case int:
		return int64(v), true
	default:
		t.Errorf("field %v offset = %v (%T), want an integer offset", f["name"], f["offset"], f["offset"])
		return 0, false
	}
}
func objEntry(doc *Document, id int) map[string]any {
	if id < 0 || id >= len(doc.Objects) {
		return nil
	}
	m, _ := doc.Objects[id].(map[string]any)
	return m
}

// typeKind returns the kind of the type-table entry with the given id.
func typeKind(doc *Document, id int) string {
	if id < 0 || id >= len(doc.Types) {
		return ""
	}
	m, _ := doc.Types[id].(map[string]any)
	if m == nil {
		return ""
	}
	k, _ := m["kind"].(string)
	return k
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

const methodSetsSrc = `package main

type Shaper interface {
	Area() float64
	String() string
}

type Both interface {
	Shaper
	Extra() int
}

type Point struct {
	X, Y int
	name string
}

func (p Point) Area() float64 { return 0 }
func (p *Point) Scale(f float64) {}
func (p Point) String() string { return "" }
func (p Point) hidden() int { return 0 }

type Other struct{ N int }

func (o Other) Area() float64 { return 0 }

type Colored struct {
	Point
	Color string
}

type Pair struct {
	A byte
	B int64
	C byte
	D [0]int
	E int
}

func main() {
	p := Point{}
	pp := &Point{}
	_ = p.Area()
	pp.Scale(1)
	_ = p.String()
	c := Colored{}
	cp := &Colored{}
	_ = cp
	_ = c.Color
	_ = Pair{}
	var s Shaper
	_ = s
	var b Both
	_ = b
	var e interface{}
	_ = e
}
`

// TestNamedMethodSets: a named type carries the method set of exactly
// that type — value-receiver methods on T, the full set (value + pointer
// receivers) on *T. Before the fix both lists were always [].
func TestNamedMethodSets(t *testing.T) {
	doc := emitTestSrc(t, methodSetsSrc)

	d := typeDetail(doc, "named", "main.Point")
	if d == nil {
		t.Fatalf("no named type main.Point")
	}
	ms := methodsOf(t, d)
	// go/types sorts the method set by name: Area, String, hidden.
	if got := methodNames(ms); !equalStrings(got, []string{"Area", "String", "hidden"}) {
		t.Errorf("Point methods = %v, want [Area String hidden]", got)
	}
	for _, m := range ms {
		if m["recv"] != "value" {
			t.Errorf("Point.%s recv = %v, want value", m["name"], m["recv"])
		}
		if m["index"] == nil {
			t.Errorf("Point.%s index is null, want []", m["name"])
		} else if idx, ok := m["index"].([]any); !ok || len(idx) != 0 {
			t.Errorf("Point.%s index = %v, want []", m["name"], m["index"])
		}
		if m["addrOf"] != false {
			t.Errorf("Point.%s addrOf = %v, want false", m["name"], m["addrOf"])
		}
		// obj resolves to a func object with the same name; sig is a
		// signature type.
		objID, ok := m["obj"].(int)
		if !ok || objID < 0 {
			t.Errorf("Point.%s obj = %v, want a valid object id", m["name"], m["obj"])
			continue
		}
		o := objEntry(doc, objID)
		if o == nil || o["kind"] != "func" || o["name"] != m["name"] {
			t.Errorf("Point.%s obj %d = %#v, want a func object named %s", m["name"], objID, o, m["name"])
		}
		sigID, ok := m["sig"].(int)
		if !ok || typeKind(doc, sigID) != "signature" {
			t.Errorf("Point.%s sig = %v, want a signature type id", m["name"], m["sig"])
		}
		if k, _ := m["implkey"].(string); k == "" {
			t.Errorf("Point.%s implkey is empty", m["name"])
		}
	}
	// Unexported methods are package-qualified in the implkey (gc rule).
	hidden := methodByName(ms, "hidden")
	if hidden == nil {
		t.Fatalf("Point.hidden missing")
	}
	if k := hidden["implkey"].(string); !strings.HasPrefix(k, "main.hidden\x00") {
		t.Errorf("hidden implkey = %q, want prefix %q", k, "main.hidden\x00")
	}
	area := methodByName(ms, "Area")
	if k := area["implkey"].(string); strings.Contains(k, "main.") {
		t.Errorf("exported Area implkey = %q, want the bare name", k)
	}

	// *Point carries the full set, including the pointer-receiver Scale.
	// The pointer entry is the one whose elem is the Point named type.
	pointID, _ := typeEntry(doc, "named", "main.Point")
	var ptrMethods []map[string]any
	for _, tv := range doc.Types {
		m, ok := tv.(map[string]any)
		if !ok || m["kind"] != "pointer" {
			continue
		}
		d, _ := m["detail"].(map[string]any)
		if d["elem"] == pointID {
			ptrMethods = methodsOf(t, d)
			break
		}
	}
	if ptrMethods == nil {
		t.Fatalf("no *Point pointer entry")
	}
	if got := methodNames(ptrMethods); !equalStrings(got, []string{"Area", "Scale", "String", "hidden"}) {
		t.Errorf("*Point methods = %v, want [Area Scale String hidden]", got)
	}
	if m := methodByName(ptrMethods, "Scale"); m == nil {
		t.Errorf("*Point.Scale missing")
	} else if m["recv"] != "pointer" {
		t.Errorf("*Point.Scale recv = %v, want pointer", m["recv"])
	}

	// Same-named methods on different types have distinct objects.
	other := typeDetail(doc, "named", "main.Other")
	if other == nil {
		t.Fatalf("no named type main.Other")
	}
	otherArea := methodByName(methodsOf(t, other), "Area")
	if otherArea == nil {
		t.Fatalf("Other.Area missing")
	}
	if otherArea["obj"] == area["obj"] {
		t.Errorf("Point.Area and Other.Area share obj %v", area["obj"])
	}
}

// TestPromotedMethods: methods promoted through an embedded struct field
// appear in the embedding type's method set with the field path as their
// index; a pointer-receiver method promoted via a value field carries
// addrOf. Before the fix the Colored method set was [].
func TestPromotedMethods(t *testing.T) {
	doc := emitTestSrc(t, methodSetsSrc)

	d := typeDetail(doc, "named", "main.Colored")
	if d == nil {
		t.Fatalf("no named type main.Colored")
	}
	ms := methodsOf(t, d)
	if got := methodNames(ms); !equalStrings(got, []string{"Area", "String", "hidden"}) {
		t.Errorf("Colored methods = %v, want [Area String hidden]", got)
	}
	area := methodByName(ms, "Area")
	if idx, ok := area["index"].([]any); !ok || len(idx) != 1 || idx[0] != 0 {
		t.Errorf("Colored.Area index = %v, want [0]", area["index"])
	}
	if area["addrOf"] != false {
		t.Errorf("Colored.Area addrOf = %v, want false", area["addrOf"])
	}
	// The promoted method resolves to the SAME object as Point.Area:
	// promotion does not mint a new function.
	pointArea := methodByName(methodsOf(t, typeDetail(doc, "named", "main.Point")), "Area")
	if area["obj"] != pointArea["obj"] {
		t.Errorf("Colored.Area obj = %v, Point.Area obj = %v, want the same object", area["obj"], pointArea["obj"])
	}

	// *Colored includes the promoted pointer-receiver Scale with addrOf.
	pointID, _ := typeEntry(doc, "named", "main.Colored")
	var ptrMethods []map[string]any
	for _, tv := range doc.Types {
		m, ok := tv.(map[string]any)
		if !ok || m["kind"] != "pointer" {
			continue
		}
		dd, _ := m["detail"].(map[string]any)
		if dd["elem"] == pointID {
			ptrMethods = methodsOf(t, dd)
			break
		}
	}
	if ptrMethods == nil {
		t.Fatalf("no *Colored pointer entry")
	}
	scale := methodByName(ptrMethods, "Scale")
	if scale == nil {
		t.Fatalf("*Colored.Scale missing")
	}
	if idx, ok := scale["index"].([]any); !ok || len(idx) != 1 || idx[0] != 0 {
		t.Errorf("*Colored.Scale index = %v, want [0]", scale["index"])
	}
	if scale["addrOf"] != true {
		t.Errorf("*Colored.Scale addrOf = %v, want true (pointer receiver via value-embedded field)", scale["addrOf"])
	}
}

// TestInterfaceMethodSets: interfaces carry their methods (flattened
// across embedded interfaces) plus the embedded type ids. Before the fix
// both lists were always [].
func TestInterfaceMethodSets(t *testing.T) {
	doc := emitTestSrc(t, methodSetsSrc)

	// Interfaces have no "name" in the detail; locate them by their
	// method sets: Shaper has {Area, String}, Both has {Area, Extra, String}.
	var shaperDetail, bothDetail map[string]any
	for _, tv := range doc.Types {
		m, ok := tv.(map[string]any)
		if !ok || m["kind"] != "interface" {
			continue
		}
		d := m["detail"].(map[string]any)
		names := methodNames(methodsOf(t, d))
		switch {
		case equalStrings(names, []string{"Area", "String"}):
			shaperDetail = d
		case equalStrings(names, []string{"Area", "Extra", "String"}):
			bothDetail = d
		}
	}
	if shaperDetail == nil {
		t.Fatalf("Shaper interface not found")
	}
	if bothDetail == nil {
		t.Fatalf("Both interface not found")
	}
	if shaperDetail["empty"] != false {
		t.Errorf("Shaper empty = %v, want false", shaperDetail["empty"])
	}
	if emb, _ := shaperDetail["embedded"].([]any); len(emb) != 0 {
		t.Errorf("Shaper embedded = %v, want []", emb)
	}
	// Interface method entries carry name/sig/implkey, no obj (there is
	// no funcdecl for an interface method).
	for _, m := range methodsOf(t, shaperDetail) {
		if _, ok := m["obj"]; ok {
			t.Errorf("interface method %s carries obj %v, want none", m["name"], m["obj"])
		}
		sigID, ok := m["sig"].(int)
		if !ok || typeKind(doc, sigID) != "signature" {
			t.Errorf("interface method %s sig = %v, want a signature type id", m["name"], m["sig"])
		}
		if k, _ := m["implkey"].(string); k == "" {
			t.Errorf("interface method %s implkey is empty", m["name"])
		}
	}
	// Both embeds Shaper: exactly one embedded type — the NAMED type
	// main.Shaper (EmbeddedType returns the type as written), whose
	// underlying interface carries the flattened Shaper methods.
	emb, _ := bothDetail["embedded"].([]any)
	if len(emb) != 1 {
		t.Fatalf("Both embedded = %v, want 1 entry", emb)
	}
	embID, ok := emb[0].(int)
	if !ok {
		t.Fatalf("Both embedded[0] = %v, want a type id", emb[0])
	}
	embEntry, _ := doc.Types[embID].(map[string]any)
	if embEntry["kind"] != "named" {
		t.Errorf("Both embedded type kind = %v, want named (main.Shaper)", embEntry["kind"])
	}
	embDetail := embEntry["detail"].(map[string]any)
	if embDetail["name"] != "main.Shaper" {
		t.Errorf("Both embedded type name = %v, want main.Shaper", embDetail["name"])
	}
	underID, _ := embDetail["underlying"].(int)
	underEntry, _ := doc.Types[underID].(map[string]any)
	if underEntry["kind"] != "interface" {
		t.Fatalf("main.Shaper underlying kind = %v, want interface", underEntry["kind"])
	}
	if got := methodNames(methodsOf(t, underEntry["detail"].(map[string]any))); !equalStrings(got, []string{"Area", "String"}) {
		t.Errorf("Both embedded methods = %v, want [Area String]", got)
	}

	// The empty interface carries empty methods/embedded and empty=true.
	emptyFound := false
	for _, tv := range doc.Types {
		m, ok := tv.(map[string]any)
		if !ok || m["kind"] != "interface" {
			continue
		}
		d := m["detail"].(map[string]any)
		if d["empty"] == true {
			emptyFound = true
			if len(methodsOf(t, d)) != 0 {
				t.Errorf("empty interface has methods: %v", methodNames(methodsOf(t, d)))
			}
			if emb, _ := d["embedded"].([]any); len(emb) != 0 {
				t.Errorf("empty interface has embedded: %v", emb)
			}
		}
	}
	if !emptyFound {
		t.Errorf("no empty interface entry in the type table")
	}
}

// TestStructFieldOffsets: field offsets are real gc/amd64 byte offsets,
// not 0. Pair is byte(0), int64(8), byte(16), [0]int(24), int(24): the
// zero-size field shares the next field's offset.
func TestStructFieldOffsets(t *testing.T) {
	doc := emitTestSrc(t, methodSetsSrc)

	d := structDetail(t, doc, "main.Pair")
	fields, _ := d["fields"].([]any)
	if len(fields) != 5 {
		t.Fatalf("Pair fields = %d, want 5", len(fields))
	}
	want := []int64{0, 8, 16, 24, 24}
	for i, f := range fields {
		fm := f.(map[string]any)
		off, ok := offsetOf(t, fm)
		if !ok {
			continue
		}
		if off != want[i] {
			t.Errorf("Pair field %d (%s) offset = %d, want %d", i, fm["name"], off, want[i])
		}
	}
	// Point's struct has no padding surprises: X@0, Y@8, name@16.
	pd := structDetail(t, doc, "main.Point")
	pfields, _ := pd["fields"].([]any)
	if len(pfields) != 3 {
		t.Fatalf("Point fields = %d, want 3", len(pfields))
	}
	for i, wantOff := range []int64{0, 8, 16} {
		off, ok := offsetOf(t, pfields[i].(map[string]any))
		if !ok {
			continue
		}
		if off != wantOff {
			t.Errorf("Point field %d offset = %d, want %d", i, off, wantOff)
		}
	}
	// A plain struct's method set is empty but present (uniform shape).
	if ms := methodsOf(t, pd); len(ms) != 0 {
		t.Errorf("Point struct methods = %v, want []", methodNames(ms))
	}
}

// TestGenericStructOffsetsUnknown: offsets of a generic struct TEMPLATE
// are instantiation-dependent, so the wire carries -1 ("unknown"), never
// a bogus 0. (An INSTANTIATED generic struct, e.g. Box[int], has concrete
// field types and real offsets — only the template is unknown.)
func TestGenericStructOffsetsUnknown(t *testing.T) {
	doc := emitTestSrc(t, `package main

type Box[T any] struct {
	V T
	N int
}

func Use[T any](b Box[T]) int { return b.N }

func main() {
	println(1)
}
`)
	// The template's named entry is "main.Box"; locate it via the type
	// of Use's parameter b.
	d := namedDetailForObj(doc, "b")
	if d == nil {
		t.Fatalf("no named type for object b")
	}
	under, ok := d["underlying"].(int)
	if !ok {
		t.Fatalf("Box[T] has no underlying type id")
	}
	m, ok := doc.Types[under].(map[string]any)
	if !ok || m["kind"] != "struct" {
		t.Fatalf("underlying of Box[T] is not a struct: %#v", doc.Types[under])
	}
	fields, _ := m["detail"].(map[string]any)["fields"].([]any)
	if len(fields) != 2 {
		t.Fatalf("Box[T] fields = %d, want 2", len(fields))
	}
	for i, f := range fields {
		off, ok := offsetOf(t, f.(map[string]any))
		if !ok {
			continue
		}
		if off != -1 {
			t.Errorf("Box[T] field %d offset = %d, want -1 (unknown)", i, off)
		}
	}
}

// TestFuncDeclObj: every funcdecl carries its object id, and same-named
// methods on different types have distinct ids resolving to distinct
// func objects.
func TestFuncDeclObj(t *testing.T) {
	doc := emitTestSrc(t, methodSetsSrc)
	fds := funcDecls(t, doc)
	seen := map[int]string{}
	missingObj := false
	for _, fd := range fds {
		f := fieldsOf(fd)
		objID, ok := f["obj"].(int)
		if !ok || objID < 0 {
			t.Errorf("funcdecl %v has no obj", f["name"])
			missingObj = true
			continue
		}
		if prev, dup := seen[objID]; dup {
			t.Errorf("funcdecls %s and %s share obj %d", prev, f["name"], objID)
		}
		seen[objID] = f["name"].(string)
		o := objEntry(doc, objID)
		if o == nil || o["kind"] != "func" || o["name"] != f["name"] {
			t.Errorf("funcdecl %v obj %d = %#v, want a func object named %v", f["name"], objID, o, f["name"])
		}
	}
	if missingObj {
		// The distinctness check below needs every obj present; the
		// missing ones are already reported above.
		t.Fatalf("some funcdecls lack obj; cannot check distinctness")
	}
	// The two Area methods (Point.Area, Other.Area) have distinct objs.
	var areas []int
	for _, fd := range fds {
		if fieldsOf(fd)["name"] == "Area" {
			areas = append(areas, fieldsOf(fd)["obj"].(int))
		}
	}
	if len(areas) != 2 {
		t.Fatalf("Area funcdecls = %d, want 2", len(areas))
	}
	if areas[0] == areas[1] {
		t.Errorf("Point.Area and Other.Area share obj %d", areas[0])
	}
}

// TestMethodSelectorObj: a direct method value / method expression
// carries the method's object id; a field selector carries none.
func TestMethodSelectorObj(t *testing.T) {
	doc := emitTestSrc(t, `package main

type Point struct{ X int }

func (p Point) Get() int { return p.X }

func main() {
	p := Point{X: 3}
	f := Point.Get
	println(p.X, p.Get(), f(p))
}
`)
	sels := nodesOf(t, doc, "selector")
	if len(sels) != 4 {
		t.Fatalf("selector nodes = %d, want 4", len(sels))
	}
	for _, s := range sels {
		f := fieldsOf(s)
		switch f["selkind"] {
		case "field":
			if _, ok := f["obj"]; ok {
				t.Errorf("field selector %s carries obj %v, want none", f["sel"], f["obj"])
			}
		case "method", "methodexpr":
			objID, ok := f["obj"].(int)
			if !ok || objID < 0 {
				t.Errorf("%s selector %s obj = %v, want a valid object id", f["selkind"], f["sel"], f["obj"])
				continue
			}
			o := objEntry(doc, objID)
			if o == nil || o["kind"] != "func" || o["name"] != f["sel"] {
				t.Errorf("%s selector %s obj %d = %#v, want a func object named %s", f["selkind"], f["sel"], objID, o, f["sel"])
			}
		default:
			t.Errorf("selector %s has unexpected selkind %v", f["sel"], f["selkind"])
		}
	}
}

// TestMethodSelectionNeedsAdaptationFails: selections needing receiver
// adaptation (implicit & / deref, promotion) fail loudly at emit time
// instead of emitting a node the factory would mis-execute. Each case
// type-checks in Go — the failure is a v2 scope limit, not a bad program.
func TestMethodSelectionNeedsAdaptationFails(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			"implicit-addr-of",
			`package main

type Point struct{ X int }

func (p *Point) Ptr() int { return p.X }

func main() {
	p := Point{}
	println(p.Ptr())
}
`,
			"implicit address-of",
		},
		{
			"implicit-deref",
			`package main

type Point struct{ X int }

func (p Point) Get() int { return p.X }

func main() {
	p := Point{}
	pp := &p
	println(pp.Get())
}
`,
			"implicit deref",
		},
		{
			"promoted-method-value",
			`package main

type Point struct{ X int }

func (p Point) Get() int { return p.X }

type Colored struct{ Point }

func main() {
	c := Colored{}
	println(c.Get())
}
`,
			"promoted via embedded field",
		},
		{
			"promoted-method-expr",
			`package main

type Point struct{ X int }

func (p Point) Get() int { return p.X }

type Colored struct{ Point }

func main() {
	f := Colored.Get
	println(f(Colored{}))
}
`,
			"promoted via embedded field",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := emitTestSrcErr(t, tc.src)
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("emit error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

// TestEmbeddedInterfaceMethodTable: a method promoted via an embedded
// interface field is IN the table as a redispatch entry (obj -1, with
// the embedded interface's type id) — not silently dropped.
func TestEmbeddedInterfaceMethodTable(t *testing.T) {
	doc := emitTestSrc(t, `package main

type Reader interface {
	Read([]byte) (int, error)
}

type Wrap struct {
	Reader
	Name string
}

func main() {
	var w Wrap
	_ = w.Name
}
`)
	// The Wrap method set contains Read as a redispatch entry.
	d := typeDetail(doc, "named", "main.Wrap")
	if d == nil {
		t.Fatalf("no named type main.Wrap")
	}
	ms := methodsOf(t, d)
	read := methodByName(ms, "Read")
	if read == nil {
		t.Fatalf("Wrap methods = %v, want Read present as a redispatch entry", methodNames(ms))
	}
	if read["obj"] != -1 {
		t.Errorf("Wrap.Read obj = %v, want -1 (redispatch entry)", read["obj"])
	}
	embID, ok := read["embeddedIface"].(int)
	if !ok {
		t.Fatalf("Wrap.Read has no embeddedIface: %#v", read)
	}
	embEntry, _ := doc.Types[embID].(map[string]any)
	if embEntry["kind"] != "interface" {
		t.Errorf("Wrap.Read embeddedIface kind = %v, want interface", embEntry["kind"])
	}
	if got := methodNames(methodsOf(t, embEntry["detail"].(map[string]any))); !equalStrings(got, []string{"Read"}) {
		t.Errorf("embedded interface methods = %v, want [Read]", got)
	}
	if idx, ok := read["index"].([]any); !ok || len(idx) != 1 || idx[0] != 0 {
		t.Errorf("Wrap.Read index = %v, want [0]", read["index"])
	}
}

// TestImplkeySemantics: implkeys identify (name, signature), not names
// alone — and render types structurally (byte == uint8, aliases expanded).
func TestImplkeySemantics(t *testing.T) {
	doc := emitTestSrc(t, `package main

type B = []byte

type W1 struct{}
type W2 struct{}

func (w W1) Write(b []byte) int  { return 0 }
func (w W2) Write(b []uint8) int { return 0 }
func (w W1) WriteString(s string) int { return 0 }
func (w W2) WriteString(s []byte) int { return 0 }

type Writer interface {
	Write(B) int
}

func main() {
	var _ Writer = W1{}
	var _ Writer = W2{}
}
`)
	w1 := methodByName(methodsOf(t, typeDetail(doc, "named", "main.W1")), "Write")
	w2 := methodByName(methodsOf(t, typeDetail(doc, "named", "main.W2")), "Write")
	if w1 == nil || w2 == nil {
		t.Fatalf("Write methods missing: w1=%v w2=%v", w1, w2)
	}
	// []byte vs []uint8 are the same type: same implkey.
	if w1["implkey"] != w2["implkey"] {
		t.Errorf("Write implkeys differ: %q vs %q (byte == uint8)", w1["implkey"], w2["implkey"])
	}
	// Different signatures: different implkeys.
	w1s := methodByName(methodsOf(t, typeDetail(doc, "named", "main.W1")), "WriteString")
	w2s := methodByName(methodsOf(t, typeDetail(doc, "named", "main.W2")), "WriteString")
	if w1s["implkey"] == w2s["implkey"] {
		t.Errorf("WriteString implkeys equal: %q (string vs []byte must differ)", w1s["implkey"])
	}
	// The interface's Write implkey matches the implementations': the
	// interface spells the parameter through the alias B (== []byte),
	// so equality proves alias expansion in the implkey.
	// Locate the Writer interface by its method set.
	var writerDetail map[string]any
	for _, tv := range doc.Types {
		m, ok := tv.(map[string]any)
		if !ok || m["kind"] != "interface" {
			continue
		}
		d := m["detail"].(map[string]any)
		if equalStrings(methodNames(methodsOf(t, d)), []string{"Write"}) {
			writerDetail = d
		}
	}
	if writerDetail == nil {
		t.Fatalf("Writer interface not found")
	}
	iw := methodByName(methodsOf(t, writerDetail), "Write")
	if iw["implkey"] != w1["implkey"] {
		t.Errorf("Writer.Write implkey = %q, W1.Write implkey = %q, want equal", iw["implkey"], w1["implkey"])
	}
}
