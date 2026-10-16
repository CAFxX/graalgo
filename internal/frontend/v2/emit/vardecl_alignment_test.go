// Regression tests for per-ValueSpec name/value/type alignment in
// grouped var declarations (review findings V2-4, V2-27).
//
// A grouped declaration like
//
//	var (a int; b = 2; c, d string = "x", "y")
//
// must emit ONE vardecl node per ValueSpec. The backend factory pairs
// values to names by index, so merging specs into a single node silently
// mis-assigns values (a got 2, b got 0) and the shared "type" field
// records only the last spec's type for every name.
//
// These tests assert alignment on the emitted wire nodes: every spec
// keeps its own names, its own values, and its own type.
package emit

import (
	"os"
	"path/filepath"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/parser"
)

// emitDoc runs a source snippet through the full pipeline (parse → check →
// emit) and returns the emitted document.
func emitDoc(t *testing.T, src string) *Document {
	t.Helper()
	dir := t.TempDir()
	fn := filepath.Join(dir, "main.go")
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

// funcBody returns the top-level statements of the named function's body.
func funcBody(t *testing.T, doc *Document, name string) []any {
	t.Helper()
	for _, f := range doc.Funcs {
		fm := f.(map[string]any)
		fields := fm["fields"].(map[string]any)
		if fields["name"] == name {
			body := fields["body"].(map[string]any)
			return body["fields"].(map[string]any)["stmts"].([]any)
		}
	}
	t.Fatalf("func %q not found in emitted document", name)
	return nil
}

// specVarDecls flattens a declaration statement node into its per-spec
// vardecl nodes: a bare "vardecl" for a single spec, or the children of
// the "block" wrapper for a grouped declaration. Any other shape is a
// failure — the test must not silently accept a new merged encoding.
func specVarDecls(t *testing.T, n map[string]any) []map[string]any {
	t.Helper()
	kind, _ := n["kind"].(string)
	fields, _ := n["fields"].(map[string]any)
	switch kind {
	case "vardecl":
		return []map[string]any{n}
	case "block":
		var out []map[string]any
		for i, s := range fields["stmts"].([]any) {
			sm := s.(map[string]any)
			if k, _ := sm["kind"].(string); k != "vardecl" {
				t.Fatalf("grouped var decl child %d: expected vardecl, got %q", i, k)
			}
			out = append(out, sm)
		}
		return out
	}
	t.Fatalf("expected vardecl or block-of-vardecls, got %q", kind)
	return nil
}

// vardeclFields extracts names/values/type of one vardecl wire node.
func vardeclFields(t *testing.T, n map[string]any) (names []string, values []any, typeID any) {
	t.Helper()
	if k, _ := n["kind"].(string); k != "vardecl" {
		t.Fatalf("expected vardecl node, got %q", k)
	}
	fields := n["fields"].(map[string]any)
	for _, name := range fields["names"].([]string) {
		names = append(names, name)
	}
	values = fields["values"].([]any)
	typeID = fields["type"]
	return names, values, typeID
}

// typeName resolves a wire type ID back to the basic type name via the
// document's type table. A nil type ID (untyped spec) yields "".
func typeName(t *testing.T, doc *Document, typeID any) string {
	t.Helper()
	if typeID == nil {
		return ""
	}
	id, ok := typeID.(int)
	if !ok {
		t.Fatalf("vardecl type field is %T, expected int or nil", typeID)
	}
	td := doc.Types[id].(map[string]any)
	detail := td["detail"].(map[string]any)
	name, _ := detail["name"].(string)
	return name
}

// basicLit returns the literal source text of an emitted BasicLit node.
func basicLit(t *testing.T, v any) string {
	t.Helper()
	vm := v.(map[string]any)
	if k, _ := vm["kind"].(string); k != "basiclit" {
		t.Fatalf("expected basiclit value node, got %q", k)
	}
	lit, _ := vm["fields"].(map[string]any)["value"].(string)
	return lit
}

func TestVarDeclGroupedAlignment(t *testing.T) {
	doc := emitDoc(t, `package main

func main() {
	var (
		a int
		b = 2
		c, d string = "x", "y"
		e float64
		f = 3.14
	)
	_, _, _, _, _, _ = a, b, c, d, e, f
}
`)
	stmts := funcBody(t, doc, "main")
	if len(stmts) != 2 {
		t.Fatalf("expected 2 top-level stmts (grouped decl + use), got %d", len(stmts))
	}
	specs := specVarDecls(t, stmts[0].(map[string]any))
	if len(specs) != 5 {
		t.Fatalf("grouped var decl with 5 specs emitted %d vardecl nodes", len(specs))
	}

	type want struct {
		names []string
		lits  []string // expected value literals; empty means no values
		typ   string   // expected declared type name; "" means untyped spec
	}
	wants := []want{
		{names: []string{"a"}, lits: nil, typ: "int"},
		{names: []string{"b"}, lits: []string{"2"}, typ: ""},
		{names: []string{"c", "d"}, lits: []string{`"x"`, `"y"`}, typ: "string"},
		{names: []string{"e"}, lits: nil, typ: "float64"},
		{names: []string{"f"}, lits: []string{"3.14"}, typ: ""},
	}
	for i, w := range wants {
		names, values, typeID := vardeclFields(t, specs[i])
		if len(names) != len(w.names) {
			t.Fatalf("spec %d: got names %v, want %v", i, names, w.names)
		}
		for j, name := range w.names {
			if names[j] != name {
				t.Fatalf("spec %d: got names %v, want %v", i, names, w.names)
			}
		}
		if len(values) != len(w.lits) {
			t.Fatalf("spec %d (%v): got %d values, want %d", i, names, len(values), len(w.lits))
		}
		for j, lit := range w.lits {
			if got := basicLit(t, values[j]); got != lit {
				t.Fatalf("spec %d (%v): value %d = %q, want %q", i, names, j, got, lit)
			}
		}
		if got := typeName(t, doc, typeID); got != w.typ {
			t.Fatalf("spec %d (%v): type = %q, want %q", i, names, got, w.typ)
		}
		// Structural invariant the factory relies on: values must align
		// with names by index, so a spec either has no values or exactly
		// one value per name.
		if len(values) != 0 && len(values) != len(names) {
			t.Fatalf("spec %d (%v): %d values do not align with %d names", i, names, len(values), len(names))
		}
	}
}

// TestVarDeclGroupedTypes asserts each spec records its OWN type (V2-27):
// previously the shared "type" field was overwritten per spec, so
// `var (a int; b string)` recorded string for both names.
func TestVarDeclGroupedTypes(t *testing.T) {
	doc := emitDoc(t, `package main

func main() {
	var (
		a int
		b string
	)
	_, _ = a, b
}
`)
	stmts := funcBody(t, doc, "main")
	specs := specVarDecls(t, stmts[0].(map[string]any))
	if len(specs) != 2 {
		t.Fatalf("expected 2 per-spec vardecl nodes, got %d", len(specs))
	}
	names0, _, type0 := vardeclFields(t, specs[0])
	names1, _, type1 := vardeclFields(t, specs[1])
	if len(names0) != 1 || names0[0] != "a" {
		t.Fatalf("spec 0 names = %v, want [a]", names0)
	}
	if len(names1) != 1 || names1[0] != "b" {
		t.Fatalf("spec 1 names = %v, want [b]", names1)
	}
	if got := typeName(t, doc, type0); got != "int" {
		t.Fatalf("spec (a): type = %q, want %q", got, "int")
	}
	if got := typeName(t, doc, type1); got != "string" {
		t.Fatalf("spec (b): type = %q, want %q", got, "string")
	}
}

// TestVarDeclSingleSpecUnchanged asserts a lone var spec keeps the
// existing single-vardecl wire shape (no block wrapper), so single-spec
// consumers see no format change.
func TestVarDeclSingleSpecUnchanged(t *testing.T) {
	doc := emitDoc(t, `package main

func main() {
	var x int = 5
	_ = x
}
`)
	stmts := funcBody(t, doc, "main")
	if len(stmts) != 2 {
		t.Fatalf("expected 2 stmts, got %d", len(stmts))
	}
	n := stmts[0].(map[string]any)
	if k, _ := n["kind"].(string); k != "vardecl" {
		t.Fatalf("single var spec emitted %q, want bare vardecl (no wrapper)", k)
	}
	names, values, typeID := vardeclFields(t, n)
	if len(names) != 1 || names[0] != "x" {
		t.Fatalf("names = %v, want [x]", names)
	}
	if len(values) != 1 || basicLit(t, values[0]) != "5" {
		t.Fatalf("values mis-emitted for single spec")
	}
	if got := typeName(t, doc, typeID); got != "int" {
		t.Fatalf("type = %q, want %q", got, "int")
	}
}
