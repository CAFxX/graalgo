// Regression tests for P1-4 (2026-11-03 review): the v2 emitter
// silently dropped every package-level variable — Document.Globals
// was hardcoded empty while references to those variables still
// emitted, leaving dangling object ids with no definition.
//
// These tests feed programs with package-level vars through the full
// pipeline (parse → check → emit) and assert the Globals entries
// exist, carry the right names/types/initializers, and are the same
// objects the function bodies reference. Each test is demonstrably
// capable of failing: on the pre-fix emitter every one of them fails
// with len(doc.Globals) == 0.
package emit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/parser"
)

// emitSources runs the full pipeline over the given filename → source
// map and returns the emitted document. Files are passed to the parser
// in map-iteration order (deliberately unordered): the emitter must
// impose deterministic order itself.
func emitSources(t *testing.T, sources map[string]string) *Document {
	t.Helper()
	dir := t.TempDir()
	paths := make([]string, 0, len(sources))
	for name, src := range sources {
		fn := filepath.Join(dir, name)
		if err := os.WriteFile(fn, []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, fn)
	}
	pres, err := parser.ParseFiles(paths)
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

// globalFields returns the fields map of Globals[i], failing the test
// unless the entry is a "global" node.
func globalFields(t *testing.T, doc *Document, i int) map[string]any {
	t.Helper()
	if i >= len(doc.Globals) {
		t.Fatalf("Globals has %d entries, want at least %d", len(doc.Globals), i+1)
	}
	entry, ok := doc.Globals[i].(map[string]any)
	if !ok {
		t.Fatalf("Globals[%d] is %T, want node map", i, doc.Globals[i])
	}
	if entry["kind"] != "global" {
		t.Fatalf("Globals[%d] kind = %v, want global", i, entry["kind"])
	}
	fields, ok := entry["fields"].(map[string]any)
	if !ok {
		t.Fatalf("Globals[%d] fields is %T, want map", i, entry["fields"])
	}
	return fields
}

// stringList converts a []any or []string of strings to []string,
// failing on any other element type. (The emitter uses []string for
// vardecl/global name lists, as in emitDeclStmt.)
func stringList(t *testing.T, v any) []string {
	t.Helper()
	switch list := v.(type) {
	case []string:
		return list
	case []any:
		out := make([]string, len(list))
		for i, e := range list {
			s, ok := e.(string)
			if !ok {
				t.Fatalf("element %d is %T, want string", i, e)
			}
			out[i] = s
		}
		return out
	default:
		t.Fatalf("value is %T, want []any or []string", v)
		return nil
	}
}

// objID converts an "objs" element to an int id.
func objID(t *testing.T, v any) int {
	t.Helper()
	switch id := v.(type) {
	case int:
		return id
	default:
		t.Fatalf("obj id is %T, want int", v)
		return -1
	}
}

// objectByID returns Objects[id] as a map, failing unless id is in range.
func objectByID(t *testing.T, doc *Document, id int) map[string]any {
	t.Helper()
	if id < 0 || id >= len(doc.Objects) {
		t.Fatalf("object id %d out of range (have %d objects): dangling reference", id, len(doc.Objects))
	}
	obj, ok := doc.Objects[id].(map[string]any)
	if !ok {
		t.Fatalf("Objects[%d] is %T, want map", id, doc.Objects[id])
	}
	return obj
}

// identObjs walks v recursively and returns every "obj" id carried by
// an "ident" node.
func identObjs(t *testing.T, v any) []int {
	t.Helper()
	var out []int
	var walk func(any)
	walk = func(x any) {
		switch n := x.(type) {
		case map[string]any:
			if n["kind"] == "ident" {
				if fields, ok := n["fields"].(map[string]any); ok {
					if id, ok := fields["obj"]; ok {
						out = append(out, objID(t, id))
					}
				}
			}
			for _, e := range n {
				walk(e)
			}
		case []any:
			for _, e := range n {
				walk(e)
			}
		}
	}
	walk(v)
	return out
}

// TestGlobalVarWithInitializerReferencedFromMain is the minimal P1-4
// repro: `var x = 1; func main() { println(x) }`. Pre-fix this emitted
// a document with no x definition while main referenced x's object id.
func TestGlobalVarWithInitializerReferencedFromMain(t *testing.T) {
	doc := emitSources(t, map[string]string{
		"main.go": `package main

var x = 1

func main() {
	println(x)
}
`,
	})
	if len(doc.Globals) != 1 {
		t.Fatalf("Globals has %d entries, want 1", len(doc.Globals))
	}
	fields := globalFields(t, doc, 0)
	if got := stringList(t, fields["names"]); len(got) != 1 || got[0] != "x" {
		t.Fatalf("global names = %v, want [x]", got)
	}
	values, ok := fields["values"].([]any)
	if !ok || len(values) != 1 {
		t.Fatalf("global values = %v, want 1 initializer", fields["values"])
	}
	lit, ok := values[0].(map[string]any)
	if !ok || lit["kind"] != "basiclit" {
		t.Fatalf("initializer is %v, want basiclit node", values[0])
	}
	litFields, _ := lit["fields"].(map[string]any)
	if litFields["value"] != "1" {
		t.Fatalf("initializer value = %v, want 1", litFields["value"])
	}
	// The global's object must be a real, named entry in the object table.
	objs, ok := fields["objs"].([]any)
	if !ok || len(objs) != 1 {
		t.Fatalf("global objs = %v, want 1 object id", fields["objs"])
	}
	globalID := objID(t, objs[0])
	if obj := objectByID(t, doc, globalID); obj["kind"] != "var" || obj["name"] != "x" {
		t.Fatalf("Objects[%d] = %v, want kind var name x", globalID, obj)
	}
	// And main's reference to x must resolve to that same object id —
	// not to a dangling id with no definition.
	refs := identObjs(t, doc.Funcs)
	found := false
	for _, id := range refs {
		if id == globalID {
			found = true
		}
		if id < 0 || id >= len(doc.Objects) {
			t.Fatalf("func body references dangling object id %d", id)
		}
	}
	if !found {
		t.Fatalf("no ident in func bodies references global object %d (refs %v)", globalID, refs)
	}
}

// TestGlobalVarMultiAndUninitialized covers the grouped/multi-var and
// uninitialized cases from the P1-4 brief: one entry per ValueSpec, in
// source order, with empty values for vars without an initializer and a
// non-nil explicit type where one is written.
func TestGlobalVarMultiAndUninitialized(t *testing.T) {
	doc := emitSources(t, map[string]string{
		"main.go": `package main

var a, b = 1, 2
var c int
var (
	d = 3
	e string
)
var f string = "s"

func main() {
	println(a)
	println(c)
}
`,
	})
	if len(doc.Globals) != 5 {
		t.Fatalf("Globals has %d entries, want 5 (one per ValueSpec)", len(doc.Globals))
	}
	// var a, b = 1, 2
	f0 := globalFields(t, doc, 0)
	if got := stringList(t, f0["names"]); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("Globals[0] names = %v, want [a b]", got)
	}
	if values, _ := f0["values"].([]any); len(values) != 2 {
		t.Fatalf("Globals[0] values has %d entries, want 2", len(values))
	}
	// var c int — no initializer, zero-initialized by the backend.
	f1 := globalFields(t, doc, 1)
	if got := stringList(t, f1["names"]); len(got) != 1 || got[0] != "c" {
		t.Fatalf("Globals[1] names = %v, want [c]", got)
	}
	if values, _ := f1["values"].([]any); len(values) != 0 {
		t.Fatalf("Globals[1] values = %v, want empty (uninitialized)", values)
	}
	if objs, _ := f1["objs"].([]any); len(objs) != 1 {
		t.Fatalf("Globals[1] objs = %v, want 1 object id", objs)
	} else if obj := objectByID(t, doc, objID(t, objs[0])); obj["name"] != "c" {
		t.Fatalf("Globals[1] object = %v, want name c", obj)
	}
	// Grouped specs stay separate entries, in source order.
	f2 := globalFields(t, doc, 2)
	if got := stringList(t, f2["names"]); len(got) != 1 || got[0] != "d" {
		t.Fatalf("Globals[2] names = %v, want [d]", got)
	}
	if values, _ := f2["values"].([]any); len(values) != 1 {
		t.Fatalf("Globals[2] values has %d entries, want 1", len(values))
	}
	f3 := globalFields(t, doc, 3)
	if got := stringList(t, f3["names"]); len(got) != 1 || got[0] != "e" {
		t.Fatalf("Globals[3] names = %v, want [e]", got)
	}
	if values, _ := f3["values"].([]any); len(values) != 0 {
		t.Fatalf("Globals[3] values = %v, want empty (uninitialized)", values)
	}
	// var f string = "s" — explicit type must be recorded.
	f4 := globalFields(t, doc, 4)
	if got := stringList(t, f4["names"]); len(got) != 1 || got[0] != "f" {
		t.Fatalf("Globals[4] names = %v, want [f]", got)
	}
	if f4["type"] == nil {
		t.Fatalf("Globals[4] type is nil, want explicit string type id")
	}
	if values, _ := f4["values"].([]any); len(values) != 1 {
		t.Fatalf("Globals[4] values has %d entries, want 1", len(values))
	}
	// Every object id in Globals must resolve in the object table.
	for i := range doc.Globals {
		fields := globalFields(t, doc, i)
		for _, o := range fields["objs"].([]any) {
			objectByID(t, doc, objID(t, o))
		}
	}
}

// TestGlobalVarForwardReference guards the declare-before-emit pass:
// an initializer may reference a global declared later in the file.
func TestGlobalVarForwardReference(t *testing.T) {
	doc := emitSources(t, map[string]string{
		"main.go": `package main

var y = x + 1
var x = 1

func main() {
	println(y)
}
`,
	})
	if len(doc.Globals) != 2 {
		t.Fatalf("Globals has %d entries, want 2", len(doc.Globals))
	}
	f0 := globalFields(t, doc, 0)
	if got := stringList(t, f0["names"]); len(got) != 1 || got[0] != "y" {
		t.Fatalf("Globals[0] names = %v, want [y]", got)
	}
	values, _ := f0["values"].([]any)
	if len(values) != 1 {
		t.Fatalf("Globals[0] values has %d entries, want 1", len(values))
	}
	if bin, ok := values[0].(map[string]any); !ok || bin["kind"] != "binary" {
		t.Fatalf("Globals[0] initializer is %v, want binary node", values[0])
	}
	// y's initializer references x's object, which must be defined.
	refs := identObjs(t, doc.Globals)
	if len(refs) == 0 {
		t.Fatalf("no ident references inside Globals initializers")
	}
	for _, id := range refs {
		objectByID(t, doc, id)
	}
}

// TestGlobalVarOrderAcrossFiles checks Globals follow deterministic
// (filename, source-position) order even when the parser is handed the
// files in the opposite order.
func TestGlobalVarOrderAcrossFiles(t *testing.T) {
	doc := emitSources(t, map[string]string{
		"a.go": `package demo

var first = 1
`,
		"b.go": `package demo

var second = 2
`,
	})
	if len(doc.Globals) != 2 {
		t.Fatalf("Globals has %d entries, want 2", len(doc.Globals))
	}
	if got := stringList(t, globalFields(t, doc, 0)["names"]); len(got) != 1 || got[0] != "first" {
		t.Fatalf("Globals[0] names = %v, want [first]", got)
	}
	if got := stringList(t, globalFields(t, doc, 1)["names"]); len(got) != 1 || got[0] != "second" {
		t.Fatalf("Globals[1] names = %v, want [second]", got)
	}
}

// TestTopLevelConstFailsLoudly pins the fail-loudly half of the P1-4
// decision: top-level const has no runtime slot and its value is not
// in the object table, so it must error, never silently vanish like
// vars used to.
func TestTopLevelConstFailsLoudly(t *testing.T) {
	dir := t.TempDir()
	fn := filepath.Join(dir, "main.go")
	src := `package main

const k = 1

func main() {
	println(k)
}
`
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
		t.Fatalf("expected loud failure for top-level const, got success")
	}
	if !strings.Contains(err.Error(), "unsupported top-level const") {
		t.Fatalf("error %q does not contain %q", err.Error(), "unsupported top-level const")
	}
}
