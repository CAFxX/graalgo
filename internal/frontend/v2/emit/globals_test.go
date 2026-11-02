// Positive tests for top-level declarations (R21) and the funcdecl
// object-description fixes (P2 item 9): globals in Document.Globals,
// populated method sets on named types and interfaces, unnamed
// parameters and results, method receivers, and correct object kinds
// for constants, type names, and package names.
//
// Like stmts_test.go, these tests assert specific node kinds, fields,
// and values — a test that only asserted "emission succeeded" would pass
// even if globals or methods were still silently dropped.
package emit

import (
	"go/constant"
	"go/token"
	"go/types"
	"testing"
)

// globalByKind returns the global nodes of the given kind, in order.
func globalsByKind(t *testing.T, doc *Document, kind string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, g := range doc.Globals {
		m, ok := g.(map[string]any)
		if !ok {
			t.Fatalf("global is not a node: %#v", g)
		}
		if m["kind"] == kind {
			out = append(out, m)
		}
	}
	return out
}

// objectByName finds an entry of Document.Objects by its "name" field.
func objectByName(t *testing.T, doc *Document, name string) map[string]any {
	t.Helper()
	for _, o := range doc.Objects {
		m, ok := o.(map[string]any)
		if !ok || m == nil {
			continue
		}
		if m["name"] == name {
			return m
		}
	}
	t.Fatalf("no object named %q in %d objects", name, len(doc.Objects))
	return nil
}

// typeDetail finds a type-table entry by kind and detail name.
func typeDetail(t *testing.T, doc *Document, kind, name string) map[string]any {
	t.Helper()
	for _, ty := range doc.Types {
		m, ok := ty.(map[string]any)
		if !ok {
			continue
		}
		if m["kind"] != kind {
			continue
		}
		detail, ok := m["detail"].(map[string]any)
		if !ok {
			continue
		}
		if detail["name"] == name {
			return detail
		}
	}
	t.Fatalf("no %s type named %q in %d types", kind, name, len(doc.Types))
	return nil
}

func TestTopLevelGlobals(t *testing.T) {
	doc := emitDoc(t, `package demo

var Count = 42
var Name, Tag string
var Pi float64 = 3.14

const Answer = 42

const (
	A = iota
	B
	C
)

type Point struct {
	X int
	Y int
}

type Alias = Point
type Counter int

func main() {}
`)
	// One node per spec, in source order.
	if len(doc.Globals) != 10 {
		t.Fatalf("Globals has %d entries, want 10", len(doc.Globals))
	}
	kinds := []string{}
	for _, g := range doc.Globals {
		kinds = append(kinds, g.(map[string]any)["kind"].(string))
	}
	wantKinds := []string{
		"globalvar", "globalvar", "globalvar",
		"globalconst", "globalconst", "globalconst", "globalconst",
		"globaltype", "globaltype", "globaltype",
	}
	for i, want := range wantKinds {
		if kinds[i] != want {
			t.Fatalf("global %d kind = %s, want %s (all kinds: %v)", i, kinds[i], want, kinds)
		}
	}

	// var Count = 42: one name, one value expression.
	count := nodeFields(t, doc.Globals[0])
	if len(count["names"].([]string)) != 1 || count["names"].([]string)[0] != "Count" {
		t.Fatalf("Count names = %v", count["names"])
	}
	values, ok := count["values"].([]any)
	if !ok || len(values) != 1 {
		t.Fatalf("Count values has %d entries, want 1", len(values))
	}
	if values[0].(map[string]any)["kind"] != "basiclit" {
		t.Fatalf("Count value kind = %v, want basiclit", values[0].(map[string]any)["kind"])
	}

	// var Name, Tag string: two names sharing one type, no values.
	names := nodeFields(t, doc.Globals[1])
	if len(names["names"].([]string)) != 2 {
		t.Fatalf("Name/Tag names = %v", names["names"])
	}
	if names["type"] == nil {
		t.Fatalf("Name/Tag type is nil")
	}
	if len(names["values"].([]any)) != 0 {
		t.Fatalf("Name/Tag values = %v, want empty", names["values"])
	}

	// Iota chain: B and C carry no AST values, but constvals resolves
	// them from go/types — that is the whole point of the field.
	consts := globalsByKind(t, doc, "globalconst")
	if len(consts) != 4 {
		t.Fatalf("found %d globalconst nodes, want 4", len(consts))
	}
	iotaVals := nodeFields(t, consts[1])["constvals"].([]any)
	if len(iotaVals) != 1 || iotaVals[0] != "0" {
		t.Fatalf("A constvals = %v, want [0]", iotaVals)
	}
	if nodeFields(t, consts[2])["constvals"].([]any)[0] != "1" {
		t.Fatalf("B constvals = %v, want [1]", nodeFields(t, consts[2])["constvals"])
	}
	if nodeFields(t, consts[3])["constvals"].([]any)[0] != "2" {
		t.Fatalf("C constvals = %v, want [2]", nodeFields(t, consts[3])["constvals"])
	}
	if len(nodeFields(t, consts[2])["values"].([]any)) != 0 {
		t.Fatalf("B values = %v, want empty (repeated spec)", nodeFields(t, consts[2])["values"])
	}

	// Type declarations: alias flagged, non-alias not.
	types := globalsByKind(t, doc, "globaltype")
	if len(types) != 3 {
		t.Fatalf("found %d globaltype nodes, want 3", len(types))
	}
	if nodeFields(t, types[0])["name"] != "Point" {
		t.Fatalf("first globaltype name = %v, want Point", nodeFields(t, types[0])["name"])
	}
	if nodeFields(t, types[1])["alias"] != true {
		t.Fatalf("Alias alias = %v, want true", nodeFields(t, types[1])["alias"])
	}
	if nodeFields(t, types[2])["alias"] == true {
		t.Fatalf("Counter alias = true, want false")
	}
	for i, ty := range types {
		if nodeFields(t, ty)["type"] == nil {
			t.Fatalf("globaltype %d has no type id", i)
		}
	}

	// Object kinds: constants are "const" and types are "type", never
	// the old mislabelled "var".
	answer := objectByName(t, doc, "Answer")
	if answer["kind"] != "const" {
		t.Fatalf("Answer object kind = %v, want const", answer["kind"])
	}
	if answer["value"] != "42" {
		t.Fatalf("Answer object value = %v, want 42", answer["value"])
	}
	point := objectByName(t, doc, "Point")
	if point["kind"] != "type" {
		t.Fatalf("Point object kind = %v, want type", point["kind"])
	}
	countObj := objectByName(t, doc, "Count")
	if countObj["kind"] != "var" {
		t.Fatalf("Count object kind = %v, want var", countObj["kind"])
	}
}

func TestNamedTypeMethodsAndInterfaces(t *testing.T) {
	doc := emitDoc(t, `package demo

type Counter int

func (c Counter) Val() int { return int(c) }
func (c *Counter) Inc() {}

type Stringer interface {
	String() string
}

type Reader interface {
	Read() int
}

type Writer interface {
	Write(int) int
}

type ReadWriter interface {
	Reader
	Writer
}

func main() {}
`)
	// Value method set holds Val; pointer-only set holds Inc, and Inc
	// must not also appear in the value set.
	counter := typeDetail(t, doc, "named", "demo.Counter")
	methods, ok := counter["methods"].([]any)
	if !ok || len(methods) != 1 {
		t.Fatalf("Counter methods has %d entries, want 1 (Val)", len(methods))
	}
	val := methods[0].(map[string]any)
	if val["name"] != "Val" || val["obj"] == nil || val["sig"] == nil {
		t.Fatalf("Val method entry = %v", val)
	}
	ptrMethods, ok := counter["ptrMethods"].([]any)
	if !ok || len(ptrMethods) != 1 {
		t.Fatalf("Counter ptrMethods has %d entries, want 1 (Inc)", len(ptrMethods))
	}
	if ptrMethods[0].(map[string]any)["name"] != "Inc" {
		t.Fatalf("ptrMethods entry = %v, want Inc", ptrMethods[0])
	}

	// Interfaces store no name in their detail, so they are identified
	// by shape: Stringer has one explicit method and is complete;
	// ReadWriter has two embedded interfaces and no explicit methods.
	stringerSeen, readWriterSeen := false, false
	for _, ty := range doc.Types {
		m, ok := ty.(map[string]any)
		if !ok || m["kind"] != "interface" {
			continue
		}
		detail := m["detail"].(map[string]any)
		ms, _ := detail["methods"].([]any)
		es, _ := detail["embedded"].([]any)
		if len(ms) == 1 && ms[0].(map[string]any)["name"] == "String" {
			if detail["complete"] != true {
				t.Fatalf("Stringer complete = %v, want true", detail["complete"])
			}
			stringerSeen = true
		}
		if len(es) == 2 && len(ms) == 0 {
			readWriterSeen = true
		}
	}
	if !stringerSeen {
		t.Fatalf("no interface with a String method found")
	}
	if !readWriterSeen {
		t.Fatalf("no interface with 2 embedded and 0 explicit methods (ReadWriter) found")
	}
}

func TestFuncDeclParamsResultsRecv(t *testing.T) {
	// NOTE: Go forbids mixing named and unnamed parameters within one
	// list (verified against the parser: "a int, string" fails with
	// "missing parameter type", "[]byte, a int" with "missing
	// parameter name", while "string, a int" silently groups into
	// two names of one field). So unnamed parameters are tested on
	// an all-unnamed list, and named parameters on a named list with
	// unnamed results — params and results are separate lists, so
	// that combination is legal.
	doc := emitDoc(t, `package demo

func Mixed(a int, c string) (int, error) {
	return a, nil
}

func AllUnnamed(string, int) (int, string) {
	return 0, ""
}

type T struct{}

func (t T) Named() {}

type U struct{}

func (U) Unnamed() {}

func Plain() {}
`)
	mixed := funcNode(t, doc, "Mixed")
	params, ok := mixed["params"].([]any)
	if !ok || len(params) != 2 {
		t.Fatalf("Mixed params has %d entries, want 2", len(params))
	}
	if nodeFields(t, params[0])["name"] != "a" || nodeFields(t, params[1])["name"] != "c" {
		t.Fatalf("param names = %q, %q, want a, c",
			nodeFields(t, params[0])["name"], nodeFields(t, params[1])["name"])
	}
	// Unnamed results: two entries, both with types.
	results, ok := mixed["results"].([]any)
	if !ok || len(results) != 2 {
		t.Fatalf("Mixed results has %d entries, want 2", len(results))
	}
	for i, r := range results {
		if nodeFields(t, r)["type"] == nil {
			t.Fatalf("result %d has no type", i)
		}
		if nodeFields(t, r)["name"] != "" {
			t.Fatalf("result %d name = %q, want empty (unnamed)", i, nodeFields(t, r)["name"])
		}
	}
	if mixed["recv"] != nil {
		t.Fatalf("plain func recv = %#v, want nil", mixed["recv"])
	}

	// All-unnamed parameters: empty names, but real objects and types
	// — the old AST-name walk dropped these entirely.
	allUnnamed := funcNode(t, doc, "AllUnnamed")
	uparams, ok := allUnnamed["params"].([]any)
	if !ok || len(uparams) != 2 {
		t.Fatalf("AllUnnamed params has %d entries, want 2", len(uparams))
	}
	for i, p := range uparams {
		fields := nodeFields(t, p)
		if fields["name"] != "" {
			t.Fatalf("unnamed param %d name = %q, want empty", i, fields["name"])
		}
		if fields["obj"] == nil || fields["type"] == nil {
			t.Fatalf("unnamed param %d lacks obj/type: %#v", i, fields)
		}
	}
	if nodeFields(t, uparams[0])["type"] == nodeFields(t, uparams[1])["type"] {
		t.Fatalf("unnamed param types are identical, want string vs int")
	}

	// Named receiver: name and object.
	named := funcNode(t, doc, "Named")
	recv := nodeFields(t, named["recv"])
	if recv["name"] != "t" || recv["obj"] == nil || recv["type"] == nil {
		t.Fatalf("Named recv = %#v", recv)
	}
	// Unnamed receiver: empty name, no object, but a type.
	unnamed := funcNode(t, doc, "Unnamed")
	urecv := nodeFields(t, unnamed["recv"])
	if urecv["name"] != "" {
		t.Fatalf("Unnamed recv name = %q, want empty", urecv["name"])
	}
	if urecv["obj"] != nil {
		t.Fatalf("Unnamed recv obj = %#v, want nil", urecv["obj"])
	}
	if urecv["type"] == nil {
		t.Fatalf("Unnamed recv has no type")
	}
	// Plain function: recv nil.
	plain := funcNode(t, doc, "Plain")
	if plain["recv"] != nil {
		t.Fatalf("Plain recv = %#v, want nil", plain["recv"])
	}
}

// Note on the "error otherwise" branch of describeObject: it cannot be
// pinned by a test, because go1.27's types.Object carries unexported
// methods (order/setType/setOrder/setParent/sameId) and therefore cannot
// be implemented outside go/types — every object the checker can hand
// the emitter is one of the eight kinds tested below. The branch stays
// as a fail-loud guard for a future toolchain that opens the interface.
func TestDescribeObjectKinds(t *testing.T) {
	pkg := types.NewPackage("demo", "demo")
	e := &Emitter{
		typeIDs: make(map[types.Type]int),
		objIDs:  make(map[types.Object]int),
	}
	constObj := types.NewConst(token.NoPos, pkg, "Answer", types.Typ[types.Int], constant.MakeInt64(42))
	m, err := e.describeObject(constObj, false)
	if err != nil {
		t.Fatalf("describeObject(Const): %v", err)
	}
	if m["kind"] != "const" || m["value"] != "42" {
		t.Fatalf("Const description = %v, want kind const value 42", m)
	}
	typeObj := types.NewTypeName(token.NoPos, pkg, "Point", nil)
	m, err = e.describeObject(typeObj, false)
	if err != nil {
		t.Fatalf("describeObject(TypeName): %v", err)
	}
	if m["kind"] != "type" {
		t.Fatalf("TypeName description kind = %v, want type", m["kind"])
	}
	imported := types.NewPackage("fmt", "fmt")
	pkgObj := types.NewPkgName(token.NoPos, pkg, "fmt", imported)
	m, err = e.describeObject(pkgObj, false)
	if err != nil {
		t.Fatalf("describeObject(PkgName): %v", err)
	}
	if m["kind"] != "package" || m["path"] != "fmt" {
		t.Fatalf("PkgName description = %v, want kind package path fmt", m)
	}
	labelObj := types.NewLabel(token.NoPos, pkg, "outer")
	m, err = e.describeObject(labelObj, false)
	if err != nil {
		t.Fatalf("describeObject(Label): %v", err)
	}
	if m["kind"] != "label" {
		t.Fatalf("Label description kind = %v, want label", m["kind"])
	}
	nilObj := types.Universe.Lookup("nil")
	m, err = e.describeObject(nilObj, false)
	if err != nil {
		t.Fatalf("describeObject(Nil): %v", err)
	}
	if m["kind"] != "nil" {
		t.Fatalf("Nil description kind = %v, want nil", m["kind"])
	}
	builtinObj := types.Universe.Lookup("println")
	m, err = e.describeObject(builtinObj, false)
	if err != nil {
		t.Fatalf("describeObject(Builtin): %v", err)
	}
	if m["kind"] != "builtin" {
		t.Fatalf("Builtin description kind = %v, want builtin", m["kind"])
	}
}
