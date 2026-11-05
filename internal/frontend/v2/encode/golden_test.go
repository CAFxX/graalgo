// Golden-document tests for the v2 emitter (review-2026-11-05-13,
// A-P1-10, the masking test).
//
// TestEncodeAllKinds runs a 200-line program through parse → check → emit
// → encode and asserts only "non-empty, starts with magic". All nine
// silent emitter mis-emissions A-P1-1–A-P1-9 pass straight through it.
// These tests assert on the emitted *Document* instead: the type classes,
// object kinds, globals, receivers, range targets, per-spec var types,
// struct offsets, and method sets the emitter produces.
//
// Status: TEST-FIRST BY DESIGN. Every subtest below FAILS on master
// 4a494cad (red) and is expected to pass once the sibling emitter lanes
// land (review-1105-v2-types: A-P1-1, A-P1-2, A-P1-8, A-P1-9;
// review-1105-v2-objs: A-P1-3–A-P1-7) and this branch is rebased onto
// them. Integration order is emitter lanes first, then this lane.
//
// Contract notes (what the sibling lanes must emit; the test pins the
// semantics, and where no existing consumer pins a field name it accepts
// the documented spellings):
//   - Basic classes follow the vocabulary the Java backend already
//     consumes (GoKind.fromBasic): "bool", "int", "uint", "float",
//     "complex", "string", "pointer" (unsafe.Pointer only). "unknown"
//     must never appear as a basic class.
//   - Named/interface method-set entries are {"name", "obj", "recv"}
//     with "recv" ∈ {"value", "pointer"}, matching v1 (export.go) and
//     the Java IfaceMethodTables consumer.
//   - Struct field offsets are real byte offsets for the target
//     platform, computed from go/types sizes (never 0 placeholders).
//   - A method funcdecl carries its receiver as fields["receiver"]
//     (a param node, per the objs lane, or a plain map) or as
//     fields["recv"] plus the receiver as params[0]; a plain func
//     carries none. The receiver's name and type must be recoverable,
//     and pointer-ness from an explicit marker or the receiver type.
//   - Object kinds are "var" / "func" / "const" / "typename" (the
//     objs lane's spelling for *types.TypeName).
//   - A package-level var appears in doc.Globals (entry shape is the
//     objs lane's choice; the test finds it by name) with its initializer.
//   - An unhandled types.Type must fail LOUDLY (Emit error) — never a
//     silent {"kind": "basic", "class": "unknown"} entry. One exception
//     is pinned separately: void calls must EMIT (their empty-tuple type
//     must not produce an "unknown" entry and must not error — every real
//     program calls void functions).
package encode

import (
	"go/types"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/emit"
	"graalgo-frontend-v2/parser"
)

// emitProgram runs src through parse → check → emit and returns the
// document or the emit error. Parse/check failures are test failures:
// they are not emitter behavior.
func emitProgram(t *testing.T, src string) (*emit.Document, error) {
	t.Helper()
	dir := t.TempDir()
	fn := filepath.Join(dir, "golden.go")
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
	return emit.Emit(cres, pres.Pkg, pres.Files)
}

// mustEmit is emitProgram with an emit error as a test failure.
func mustEmit(t *testing.T, src string) *emit.Document {
	t.Helper()
	doc, err := emitProgram(t, src)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	return doc
}

// asInt converts the integer forms that appear in a Document
// (int ids, int64 counts) to int.
func asInt(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case int32:
		return int(x), true
	}
	return 0, false
}

func asString(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}

// asStringList converts a string-list field (stored as []string or []any)
// to []string.
func asStringList(v any) []string {
	switch x := v.(type) {
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// nodeFields returns n["fields"] as a map, or nil.
func nodeFields(n map[string]any) map[string]any {
	f, _ := n["fields"].(map[string]any)
	return f
}

// typeEntries returns the type table as a slice of entry maps.
func typeEntries(doc *emit.Document) []map[string]any {
	out := make([]map[string]any, 0, len(doc.Types))
	for _, tv := range doc.Types {
		if m, ok := tv.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// typeByID returns the type-table entry for id, or nil.
func typeByID(doc *emit.Document, id int) map[string]any {
	if id < 0 || id >= len(doc.Types) {
		return nil
	}
	m, _ := doc.Types[id].(map[string]any)
	return m
}

// basicClass returns the class of the basic type named name, or ("", false).
func basicClass(doc *emit.Document, name string) (string, bool) {
	for _, e := range typeEntries(doc) {
		kind, _ := e["kind"].(string)
		if kind != "basic" {
			continue
		}
		detail, _ := e["detail"].(map[string]any)
		if detail == nil {
			continue
		}
		if n, _ := detail["name"].(string); n == name {
			c, _ := detail["class"].(string)
			return c, true
		}
	}
	return "", false
}

// objectByName returns the object-table entry named name, or nil.
func objectByName(doc *emit.Document, name string) map[string]any {
	for _, ov := range doc.Objects {
		m, ok := ov.(map[string]any)
		if !ok {
			continue
		}
		if n, _ := m["name"].(string); n == name {
			return m
		}
	}
	return nil
}

// collectNodes walks root (Document fields, node maps, lists) and returns
// every node map whose "kind" equals kind.
func collectNodes(root any, kind string) []map[string]any {
	var out []map[string]any
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if k, _ := x["kind"].(string); k == kind {
				out = append(out, x)
			}
			for _, w := range x {
				walk(w)
			}
		case []any:
			for _, e := range x {
				walk(e)
			}
		}
	}
	walk(root)
	return out
}

// findMapsNamed returns every map anywhere under root containing
// "name" == name.
func findMapsNamed(root any, name string) []map[string]any {
	var out []map[string]any
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if n, _ := x["name"].(string); n == name {
				out = append(out, x)
			}
			for _, w := range x {
				walk(w)
			}
		case []any:
			for _, e := range x {
				walk(e)
			}
		}
	}
	walk(root)
	return out
}

// containsBasicLit reports whether any basiclit node under root has the
// literal source text value.
func containsBasicLit(root any, value string) bool {
	for _, n := range collectNodes(root, "basiclit") {
		if f := nodeFields(n); f != nil {
			if v, _ := f["value"].(string); v == value {
				return true
			}
		}
	}
	return false
}

// globalEntries returns every entry in doc.Globals that names the
// package-level variable name, in either of the plausible shapes: a
// plain map carrying "name", or a vardecl-shaped node whose
// fields.names list contains it.
func globalEntries(doc *emit.Document, name string) []map[string]any {
	var out []map[string]any
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if n, _ := x["name"].(string); n == name {
				out = append(out, x)
			}
			if f := nodeFields(x); f != nil {
				for _, ns := range asStringList(f["names"]) {
					if ns == name {
						out = append(out, x)
						break
					}
				}
			}
			for _, w := range x {
				walk(w)
			}
		case []any:
			for _, e := range x {
				walk(e)
			}
		}
	}
	walk(doc.Globals)
	return out
}

// namedTypeID returns the type-table id of the named type whose
// detail.name ends with "."+name (e.g. "main.Point").
func namedTypeID(doc *emit.Document, name string) (int, bool) {
	for i, e := range typeEntries(doc) {
		kind, _ := e["kind"].(string)
		if kind != "named" {
			continue
		}
		detail, _ := e["detail"].(map[string]any)
		if detail == nil {
			continue
		}
		if n, _ := detail["name"].(string); strings.HasSuffix(n, "."+name) || n == name {
			return i, true
		}
	}
	return 0, false
}

// methodNames returns the "name" of every entry in a type detail's
// "methods" list (nil + an error if the field is absent or malformed,
// so one missing method set does not abort the rest of the test).
func methodNames(t *testing.T, detail map[string]any) []string {
	t.Helper()
	raw, ok := detail["methods"]
	if !ok {
		t.Errorf("type detail has no \"methods\" field: %#v", detail)
		return nil
	}
	list, ok := raw.([]any)
	if !ok {
		t.Errorf("\"methods\" is %T, want []any", raw)
		return nil
	}
	var names []string
	for _, e := range list {
		m, ok := e.(map[string]any)
		if !ok {
			t.Fatalf("method entry is %T, want map[string]any", e)
		}
		n, _ := m["name"].(string)
		names = append(names, n)
	}
	return names
}

// methodEntry returns the method-set entry named name, or nil.
func methodEntry(detail map[string]any, name string) map[string]any {
	list, _ := detail["methods"].([]any)
	for _, e := range list {
		if m, ok := e.(map[string]any); ok {
			if n, _ := m["name"].(string); n == name {
				return m
			}
		}
	}
	return nil
}

// receiverInfo extracts the receiver from a funcdecl node. Accepted
// forms: fields["receiver"] as a param node (the objs lane's shape:
// {"kind": "param", "fields": {"name", "obj", "type"}}) or as a plain
// map {name, type[, pointer|obj]}, or fields["recv"]
// ("value"/"pointer", or a map) together with the receiver as params[0].
// Pointer-ness comes from an explicit marker when present, else from
// the receiver's type-table entry (a "pointer" entry means a pointer
// receiver). Returns found=false for a plain function.
func receiverInfo(doc *emit.Document, fd map[string]any) (name string, typeID int, pointer bool, found bool) {
	fields := nodeFields(fd)
	if fields == nil {
		return "", 0, false, false
	}
	// Resolve an object id to its type-table id.
	objType := func(oid int) int {
		if oid < 0 || oid >= len(doc.Objects) || doc.Objects[oid] == nil {
			return -1
		}
		obj, _ := doc.Objects[oid].(map[string]any)
		if obj == nil {
			return -1
		}
		id, _ := asInt(obj["type"])
		return id
	}
	// A param node carries its payload under "fields"; a plain map
	// carries it directly.
	normalize := func(m map[string]any) map[string]any {
		if k, _ := m["kind"].(string); k == "param" {
			if f := nodeFields(m); f != nil {
				return f
			}
		}
		return m
	}
	pointerFrom := func(m map[string]any, tid int) bool {
		if p, ok := m["pointer"].(bool); ok {
			return p
		}
		if r, ok := m["recv"].(string); ok {
			return r == "pointer"
		}
		if e := typeByID(doc, tid); e != nil {
			if k, _ := e["kind"].(string); k == "pointer" {
				return true
			}
		}
		return false
	}
	read := func(m map[string]any) (string, int) {
		n, _ := m["name"].(string)
		if id, ok := asInt(m["type"]); ok {
			return n, id
		}
		if oid, ok := asInt(m["obj"]); ok {
			return n, objType(oid)
		}
		return n, -1
	}
	if rv, ok := fields["receiver"].(map[string]any); ok && rv != nil {
		m := normalize(rv)
		name, typeID = read(m)
		return name, typeID, pointerFrom(m, typeID), true
	}
	if rv, ok := fields["recv"]; ok && rv != nil {
		if m, ok := rv.(map[string]any); ok {
			m = normalize(m)
			name, typeID = read(m)
			return name, typeID, pointerFrom(m, typeID), true
		}
		if str, ok := rv.(string); ok {
			pointer = str == "pointer"
			// Receiver is params[0].
			if params, ok := fields["params"].([]any); ok && len(params) > 0 {
				if p0, ok := params[0].(map[string]any); ok {
					pf := nodeFields(p0)
					name, typeID = read(pf)
					if !pointer {
						pointer = pointerFrom(pf, typeID)
					}
				}
			}
			return name, typeID, pointer, true
		}
	}
	return "", 0, false, false
}

// TestGoldenBasicClasses pins A-P1-1: every *types.Basic kind maps to the
// class the Java backend consumes (GoKind.fromBasic). On master, bool is
// "unknown" and every unsigned int (plus byte/uintptr) is "int".
func TestGoldenBasicClasses(t *testing.T) {
	src := `package main

import "unsafe"

const (
	CI = 1
	CF = 1.5
	CS = "s"
	CB = true
	CC = 1 + 2i
	CR = 'a'
)

func main() {
	var b bool = true
	var i int = 1
	var i8 int8 = 1
	var i16 int16 = 1
	var i32 int32 = 1
	var i64 int64 = 1
	var u uint = 1
	var u8 uint8 = 1
	var u16 uint16 = 1
	var u32 uint32 = 1
	var u64 uint64 = 1
	var up uintptr = 1
	var f32 float32 = 1.5
	var f64 float64 = 1.5
	var c64 complex64 = 1 + 2i
	var c128 complex128 = 1 + 2i
	var s string = "x"
	var by byte = 1
	var r rune = 'a'
	var p unsafe.Pointer
	println(b, i, i8, i16, i32, i64, u, u8, u16, u32, u64, up, f32, f64, c64, c128, s, by, r)
	println(CI, CF, CS, CB, CC, CR)
	_ = p
}
`
	doc := mustEmit(t, src)
	// name → expected class, per GoKind.fromBasic. "Pointer" is the
	// go/types name of unsafe.Pointer (types.UnsafePointer.Name()).
	want := map[string]string{
		"bool": "bool", "int": "int", "int8": "int", "int16": "int",
		"int32": "int", "int64": "int",
		"uint": "uint", "uint8": "uint", "uint16": "uint",
		"uint32": "uint", "uint64": "uint", "uintptr": "uint",
		"float32": "float", "float64": "float",
		"complex64": "complex", "complex128": "complex",
		"string":      "string",
		"Pointer":     "pointer",
		"untyped int": "int", "untyped float": "float",
		"untyped string": "string", "untyped bool": "bool",
		"untyped complex": "complex", "untyped rune": "int",
	}
	for name, class := range want {
		got, ok := basicClass(doc, name)
		if !ok {
			t.Errorf("basic type %q: no entry in type table", name)
			continue
		}
		if got != class {
			t.Errorf("basic type %q: class = %q, want %q", name, got, class)
		}
	}
}

// allowedBasicPairs is the (name, class) vocabulary GoKind.fromBasic
// accepts. Any basic entry outside it is a silent mis-emission.
var allowedBasicPairs = map[string]map[string]bool{
	"int":     {"int": true, "int8": true, "int16": true, "int32": true, "int64": true, "rune": true, "untyped int": true, "untyped rune": true},
	"uint":    {"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true, "uintptr": true, "byte": true, "untyped int": true, "untyped rune": true},
	"float":   {"float32": true, "float64": true, "untyped float": true},
	"complex": {"complex64": true, "complex128": true, "untyped complex": true},
	"bool":    {"bool": true, "untyped bool": true},
	"string":  {"string": true, "untyped string": true},
	"pointer": {"Pointer": true},
}

// assertNoSilentUnknownTypes fails if the type table contains any
// {"kind": "basic", "class": "unknown"} entry or any basic entry whose
// (name, class) the backend cannot consume.
func assertNoSilentUnknownTypes(t *testing.T, doc *emit.Document) {
	t.Helper()
	for i, e := range typeEntries(doc) {
		kind, _ := e["kind"].(string)
		if kind != "basic" {
			continue
		}
		detail, _ := e["detail"].(map[string]any)
		if detail == nil {
			t.Errorf("types[%d]: basic entry without detail", i)
			continue
		}
		name, _ := detail["name"].(string)
		class, _ := detail["class"].(string)
		if class == "unknown" {
			t.Errorf("types[%d]: basic type %q has class \"unknown\" (silent mis-emission)", i, name)
			continue
		}
		names, ok := allowedBasicPairs[class]
		if !ok || !names[name] {
			t.Errorf("types[%d]: basic type %q has class %q, which GoKind.fromBasic cannot consume", i, name, class)
		}
	}
}

// TestGoldenNoSilentUnknownTypes pins A-P1-2: unhandled types.Type kinds
// (TypeParam, Alias, Union, Tuple) must fail LOUDLY or be handled —
// never a silent {"kind": "basic", "class": "unknown"} entry. Either a
// loud Emit error or a clean type table passes; the silent entry fails.
func TestGoldenNoSilentUnknownTypes(t *testing.T) {
	src := `package main

type A = int

type Signed interface {
	~int | ~int64
}

func id[T any](x T) T { return x }

func use[T Signed](x T) T { return x }

func main() {
	println(id(1), A(2), use(3))
}
`
	doc, err := emitProgram(t, src)
	if err != nil {
		// Loud rejection is an acceptable fix for unhandled types.
		t.Logf("emit rejected the program loudly: %v", err)
		return
	}
	assertNoSilentUnknownTypes(t, doc)
}

// TestGoldenVoidCallType pins the A-P1-2 boundary: a call to a void
// function has the empty-tuple type "()". The emitter must EMIT it (or
// omit the type) — erroring here would make the emitter unusable, since
// every real program calls void functions. On master the empty tuple
// lands as {"kind": "basic", "class": "unknown", "name": "()"}.
func TestGoldenVoidCallType(t *testing.T) {
	src := `package main

func say(s string) {}

func main() {
	say("hi")
	println("done")
}
`
	doc := mustEmit(t, src)
	assertNoSilentUnknownTypes(t, doc)
}

// TestGoldenObjectKinds pins A-P1-3: constants are "const", type names
// are "typename" — never "var". Phantom objects must not exist: the
// predeclared true, the conversion target int, and struct-literal field
// keys X/Y are not variables.
func TestGoldenObjectKinds(t *testing.T) {
	src := `package main

import "math"

const K = 42
const S = "hi"

type T int

type Point struct {
	X int
	Y int
}

func main() {
	println(K, S, int(T(1)), math.Pi, true)
	p := Point{X: 1, Y: 2}
	q := new(Point)
	println(p, q)
}
`
	doc := mustEmit(t, src)
	// The TypeName spelling is "typename" (the objs lane's choice,
	// pinned by its own test; "type" would collide with the "type"
	// field name carried by every node).
	wantKind := map[string]string{
		"K": "const", "S": "const", "T": "typename", "Point": "typename",
		"Pi": "const", "true": "const", "int": "typename", "p": "var",
	}
	for name, kind := range wantKind {
		obj := objectByName(doc, name)
		if obj == nil {
			t.Errorf("object %q: missing from object table", name)
			continue
		}
		if got, _ := obj["kind"].(string); got != kind {
			t.Errorf("object %q: kind = %q, want %q", name, got, kind)
		}
	}
	// Struct-literal field keys must not be declared as objects at all.
	for _, phantom := range []string{"X", "Y"} {
		if obj := objectByName(doc, phantom); obj != nil {
			t.Errorf("phantom object %q exists (kind %q): field idents in keyed literals must not be declared",
				phantom, obj["kind"])
		}
	}
}

// TestGoldenGlobals pins A-P1-4: package-level vars are emitted in
// doc.Globals with their initializers, and every object id referenced
// from function bodies resolves to a real object-table entry.
func TestGoldenGlobals(t *testing.T) {
	src := `package main

var G int = 7
var H string = "h"
var Multi1, Multi2 int = 1, 2

const K = 42

func main() {
	println(G, H, Multi1, Multi2, K)
}
`
	doc := mustEmit(t, src)
	if len(doc.Globals) == 0 {
		t.Fatalf("doc.Globals is empty: package-level vars are silently dropped (A-P1-4)")
	}
	// Each package-level var must appear in Globals (by name) with its
	// initializer literal present in the same entry's subtree. When one
	// entry covers several names (a grouped spec), the values must line
	// up positionally.
	wantInit := map[string]string{"G": "7", "H": `"h"`, "Multi1": "1", "Multi2": "2"}
	for name, lit := range wantInit {
		entries := globalEntries(doc, name)
		if len(entries) == 0 {
			t.Errorf("global %q: no entry in doc.Globals names it", name)
			continue
		}
		found := false
		for _, e := range entries {
			// A vardecl-shaped entry carries names/values in fields;
			// when one entry covers several names, the values must
			// line up positionally.
			if f := nodeFields(e); f != nil {
				if names := asStringList(f["names"]); names != nil {
					if values, ok := f["values"].([]any); ok && len(names) == len(values) {
						for i, ns := range names {
							if ns == name {
								if vn, ok := values[i].(map[string]any); ok {
									if vf := nodeFields(vn); vf != nil {
										if v, _ := vf["value"].(string); v == lit {
											found = true
										}
									}
								}
							}
						}
						continue
					}
				}
			}
			if containsBasicLit(e, lit) {
				found = true
			}
		}
		if !found {
			t.Errorf("global %q: entry in doc.Globals has no initializer literal %s", name, lit)
		}
		// The global's object must exist with kind "var".
		obj := objectByName(doc, name)
		if obj == nil {
			t.Errorf("global %q: no object-table entry", name)
		} else if kind, _ := obj["kind"].(string); kind != "var" {
			t.Errorf("global %q: object kind = %q, want \"var\"", name, kind)
		}
	}
	// No dangling object references: every "obj" id in the function
	// bodies must resolve to a non-nil object-table entry.
	var check func(v any)
	check = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if oid, ok := asInt(x["obj"]); ok {
				if oid < 0 || oid >= len(doc.Objects) || doc.Objects[oid] == nil {
					t.Errorf("dangling object reference: obj id %d has no object-table entry", oid)
				}
			}
			for _, w := range x {
				check(w)
			}
		case []any:
			for _, e := range x {
				check(e)
			}
		}
	}
	check(doc.Funcs)
}

// TestGoldenMethodReceivers pins A-P1-5: methods are emitted with their
// receiver; two methods with the same name on different types stay
// distinguishable; plain functions carry no receiver.
func TestGoldenMethodReceivers(t *testing.T) {
	src := `package main

type Point struct {
	X, Y int
}

func (p Point) Name() string { _ = p; return "point" }
func (p *Point) Scale(n int) { p.X *= n; p.Y *= n }

type Rect struct {
	W, H int
}

func (r Rect) Name() string { _ = r; return "rect" }

func plain(x int) int { return x }

func main() {
	p := Point{1, 2}
	println(p.Name(), plain(1))
	p.Scale(3)
	r := Rect{1, 2}
	println(r.Name())
}
`
	doc := mustEmit(t, src)
	pointID, ok := namedTypeID(doc, "Point")
	if !ok {
		t.Fatal("named type Point missing from type table")
	}
	rectID, ok := namedTypeID(doc, "Rect")
	if !ok {
		t.Fatal("named type Rect missing from type table")
	}
	// The pointer-to-Point type id (receiver type of Scale).
	ptrPointID := -1
	for i, e := range typeEntries(doc) {
		kind, _ := e["kind"].(string)
		if kind != "pointer" {
			continue
		}
		detail, _ := e["detail"].(map[string]any)
		if detail == nil {
			continue
		}
		if elem, ok := asInt(detail["elem"]); ok && elem == pointID {
			ptrPointID = i
		}
	}

	decls := collectNodes(doc.Funcs, "funcdecl")
	var names []map[string]any
	var scales []map[string]any
	var plains []map[string]any
	for _, d := range decls {
		f := nodeFields(d)
		if f == nil {
			continue
		}
		switch n, _ := f["name"].(string); n {
		case "Name":
			names = append(names, d)
		case "Scale":
			scales = append(scales, d)
		case "plain":
			plains = append(plains, d)
		}
	}
	if len(names) != 2 {
		t.Fatalf("want 2 funcdecls named \"Name\" (Point.Name, Rect.Name), got %d (same-named methods must not collide)", len(names))
	}
	seenTypes := map[int]bool{}
	for _, d := range names {
		name, typeID, pointer, found := receiverInfo(doc, d)
		if !found {
			t.Errorf("funcdecl Name: no receiver info (fields lack \"receiver\"/\"recv\")")
			continue
		}
		if pointer {
			t.Errorf("funcdecl Name: pointer receiver, want value receiver")
		}
		if typeID != pointID && typeID != rectID {
			t.Errorf("funcdecl Name: receiver type id %d, want Point (%d) or Rect (%d)", typeID, pointID, rectID)
		}
		if name != "p" && name != "r" {
			t.Errorf("funcdecl Name: receiver name %q, want \"p\" or \"r\"", name)
		}
		seenTypes[typeID] = true
	}
	if len(seenTypes) != 2 {
		t.Errorf("the two Name methods must have different receiver types, got %v", seenTypes)
	}
	if len(scales) != 1 {
		t.Fatalf("want 1 funcdecl named \"Scale\", got %d", len(scales))
	}
	{
		name, typeID, pointer, found := receiverInfo(doc, scales[0])
		if !found {
			t.Errorf("funcdecl Scale: no receiver info")
		} else {
			if name != "p" {
				t.Errorf("funcdecl Scale: receiver name %q, want \"p\"", name)
			}
			if !pointer {
				t.Errorf("funcdecl Scale: receiver must be a pointer receiver")
			}
			if typeID != ptrPointID && typeID != pointID {
				t.Errorf("funcdecl Scale: receiver type id %d, want *Point (%d) or Point (%d)", typeID, ptrPointID, pointID)
			}
		}
	}
	if len(plains) != 1 {
		t.Fatalf("want 1 funcdecl named \"plain\", got %d", len(plains))
	}
	if _, _, _, found := receiverInfo(doc, plains[0]); found {
		t.Errorf("plain func \"plain\" carries receiver info; only methods have receivers")
	}
	// Receiver variables must be declared objects of the right type.
	for _, recv := range []string{"p", "r"} {
		obj := objectByName(doc, recv)
		if obj == nil {
			t.Errorf("receiver var %q: no object-table entry", recv)
		}
	}
}

// TestGoldenRangeAssign pins A-P1-6: range key/value are the declared
// objects in BOTH the := and = forms. On master the = form emits
// key/value null.
func TestGoldenRangeAssign(t *testing.T) {
	src := `package main

func main() {
	nums := []int{1, 2, 3}
	var k, v int
	for k, v = range nums {
		println(k, v)
	}
	for i, x := range nums {
		println(i, x)
	}
	m := map[string]int{"a": 1}
	var mk string
	var mv int
	for mk, mv = range m {
		println(mk, mv)
	}
	for j := range nums {
		println(j)
	}
	for k = range nums {
		println(k)
	}
}
`
	doc := mustEmit(t, src)
	ranges := collectNodes(doc.Funcs, "range")
	if len(ranges) != 5 {
		t.Fatalf("want 5 range nodes, got %d", len(ranges))
	}
	// Order is source order: (k,v =), (i,x :=), (mk,mv =), (j :=), (k =).
	want := []struct {
		key, value string // "" means the slot must be absent/nil
		define     bool
	}{
		{"k", "v", false},
		{"i", "x", true},
		{"mk", "mv", false},
		{"j", "", true},
		{"k", "", false},
	}
	for idx, w := range want {
		f := nodeFields(ranges[idx])
		if f == nil {
			t.Fatalf("range[%d]: no fields", idx)
		}
		define, _ := f["define"].(bool)
		if define != w.define {
			t.Errorf("range[%d]: define = %v, want %v", idx, define, w.define)
		}
		checkSlot := func(slot, wantName string) {
			raw, present := f[slot]
			if wantName == "" {
				if present && raw != nil {
					t.Errorf("range[%d]: %s = %v, want absent/nil", idx, slot, raw)
				}
				return
			}
			if !present || raw == nil {
				t.Errorf("range[%d]: %s is null, want object id of %q (= form drops the target)", idx, slot, wantName)
				return
			}
			oid, ok := asInt(raw)
			if !ok {
				t.Errorf("range[%d]: %s = %T, want object id (int)", idx, slot, raw)
				return
			}
			if oid < 0 || oid >= len(doc.Objects) || doc.Objects[oid] == nil {
				t.Errorf("range[%d]: %s obj id %d has no object-table entry", idx, slot, oid)
				return
			}
			obj := doc.Objects[oid].(map[string]any)
			if n, _ := obj["name"].(string); n != wantName {
				t.Errorf("range[%d]: %s names object %q, want %q", idx, slot, n, wantName)
			}
		}
		checkSlot("key", w.key)
		checkSlot("value", w.value)
	}
}

// TestGoldenGroupedVar pins A-P1-7: one vardecl node per ValueSpec, each
// with its own type. On master the grouped block merges into one node
// whose single type (the last spec's) is attributed to every name.
func TestGoldenGroupedVar(t *testing.T) {
	src := `package main

func main() {
	var (
		n int    = 1
		t string = "x"
		b bool   = true
	)
	println(n, t, b)
	var a int = 1
	var s string = "y"
	println(a, s)
}
`
	doc := mustEmit(t, src)
	decls := collectNodes(doc.Funcs, "vardecl")
	byName := map[string]map[string]any{}
	for _, d := range decls {
		f := nodeFields(d)
		if f == nil {
			continue
		}
		for _, ns := range asStringList(f["names"]) {
			if _, dup := byName[ns]; dup {
				t.Errorf("var %q appears in two vardecl nodes (specs must not be merged or duplicated)", ns)
			}
			byName[ns] = d
		}
	}
	for name, wantTypeName := range map[string]string{
		"n": "int", "t": "string", "b": "bool", "a": "int", "s": "string",
	} {
		d, ok := byName[name]
		if !ok {
			t.Errorf("var %q: no vardecl node names it", name)
			continue
		}
		f := nodeFields(d)
		names := asStringList(f["names"])
		if len(names) != 1 {
			t.Errorf("var %q: its vardecl node names %v, want exactly [%q] (one node per spec)", name, names, name)
		}
		nodeType, ok := asInt(f["type"])
		if !ok {
			t.Errorf("var %q: vardecl node has no integer type id (type = %#v)", name, f["type"])
			continue
		}
		// Ground truth: the object's own type.
		obj := objectByName(doc, name)
		if obj == nil {
			t.Errorf("var %q: no object-table entry", name)
			continue
		}
		wantType, ok := asInt(obj["type"])
		if !ok {
			t.Errorf("var %q: object has no integer type id", name)
			continue
		}
		if nodeType != wantType {
			wantEntry := typeByID(doc, wantType)
			gotEntry := typeByID(doc, nodeType)
			t.Errorf("var %q: vardecl node type = %v, want %v (declared %s)", name, gotEntry, wantEntry, wantTypeName)
		}
	}
}

// TestGoldenStructOffsets pins A-P1-8: struct field offsets are real byte
// offsets for the target platform. On master every offset is 0.
func TestGoldenStructOffsets(t *testing.T) {
	src := `package main

type S struct {
	A int8
	B int64
	C int8
	D string
	E bool
	F float64
	G [3]int32
	H *int
}

func main() {
	s := S{}
	println(s)
}
`
	doc := mustEmit(t, src)
	sizes := types.SizesFor("gc", runtime.GOARCH)
	if sizes == nil {
		t.Fatalf("no go/types sizes for gc/%s", runtime.GOARCH)
	}
	// Expected offsets from go/types itself, for the field types in
	// source order. Written out so a drift is visible in the failure.
	var (
		tInt8    = types.Typ[types.Int8]
		tInt64   = types.Typ[types.Int64]
		tString  = types.Typ[types.String]
		tBool    = types.Typ[types.Bool]
		tFloat64 = types.Typ[types.Float64]
		tArr     = types.NewArray(types.Typ[types.Int32], 3)
		tPtr     = types.NewPointer(types.Typ[types.Int])
	)
	fieldTypes := []types.Type{tInt8, tInt64, tInt8, tString, tBool, tFloat64, tArr, tPtr}
	fieldNames := []string{"A", "B", "C", "D", "E", "F", "G", "H"}
	vars := make([]*types.Var, len(fieldTypes))
	for i, ft := range fieldTypes {
		vars[i] = types.NewVar(0, nil, fieldNames[i], ft)
	}
	wantOffsets := sizes.Offsetsof(vars)

	structEntry, ok := func() (map[string]any, bool) {
		for _, e := range typeEntries(doc) {
			kind, _ := e["kind"].(string)
			if kind != "struct" {
				continue
			}
			return e, true
		}
		return nil, false
	}()
	if !ok {
		t.Fatal("no struct entry in type table")
	}
	detail, _ := structEntry["detail"].(map[string]any)
	fields, _ := detail["fields"].([]any)
	if len(fields) != len(fieldTypes) {
		t.Fatalf("struct has %d fields, want %d", len(fields), len(fieldTypes))
	}
	for i, fv := range fields {
		f, ok := fv.(map[string]any)
		if !ok {
			t.Fatalf("struct field %d is %T, want map", i, fv)
		}
		name, _ := f["name"].(string)
		if name != fieldNames[i] {
			t.Errorf("struct field %d: name = %q, want %q", i, name, fieldNames[i])
		}
		off, ok := asInt(f["offset"])
		if !ok {
			t.Errorf("struct field %s: no integer offset (offset = %#v)", name, f["offset"])
			continue
		}
		if off != int(wantOffsets[i]) {
			t.Errorf("struct field %s: offset = %d, want %d", name, off, wantOffsets[i])
		}
	}
}

// TestGoldenMethodSets pins A-P1-9: named types, pointer types, and
// interfaces carry their method sets. The contract is v1's
// (docs/arch/frontend-v1.md): the NAMED entry carries
// types.NewMethodSet(T) — value receivers only — while the POINTER
// entry carries types.NewMethodSet(*T) — value AND pointer receivers.
// A Circle value must NOT claim Greet (it does not implement Greeter;
// only *Circle does), so Greet belongs on *Circle, not on Circle.
// Entries are {"name", "obj", "recv"} with recv ∈ {"value", "pointer"}
// (v1 / Java IfaceMethodTables vocabulary); interface entries carry at
// least "name".
func TestGoldenMethodSets(t *testing.T) {
	src := `package main

type Shaper interface {
	Area() float64
	Name() string
}

type Greeter interface {
	Shaper
	Greet() string
}

type Circle struct {
	R float64
}

func (c Circle) Area() float64 { return 3.14 * c.R * c.R }
func (c Circle) Name() string  { return "circle" }
func (c *Circle) Greet() string { return "hi" }

func main() {
	var s Shaper = Circle{R: 1}
	println(s.Area(), s.Name())
	var g Greeter = &Circle{R: 2}
	println(g.Greet())
}
`
	doc := mustEmit(t, src)
	circleID, ok := namedTypeID(doc, "Circle")
	if !ok {
		t.Fatal("named type Circle missing from type table")
	}
	circle := typeByID(doc, circleID)
	detail, _ := circle["detail"].(map[string]any)
	if detail == nil {
		t.Fatalf("Circle type entry has no detail: %#v", circle)
	}
	// The named entry carries NewMethodSet(Circle): value receivers
	// only. Greet (pointer receiver) must NOT be listed here.
	if got := methodNames(t, detail); !sameSet(got, []string{"Area", "Name"}) {
		t.Errorf("Circle methods = %v, want [Area Name] (NewMethodSet(T): value receivers only)", got)
	}
	if e := methodEntry(detail, "Greet"); e != nil {
		t.Errorf("Circle methods list Greet, a pointer-receiver method: it belongs on *Circle, not on Circle (a Circle value does not implement Greeter)")
	}
	checkNamedMethod := func(detail map[string]any, typeDesc, name, wantRecv string) {
		e := methodEntry(detail, name)
		if e == nil {
			t.Errorf("%s methods: missing %q", typeDesc, name)
			return
		}
		if recv, _ := e["recv"].(string); recv != wantRecv {
			t.Errorf("%s method %q: recv = %q, want %q", typeDesc, name, recv, wantRecv)
		}
		oid, ok := asInt(e["obj"])
		if !ok {
			t.Errorf("%s method %q: no integer obj id (obj = %#v)", typeDesc, name, e["obj"])
			return
		}
		if oid < 0 || oid >= len(doc.Objects) || doc.Objects[oid] == nil {
			t.Errorf("%s method %q: obj id %d has no object-table entry", typeDesc, name, oid)
			return
		}
		obj := doc.Objects[oid].(map[string]any)
		if kind, _ := obj["kind"].(string); kind != "func" {
			t.Errorf("%s method %q: obj kind = %q, want \"func\"", typeDesc, name, kind)
		}
		if n, _ := obj["name"].(string); n != name {
			t.Errorf("%s method %q: obj names %q", typeDesc, name, n)
		}
	}
	checkNamedMethod(detail, "Circle", "Area", "value")
	checkNamedMethod(detail, "Circle", "Name", "value")

	// The pointer entry carries NewMethodSet(*Circle): value AND
	// pointer receivers.
	ptrDetail, ok := func() (map[string]any, bool) {
		for _, e := range typeEntries(doc) {
			kind, _ := e["kind"].(string)
			if kind != "pointer" {
				continue
			}
			d, _ := e["detail"].(map[string]any)
			if d == nil {
				continue
			}
			if elem, ok := asInt(d["elem"]); ok && elem == circleID {
				return d, true
			}
		}
		return nil, false
	}()
	if !ok {
		t.Fatalf("no pointer type entry for *Circle in type table")
	}
	if got := methodNames(t, ptrDetail); !sameSet(got, []string{"Area", "Name", "Greet"}) {
		t.Errorf("*Circle methods = %v, want [Area Name Greet] (NewMethodSet(*T): value and pointer receivers)", got)
	}
	checkNamedMethod(ptrDetail, "*Circle", "Area", "value")
	checkNamedMethod(ptrDetail, "*Circle", "Name", "value")
	checkNamedMethod(ptrDetail, "*Circle", "Greet", "pointer")

	// Interface method sets, by name. Greeter embeds Shaper, so its set
	// must include the promoted Area/Name as well as Greet.
	for _, want := range []struct {
		iface   string
		methods []string
	}{
		{"Shaper", []string{"Area", "Name"}},
		{"Greeter", []string{"Area", "Name", "Greet"}},
	} {
		var ifaceDetail map[string]any
		for _, e := range typeEntries(doc) {
			kind, _ := e["kind"].(string)
			if kind != "interface" {
				continue
			}
			// The interface under test is reachable as the underlying
			// type of the named interface type; match by method names
			// instead: find the interface whose method set equals want.
			d, _ := e["detail"].(map[string]any)
			if d == nil {
				continue
			}
			if names := methodNames(t, d); sameSet(names, want.methods) {
				ifaceDetail = d
				break
			}
		}
		if ifaceDetail == nil {
			// Report what the interfaces actually carry.
			var have []string
			for _, e := range typeEntries(doc) {
				if kind, _ := e["kind"].(string); kind == "interface" {
					if d, _ := e["detail"].(map[string]any); d != nil {
						have = append(have, strings.Join(methodNames(t, d), ","))
					}
				}
			}
			t.Errorf("interface with methods %v: none found (interface method sets present: [%s])",
				want.methods, strings.Join(have, "] ["))
		}
	}
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]int{}
	for _, x := range a {
		seen[x]++
	}
	for _, x := range b {
		seen[x]--
		if seen[x] < 0 {
			return false
		}
	}
	return true
}
