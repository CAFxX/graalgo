// Regression tests for the review-1105-v2-objs remediation lane
// (independent codebase review, 2026-11-05).
//
// Each test pins a shape the reviewer proved wrong by parse→check→emit
// probe: consts/type names mis-emitted as vars, phantom vars for struct
// literal field keys, dangling global references, receiverless methods,
// null range variables in the = form, and merged multi-spec var decls.
// The tests are demonstrably capable of failing: each one fails on the
// pre-fix emitter (verified by running them against master@4a494cad).
package emit

import (
	"os"
	"path/filepath"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/parser"
)

// emitTestSrc runs one source file through parse → check → emit.
func emitTestSrc(t *testing.T, src string) *Document {
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

// nodesOf returns every node map of the given kind anywhere in the
// document's Funcs and Globals, in encounter order.
func nodesOf(t *testing.T, doc *Document, kind string) []map[string]any {
	t.Helper()
	var out []map[string]any
	var walk func(v any)
	walk = func(v any) {
		switch n := v.(type) {
		case map[string]any:
			if n["kind"] == kind {
				out = append(out, n)
			}
			for _, c := range n {
				walk(c)
			}
		case []any:
			for _, c := range n {
				walk(c)
			}
		}
	}
	walk(doc.Funcs)
	walk(doc.Globals)
	return out
}

// objectKind returns the kind recorded for the object with the given name,
// or "" if no such object exists.
func objectKind(doc *Document, name string) string {
	for _, o := range doc.Objects {
		if m, ok := o.(map[string]any); ok && m["name"] == name {
			if k, ok := m["kind"].(string); ok {
				return k
			}
		}
	}
	return ""
}

// funcDecls returns the funcdecl nodes, in emission order.
func funcDecls(t *testing.T, doc *Document) []map[string]any {
	t.Helper()
	return nodesOf(t, doc, "funcdecl")
}

// fieldsOf returns a node's fields map.
func fieldsOf(n map[string]any) map[string]any {
	if f, ok := n["fields"].(map[string]any); ok {
		return f
	}
	return nil
}

// basicTypeID returns the type-table id of the basic type with the given
// name, or -1.
func basicTypeID(doc *Document, name string) int {
	for i, tv := range doc.Types {
		m, ok := tv.(map[string]any)
		if !ok || m["kind"] != "basic" {
			continue
		}
		d, ok := m["detail"].(map[string]any)
		if ok && d["name"] == name {
			return i
		}
	}
	return -1
}

// TestObjectsConstAndTypeName: consts and type names are not vars.
// math.Pi and true are *types.Const; int in a conversion int(u) is a
// *types.TypeName; struct literal field keys denote fields, not vars.
// Before the fix all of them came out as {"kind":"var"}.
func TestObjectsConstAndTypeName(t *testing.T) {
	doc := emitTestSrc(t, `package main

import "math"

type Point struct{ X, Y int }

func main() {
	a := math.Pi
	b := true
	var u uint = 3
	c := int(u)
	p := Point{X: 1, Y: 2}
	println(a, b, c, p)
}
`)
	if got := objectKind(doc, "Pi"); got != "const" {
		t.Errorf("Pi object kind = %q, want const", got)
	}
	if got := objectKind(doc, "true"); got != "const" {
		t.Errorf("true object kind = %q, want const", got)
	}
	if got := objectKind(doc, "int"); got != "typename" {
		t.Errorf("int object kind = %q, want typename", got)
	}
	for _, phantom := range []string{"X", "Y"} {
		if got := objectKind(doc, phantom); got != "" {
			t.Errorf("field %s has object kind %q, want no object", phantom, got)
		}
	}
	// The struct literal key is a fieldname node, not an ident with an obj.
	keys := nodesOf(t, doc, "fieldname")
	if len(keys) != 2 {
		t.Fatalf("fieldname nodes = %d, want 2", len(keys))
	}
	names := map[string]bool{}
	for _, k := range keys {
		names[fieldsOf(k)["name"].(string)] = true
	}
	if !names["X"] || !names["Y"] {
		t.Errorf("fieldname names = %v, want X and Y", names)
	}
	// A const records its value.
	for _, o := range doc.Objects {
		m := o.(map[string]any)
		if m["name"] == "true" {
			if m["value"] != "true" {
				t.Errorf("true const value = %v, want true", m["value"])
			}
		}
	}
}

// TestNilAndBuiltinObjects: nil is a nil object, unsafe.Sizeof a builtin.
func TestNilAndBuiltinObjects(t *testing.T) {
	doc := emitTestSrc(t, `package main

import "unsafe"

func main() {
	var p *int = nil
	if p == nil {
		println(unsafe.Sizeof(0))
	}
}
`)
	if got := objectKind(doc, "nil"); got != "nil" {
		t.Errorf("nil object kind = %q, want nil", got)
	}
	if got := objectKind(doc, "Sizeof"); got != "builtin" {
		t.Errorf("Sizeof object kind = %q, want builtin", got)
	}
}

// TestGlobalsEmitted: package-level vars are emitted into Globals with
// their initializers; references to them resolve to those objects.
// Before the fix Globals was always empty while main's idents pointed
// at objects with no definition anywhere.
func TestGlobalsEmitted(t *testing.T) {
	doc := emitTestSrc(t, `package main

const Answer = 42

var GlobalCounter int
var GlobalFlag bool
var Derived = Answer + 1

func main() {
	println(GlobalCounter, GlobalFlag, Derived)
}
`)
	if len(doc.Globals) != 3 {
		t.Fatalf("Globals = %d entries, want 3", len(doc.Globals))
	}
	want := []struct {
		name string
		typ  string
		nval int
	}{
		{"GlobalCounter", "int", 0},
		{"GlobalFlag", "bool", 0},
		{"Derived", "", 1},
	}
	for i, w := range want {
		g, ok := doc.Globals[i].(map[string]any)
		if !ok || g["kind"] != "vardecl" {
			t.Fatalf("Globals[%d] is not a vardecl node: %#v", i, doc.Globals[i])
		}
		f := fieldsOf(g)
		names, _ := f["names"].([]string)
		if len(names) != 1 || names[0] != w.name {
			t.Errorf("Globals[%d] names = %v, want [%s]", i, names, w.name)
		}
		if w.nval == 0 && len(f["values"].([]any)) != 0 {
			t.Errorf("Globals[%d] values = %d, want 0", i, len(f["values"].([]any)))
		}
		if w.nval == 1 && len(f["values"].([]any)) != 1 {
			t.Errorf("Globals[%d] values = %d, want 1", i, len(f["values"].([]any)))
		}
		if w.typ != "" {
			if got, _ := f["type"].(int); got != basicTypeID(doc, w.typ) {
				t.Errorf("Globals[%d] type = %v, want id of %s", i, f["type"], w.typ)
			}
		}
	}
	// A top-level const needs no Globals entry: it is fully described
	// in the object table (kind const, with value).
	for _, g := range doc.Globals {
		if f := fieldsOf(g.(map[string]any)); f != nil {
			for _, n := range f["names"].([]string) {
				if n == "Answer" {
					t.Errorf("const Answer must not be a global var")
				}
			}
		}
	}
	if got := objectKind(doc, "Answer"); got != "const" {
		t.Errorf("Answer object kind = %q, want const", got)
	}
	// The ident in main resolves to the same object id as the global.
	idents := nodesOf(t, doc, "ident")
	found := false
	for _, id := range idents {
		f := fieldsOf(id)
		if f["name"] == "GlobalCounter" {
			found = true
			glob := fieldsOf(doc.Globals[0].(map[string]any))
			objs, _ := glob["objs"].([]any)
			if len(objs) != 1 || objs[0] != f["obj"] {
				t.Errorf("GlobalCounter ident obj = %v, global objs = %v", f["obj"], objs)
			}
		}
	}
	if !found {
		t.Errorf("no ident for GlobalCounter in main")
	}
}

// TestMethodReceiver: methods carry their receiver; same-named methods
// on different types are distinguishable. Plain funcs have a null
// receiver. Before the fix both methods were {name:"Name", params:[]}
// with no receiver at all.
func TestMethodReceiver(t *testing.T) {
	doc := emitTestSrc(t, `package main

type Point struct{ X, Y int }
type Other struct{ N int }

func (p Point) Name() string  { return "p" }
func (o Other) Name() string  { return "o" }
func (Point) Blank() string   { return "b" }
func (p *Point) Ptr() int     { return p.X }
func main() {
	p := Point{}
	println(p.Name(), p.Ptr())
}
`)
	fds := funcDecls(t, doc)
	if len(fds) != 5 {
		t.Fatalf("funcdecls = %d, want 5", len(fds))
	}
	var receivers []map[string]any
	var plain int
	for _, fd := range fds {
		f := fieldsOf(fd)
		if f["receiver"] == nil {
			plain++
			if f["name"] != "main" {
				t.Errorf("func %v has null receiver, want a receiver", f["name"])
			}
			continue
		}
		receivers = append(receivers, fieldsOf(f["receiver"].(map[string]any)))
	}
	if plain != 1 {
		t.Errorf("plain funcs = %d, want 1 (main)", plain)
	}
	if len(receivers) != 4 {
		t.Fatalf("methods with receivers = %d, want 4", len(receivers))
	}
	// The two Name methods have different receiver types.
	recvTypes := map[string]int{}
	for _, r := range receivers {
		recvTypes[r["name"].(string)] = r["type"].(int)
	}
	if recvTypes["p"] == recvTypes["o"] {
		t.Errorf("Point.Name and Other.Name share receiver type %d", recvTypes["p"])
	}
	if recvTypes[""] == 0 && receivers[2]["name"] == "" {
		// Unnamed receiver: name is "" but obj and type are present.
		if receivers[2]["obj"] == nil || receivers[2]["type"] == nil {
			t.Errorf("unnamed receiver lacks obj/type: %v", receivers[2])
		}
	}
	// The pointer receiver's type is a pointer.
	ptrType, _ := doc.Types[recvTypes["p"]].(map[string]any)
	_ = ptrType
	for _, r := range receivers {
		if r["name"] == "p" {
			if t, ok := doc.Types[r["type"].(int)].(map[string]any); ok {
				if t["kind"] == "pointer" {
					return // found the pointer receiver; all checks done
				}
			}
		}
	}
	// The p receiver appears twice (value + pointer); at least one must
	// be a pointer type.
	t.Errorf("no pointer receiver type found among %v", recvTypes)
}

// TestRangeAssignUses: the = form resolves key/value through Info.Uses.
// Before the fix for a, b = range xs emitted key/value null.
func TestRangeAssignUses(t *testing.T) {
	doc := emitTestSrc(t, `package main

func main() {
	xs := []int{1, 2}
	var a, b int
	for a, b = range xs {
		println(a, b)
	}
	for k, v := range xs {
		println(k, v)
	}
	for i := range xs {
		println(i)
	}
}
`)
	ranges := nodesOf(t, doc, "range")
	if len(ranges) != 3 {
		t.Fatalf("range nodes = %d, want 3", len(ranges))
	}
	// = form: key/value are the a/b object ids (non-null).
	f := fieldsOf(ranges[0])
	if f["key"] == nil || f["value"] == nil {
		t.Errorf("= range key/value = %v/%v, want object ids", f["key"], f["value"])
	}
	if f["define"] != false {
		t.Errorf("= range define = %v, want false", f["define"])
	}
	// := form: key/value are object ids too, define true.
	f = fieldsOf(ranges[1])
	if f["key"] == nil || f["value"] == nil {
		t.Errorf(":= range key/value = %v/%v, want object ids", f["key"], f["value"])
	}
	if f["define"] != true {
		t.Errorf(":= range define = %v, want true", f["define"])
	}
	// Key-only form: value stays null.
	f = fieldsOf(ranges[2])
	if f["key"] == nil {
		t.Errorf("key-only range key is null")
	}
	if f["value"] != nil {
		t.Errorf("key-only range value = %v, want null", f["value"])
	}
}

// TestRangeNonIdentTarget: a non-identifier range target is emitted as
// an expression node, like an assignment LHS — not dropped, not null.
func TestRangeNonIdentTarget(t *testing.T) {
	doc := emitTestSrc(t, `package main

func main() {
	xs := []int{1, 2}
	ys := []int{0, 0}
	for ys[0], _ = range xs {
		println(ys[0])
	}
}
`)
	ranges := nodesOf(t, doc, "range")
	if len(ranges) != 1 {
		t.Fatalf("range nodes = %d, want 1", len(ranges))
	}
	f := fieldsOf(ranges[0])
	key, ok := f["key"].(map[string]any)
	if !ok || key["kind"] != "index" {
		t.Errorf("non-ident range key = %#v, want an index node", f["key"])
	}
	if f["value"] != nil {
		t.Errorf("blank range value = %v, want null", f["value"])
	}
}

// TestMultiSpecVarDecl: one vardecl node per ValueSpec, each with its
// own type. Before the fix var (n int=1; t string="x") merged into one
// node with names [n t] and the last spec's type (string) for both.
func TestMultiSpecVarDecl(t *testing.T) {
	doc := emitTestSrc(t, `package main

func main() {
	var (
		n int    = 1
		t string = "x"
	)
	println(n, t)
}
`)
	decls := nodesOf(t, doc, "vardecl")
	if len(decls) != 2 {
		t.Fatalf("vardecl nodes = %d, want 2 (one per spec)", len(decls))
	}
	intID := basicTypeID(doc, "int")
	strID := basicTypeID(doc, "string")
	if intID < 0 || strID < 0 {
		t.Fatalf("basic type ids: int=%d string=%d", intID, strID)
	}
	f0 := fieldsOf(decls[0])
	if names, _ := f0["names"].([]string); len(names) != 1 || names[0] != "n" {
		t.Errorf("first vardecl names = %v, want [n]", f0["names"])
	}
	if got, _ := f0["type"].(int); got != intID {
		t.Errorf("first vardecl type = %v, want int id %d", f0["type"], intID)
	}
	if len(f0["values"].([]any)) != 1 {
		t.Errorf("first vardecl values = %d, want 1", len(f0["values"].([]any)))
	}
	f1 := fieldsOf(decls[1])
	if names, _ := f1["names"].([]string); len(names) != 1 || names[0] != "t" {
		t.Errorf("second vardecl names = %v, want [t]", f1["names"])
	}
	if got, _ := f1["type"].(int); got != strID {
		t.Errorf("second vardecl type = %v, want string id %d", f1["type"], strID)
	}
	if len(f1["values"].([]any)) != 1 {
		t.Errorf("second vardecl values = %d, want 1", len(f1["values"].([]any)))
	}
}

// TestSelectorKindAndIndex: selections record their kind and index.
func TestSelectorKindAndIndex(t *testing.T) {
	doc := emitTestSrc(t, `package main

type Point struct{ X int }

func (p Point) Get() int { return p.X }

func main() {
	p := Point{X: 3}
	f := Point.Get
	println(p.X, p.Get(), f(p))
}
`)
	// Four selectors: p.X in Get's body, p.X in main, Point.Get
	// (method expression), p.Get (method value).
	sels := nodesOf(t, doc, "selector")
	if len(sels) != 4 {
		t.Fatalf("selector nodes = %d, want 4", len(sels))
	}
	want := map[string]string{"X": "field", "Get": "method"}
	seen := map[string]int{}
	for _, s := range sels {
		f := fieldsOf(s)
		name := f["sel"].(string)
		seen[name]++
		if f["selkind"] == nil {
			t.Errorf("selector %s has no selkind", name)
			continue
		}
		if name == "Get" && seen[name] == 1 {
			// First Get in encounter order is the method expression
			// Point.Get (f := Point.Get precedes p.Get() in main).
			if f["selkind"] != "methodexpr" {
				t.Errorf("Point.Get selkind = %v, want methodexpr", f["selkind"])
			}
		} else if w, ok := want[name]; ok && f["selkind"] != w {
			t.Errorf("selector %s selkind = %v, want %s", name, f["selkind"], w)
		}
		idx, ok := f["index"].([]any)
		if !ok || len(idx) != 1 || idx[0] != 0 {
			t.Errorf("selector %s index = %v, want [0]", name, f["index"])
		}
	}
}

// TestUnnamedParams: unnamed parameters are emitted, not dropped.
// Before the fix func Unnamed(int, string) produced params:[].
func TestUnnamedParams(t *testing.T) {
	doc := emitTestSrc(t, `package main

func Unnamed(int, string) {}
func Blank(_ int, s string) string { return s }

func main() {
	Unnamed(1, "x")
	println(Blank(1, "y"))
}
`)
	fds := funcDecls(t, doc)
	if len(fds) != 3 {
		t.Fatalf("funcdecls = %d, want 3", len(fds))
	}
	f := fieldsOf(fds[0])
	params, _ := f["params"].([]any)
	if len(params) != 2 {
		t.Fatalf("Unnamed params = %d, want 2", len(params))
	}
	intID := basicTypeID(doc, "int")
	strID := basicTypeID(doc, "string")
	p0 := fieldsOf(params[0].(map[string]any))
	if p0["name"] != "" || p0["type"] != intID {
		t.Errorf("Unnamed param 0 = name %v type %v, want \"\" and int %d", p0["name"], p0["type"], intID)
	}
	p1 := fieldsOf(params[1].(map[string]any))
	if p1["name"] != "" || p1["type"] != strID {
		t.Errorf("Unnamed param 1 = name %v type %v, want \"\" and string %d", p1["name"], p1["type"], strID)
	}
	// The blank parameter is emitted too.
	f = fieldsOf(fds[1])
	params, _ = f["params"].([]any)
	if len(params) != 2 {
		t.Fatalf("Blank params = %d, want 2", len(params))
	}
}

// TestCallHasNoEllipsis: the call node carries no ellipsis field
// (variadic spread is rejected loudly before emission, so the field was
// always false).
func TestCallHasNoEllipsis(t *testing.T) {
	doc := emitTestSrc(t, `package main

func main() {
	println("x")
}
`)
	for _, c := range nodesOf(t, doc, "call") {
		if _, ok := fieldsOf(c)["ellipsis"]; ok {
			t.Errorf("call node still has ellipsis field: %v", fieldsOf(c))
		}
	}
}
