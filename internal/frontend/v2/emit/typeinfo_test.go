// Type-table info tests: struct field offsets, named-type method sets,
// interface method sets and embedded interfaces.
//
// These pin the dispatch/layout data the backend needs. Before the fix,
// struct fields carried a hardcoded "offset": 0 and named/interface
// "methods"/"embedded" were always empty — field lookup by name happened
// to work, but any byte-offset use broke silently and dispatch had no data.
package emit

import (
	"go/types"
	"os"
	"path/filepath"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/parser"
)

const typeinfoSrc = `package main

type Inner struct {
	a bool
	b int64
	c int8
	d string
	e *int
	f [3]uint16
}

type Counter int

func (c Counter) Value() int { return int(c) }
func (c *Counter) Inc()     {} // pointer receiver: in *Counter's method set, not Counter's

type Stringer interface {
	String() string
}

type ReadWriter interface {
	Stringer
	Read(p []byte) int
}

func use(i Inner, c Counter, s Stringer, rw ReadWriter) {
	println(i.a, i.b, i.c)
	println(len(i.d))
	println(c.Value())
	println(s.String())
	println(rw.Read(nil))
}

func main() { use(Inner{}, 0, nil, nil) }
`

// emitTypeinfo runs the full pipeline on typeinfoSrc and returns the document.
func emitTypeinfo(t *testing.T) *Document {
	t.Helper()
	dir := t.TempDir()
	fn := filepath.Join(dir, "typeinfo.go")
	if err := os.WriteFile(fn, []byte(typeinfoSrc), 0644); err != nil {
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

// findType returns the first type-table entry with the given kind whose
// detail name matches (for named entries, "main.<Name>").
func findType(t *testing.T, doc *Document, kind, name string) map[string]any {
	t.Helper()
	for _, te := range doc.Types {
		m, ok := te.(map[string]any)
		if !ok {
			t.Fatalf("type entry is %T, want map[string]any", te)
		}
		if m["kind"] != kind {
			continue
		}
		d, ok := m["detail"].(map[string]any)
		if !ok {
			t.Fatalf("kind %s entry has detail %T", kind, m["detail"])
		}
		if name == "" || d["name"] == name {
			return d
		}
	}
	t.Fatalf("no type entry kind=%q name=%q", kind, name)
	return nil
}

// TestStructFieldOffsets asserts byte offsets for a struct with mixed field
// types. The gc/amd64 layout is the contract (see the sizes comment in
// emit.go): a bool, int64, int8, string, *int, [3]uint16 must land at
// 0, 8, 16, 24, 40, 48. The old hardcoded-0 emission fails this test.
func TestStructFieldOffsets(t *testing.T) {
	doc := emitTypeinfo(t)
	d := findType(t, doc, "struct", "")
	fields, ok := d["fields"].([]any)
	if !ok {
		t.Fatalf("struct detail fields is %T", d["fields"])
	}
	if len(fields) != 6 {
		t.Fatalf("got %d fields, want 6", len(fields))
	}
	wantNames := []string{"a", "b", "c", "d", "e", "f"}
	// gc/amd64 reference layout for the Inner struct above.
	wantOffsets := []int64{0, 8, 16, 24, 40, 48}
	for i, fe := range fields {
		f, ok := fe.(map[string]any)
		if !ok {
			t.Fatalf("field %d is %T", i, fe)
		}
		if f["name"] != wantNames[i] {
			t.Errorf("field %d name = %v, want %v", i, f["name"], wantNames[i])
		}
		off, ok := f["offset"].(int64)
		if !ok {
			t.Fatalf("field %s offset is %T, want int64", wantNames[i], f["offset"])
		}
		if off != wantOffsets[i] {
			t.Errorf("field %s offset = %d, want %d (gc/amd64)", wantNames[i], off, wantOffsets[i])
		}
	}
}

// TestStructOffsetsMatchReferenceModel type-checks the same source with
// go/types directly and asserts every emitted struct offset equals the
// reference gc/amd64 Offsetsof result. This fails if the emitter ever
// changes its sizes choice without updating the documented contract.
func TestStructOffsetsMatchReferenceModel(t *testing.T) {
	doc := emitTypeinfo(t)
	d := findType(t, doc, "struct", "")
	fields := d["fields"].([]any)

	dir := t.TempDir()
	fn := filepath.Join(dir, "ref.go")
	if err := os.WriteFile(fn, []byte(typeinfoSrc), 0644); err != nil {
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
	obj := cres.Pkg.Scope().Lookup("Inner")
	tn, ok := obj.(*types.TypeName)
	if !ok {
		t.Fatalf("Inner is %T", obj)
	}
	st, ok := tn.Type().Underlying().(*types.Struct)
	if !ok {
		t.Fatalf("Inner underlying is %T", tn.Type().Underlying())
	}
	vars := make([]*types.Var, st.NumFields())
	for i := range vars {
		vars[i] = st.Field(i)
	}
	ref := types.SizesFor("gc", "amd64").Offsetsof(vars)
	if len(ref) != len(fields) {
		t.Fatalf("reference has %d offsets, emitted %d fields", len(ref), len(fields))
	}
	for i, fe := range fields {
		off := fe.(map[string]any)["offset"].(int64)
		if off != ref[i] {
			t.Errorf("field %d: emitted offset %d != reference %d", i, off, ref[i])
		}
	}
}

// TestUnknownOffsetForGenericField pins the -1 sentinel: a struct whose
// field mentions a type parameter has an instantiation-dependent layout,
// so every offset must be unknownOffset (-1), never a guessed number.
func TestUnknownOffsetForGenericField(t *testing.T) {
	tp := types.NewTypeParam(
		types.NewTypeName(0, nil, "T", nil),
		types.NewInterfaceType(nil, nil).Complete(),
	)
	st := types.NewStruct([]*types.Var{
		types.NewField(0, nil, "v", tp, false),
		types.NewField(0, nil, "n", types.Typ[types.Int], false),
	}, nil)
	vars := []*types.Var{st.Field(0), st.Field(1)}
	offs := structOffsets(vars)
	for i, o := range offs {
		if o != unknownOffset {
			t.Errorf("field %d offset = %d, want unknownOffset (%d)", i, o, unknownOffset)
		}
	}
	if !sizesWouldPanic(tp) {
		t.Errorf("sizesWouldPanic(typeparam) = false, want true")
	}
	if sizesWouldPanic(types.Typ[types.Int64]) {
		t.Errorf("sizesWouldPanic(int64) = true, want false")
	}
}

// methodNames extracts method entry names, resolving each "sig" to a
// signature type-table entry (failing if any sig id is dangling).
func methodNames(t *testing.T, doc *Document, d map[string]any) []string {
	t.Helper()
	ms, ok := d["methods"].([]any)
	if !ok {
		t.Fatalf("methods is %T, want []any", d["methods"])
	}
	names := make([]string, 0, len(ms))
	for _, me := range ms {
		m, ok := me.(map[string]any)
		if !ok {
			t.Fatalf("method entry is %T", me)
		}
		name, _ := m["name"].(string)
		if name == "" {
			t.Errorf("method entry has no name: %v", m)
		}
		sigID, ok := m["sig"].(int)
		if !ok {
			t.Fatalf("method %q sig is %T, want int type id", name, m["sig"])
		}
		if sigID < 0 || sigID >= len(doc.Types) {
			t.Fatalf("method %q sig id %d out of range", name, sigID)
		}
		sigEnt, ok := doc.Types[sigID].(map[string]any)
		if !ok || sigEnt["kind"] != "signature" {
			t.Fatalf("method %q sig id %d does not resolve to a signature entry", name, sigID)
		}
		names = append(names, name)
	}
	return names
}

func hasName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

// TestNamedTypeMethodSet asserts the named type carries its method set
// (names + resolvable signatures). The old empty-methods emission fails
// this test. Pointer-receiver methods belong to *Counter's method set,
// not Counter's — pinned here so the contract is explicit.
func TestNamedTypeMethodSet(t *testing.T) {
	doc := emitTypeinfo(t)
	d := findType(t, doc, "named", "main.Counter")
	names := methodNames(t, doc, d)
	if !hasName(names, "Value") {
		t.Errorf("Counter methods = %v, want Value present", names)
	}
	if hasName(names, "Inc") {
		t.Errorf("Counter methods = %v, want Inc absent (pointer-receiver: method set of *Counter)", names)
	}
}

// TestInterfaceTypeInfo asserts interfaces carry their methods and their
// embedded interfaces as type references. The old empty emission fails.
func TestInterfaceTypeInfo(t *testing.T) {
	doc := emitTypeinfo(t)

	s := findType(t, doc, "interface", "")
	sNames := methodNames(t, doc, s)
	if !hasName(sNames, "String") {
		t.Errorf("Stringer methods = %v, want String", sNames)
	}
	if emb, ok := s["embedded"].([]any); !ok || len(emb) != 0 {
		t.Errorf("Stringer embedded = %v, want empty", s["embedded"])
	}

	// Find the ReadWriter entry: the interface whose methods include Read.
	var rw map[string]any
	for _, te := range doc.Types {
		m := te.(map[string]any)
		if m["kind"] != "interface" {
			continue
		}
		if hasName(methodNames(t, doc, m["detail"].(map[string]any)), "Read") {
			rw = m["detail"].(map[string]any)
			break
		}
	}
	if rw == nil {
		t.Fatal("no interface entry with method Read (ReadWriter)")
	}
	emb, ok := rw["embedded"].([]any)
	if !ok || len(emb) != 1 {
		t.Fatalf("ReadWriter embedded = %v, want exactly one embedded interface", rw["embedded"])
	}
	embID, ok := emb[0].(int)
	if !ok {
		t.Fatalf("ReadWriter embedded[0] is %T, want int type id", emb[0])
	}
	// The embedded reference is the *named* type Stringer; its underlying
	// is the interface carrying the String method.
	embEnt, ok := doc.Types[embID].(map[string]any)
	if !ok || embEnt["kind"] != "named" {
		t.Fatalf("ReadWriter embedded id %d does not resolve to the named Stringer entry (got %v)", embID, doc.Types[embID])
	}
	embDetail := embEnt["detail"].(map[string]any)
	if embDetail["name"] != "main.Stringer" {
		t.Errorf("ReadWriter embedded name = %v, want main.Stringer", embDetail["name"])
	}
	underID, ok := embDetail["underlying"].(int)
	if !ok {
		t.Fatalf("Stringer named entry underlying is %T", embDetail["underlying"])
	}
	underEnt, ok := doc.Types[underID].(map[string]any)
	if !ok || underEnt["kind"] != "interface" {
		t.Fatalf("Stringer underlying id %d does not resolve to an interface entry", underID)
	}
	if !hasName(methodNames(t, doc, underEnt["detail"].(map[string]any)), "String") {
		t.Errorf("ReadWriter's embedded interface has no String method")
	}
	if rw["empty"] != false {
		t.Errorf("ReadWriter empty = %v, want false", rw["empty"])
	}
}
