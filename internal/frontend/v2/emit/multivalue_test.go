// Multi-value shapes through the v2 emitter.
//
// These tests pin the emitter side of the H1 contract: the emitter must
// emit multi-value constructs faithfully (it must NOT reject them —
// rejection is the Java factory's job, where the shapes fail loudly
// until tuple unpacking exists; see NodeFactoryV2MultiValueTest on the
// Java side). Each test asserts the exact emitted shape that the
// factory's fail-fast checks key on, plus the tuple type-table entry
// for multi-valued expressions (a call returning several values, a
// comma-ok index), which the old default branch misdescribed as a
// basic type named "(int, int)".
package emit

import (
	"os"
	"path/filepath"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/parser"
)

func emitDoc(t *testing.T, src string) *Document {
	t.Helper()
	dir := t.TempDir()
	fn := filepath.Join(dir, "p.go")
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

func nodeFields(t *testing.T, n any) map[string]any {
	t.Helper()
	m, ok := n.(map[string]any)
	if !ok {
		t.Fatalf("node is %T, want map", n)
	}
	f, ok := m["fields"].(map[string]any)
	if !ok {
		t.Fatalf("node %v has no fields map", m["kind"])
	}
	return f
}

func nodeKind(t *testing.T, n any) string {
	t.Helper()
	m, ok := n.(map[string]any)
	if !ok {
		t.Fatalf("node is %T, want map", n)
	}
	k, ok := m["kind"].(string)
	if !ok {
		t.Fatalf("node has no string kind: %#v", m)
	}
	return k
}

func funcByName(t *testing.T, doc *Document, name string) map[string]any {
	t.Helper()
	for _, f := range doc.Funcs {
		if nodeFields(t, f)["name"] == name {
			return f.(map[string]any)
		}
	}
	t.Fatalf("no func %q in document", name)
	return nil
}

func bodyStmts(t *testing.T, fn map[string]any) []any {
	t.Helper()
	body, ok := nodeFields(t, fn)["body"].(map[string]any)
	if !ok {
		t.Fatalf("func has no body map")
	}
	stmts, ok := nodeFields(t, body)["stmts"].([]any)
	if !ok {
		t.Fatalf("body has no stmts list")
	}
	return stmts
}

func listField(t *testing.T, n any, field string) []any {
	t.Helper()
	l, ok := nodeFields(t, n)[field].([]any)
	if !ok {
		t.Fatalf("node kind %q has no %q list", nodeKind(t, n), field)
	}
	return l
}

// tupleResults returns the result type ids of the type-table entry id,
// failing unless the entry is a tuple with the expected arity.
func tupleResults(t *testing.T, doc *Document, typeID any, want int) []any {
	t.Helper()
	id, ok := typeID.(int)
	if !ok {
		t.Fatalf("type id is %T, want int", typeID)
	}
	if id < 0 || id >= len(doc.Types) {
		t.Fatalf("type id %d out of range (have %d types)", id, len(doc.Types))
	}
	entry, ok := doc.Types[id].(map[string]any)
	if !ok {
		t.Fatalf("type %d is %T, want map", id, doc.Types[id])
	}
	if entry["kind"] != "tuple" {
		t.Fatalf("type %d kind is %q, want \"tuple\" (multi-valued expression)", id, entry["kind"])
	}
	detail, ok := entry["detail"].(map[string]any)
	if !ok {
		t.Fatalf("tuple type %d has no detail map", id)
	}
	results, ok := detail["results"].([]any)
	if !ok || len(results) != want {
		t.Fatalf("tuple type %d has %d results, want %d", id, len(results), want)
	}
	return results
}

func TestMultiValueReturnEmitted(t *testing.T) {
	doc := emitDoc(t, "package main\nfunc f() (int, int) { a := 1; b := 2; return a, b }\nfunc main() {}\n")
	stmts := bodyStmts(t, funcByName(t, doc, "f"))
	if len(stmts) != 3 {
		t.Fatalf("f body has %d stmts, want 3", len(stmts))
	}
	ret := stmts[2]
	if nodeKind(t, ret) != "return" {
		t.Fatalf("last stmt kind is %q, want return", nodeKind(t, ret))
	}
	if got := len(listField(t, ret, "results")); got != 2 {
		t.Fatalf("return has %d results, want 2", got)
	}
}

func TestMultiValueAssignFromCallEmitted(t *testing.T) {
	doc := emitDoc(t, "package main\nfunc f() (int, int) { return 1, 2 }\nfunc main() { a, b := f(); _, _ = a, b }\n")
	stmts := bodyStmts(t, funcByName(t, doc, "main"))
	as := stmts[0]
	if nodeKind(t, as) != "assign" {
		t.Fatalf("first stmt kind is %q, want assign", nodeKind(t, as))
	}
	if got := len(listField(t, as, "lhs")); got != 2 {
		t.Fatalf("assign has %d lhs, want 2", got)
	}
	rhs := listField(t, as, "rhs")
	if len(rhs) != 1 {
		t.Fatalf("assign has %d rhs, want 1 (the multi-valued call)", len(rhs))
	}
	if nodeKind(t, rhs[0]) != "call" {
		t.Fatalf("rhs kind is %q, want call", nodeKind(t, rhs[0]))
	}
	tupleResults(t, doc, nodeFields(t, rhs[0])["type"], 2)
}

func TestMultiValueAssignCommaOkEmitted(t *testing.T) {
	doc := emitDoc(t, "package main\nfunc main() { m := map[string]int{\"k\": 7}; v, ok := m[\"k\"]; _, _ = v, ok }\n")
	stmts := bodyStmts(t, funcByName(t, doc, "main"))
	as := stmts[1]
	if nodeKind(t, as) != "assign" {
		t.Fatalf("second stmt kind is %q, want assign", nodeKind(t, as))
	}
	if got := len(listField(t, as, "lhs")); got != 2 {
		t.Fatalf("comma-ok assign has %d lhs, want 2", got)
	}
	rhs := listField(t, as, "rhs")
	if len(rhs) != 1 {
		t.Fatalf("comma-ok assign has %d rhs, want 1 (the index)", len(rhs))
	}
	if nodeKind(t, rhs[0]) != "index" {
		t.Fatalf("comma-ok rhs kind is %q, want index", nodeKind(t, rhs[0]))
	}
	tupleResults(t, doc, nodeFields(t, rhs[0])["type"], 2)
}

func TestMultiValueVarDeclFromCallEmitted(t *testing.T) {
	doc := emitDoc(t, "package main\nfunc f() (int, int) { return 1, 2 }\nfunc main() { var c, d = f(); _, _ = c, d }\n")
	stmts := bodyStmts(t, funcByName(t, doc, "main"))
	vd := stmts[0]
	if nodeKind(t, vd) != "vardecl" {
		t.Fatalf("first stmt kind is %q, want vardecl", nodeKind(t, vd))
	}
	f := nodeFields(t, vd)
	names, ok := f["names"].([]string)
	if !ok || len(names) != 2 {
		t.Fatalf("vardecl names are %#v, want 2 names", f["names"])
	}
	values, ok := f["values"].([]any)
	if !ok || len(values) != 1 {
		t.Fatalf("vardecl has %d values, want 1 (the multi-valued call)", len(values))
	}
	if nodeKind(t, values[0]) != "call" {
		t.Fatalf("vardecl value kind is %q, want call", nodeKind(t, values[0]))
	}
	tupleResults(t, doc, nodeFields(t, values[0])["type"], 2)
}

func TestMultiValueReturnFromCallEmitted(t *testing.T) {
	doc := emitDoc(t, "package main\nfunc f() (int, int) { return 1, 2 }\nfunc g() (int, int) { return f() }\nfunc main() {}\n")
	stmts := bodyStmts(t, funcByName(t, doc, "g"))
	ret := stmts[0]
	if nodeKind(t, ret) != "return" {
		t.Fatalf("stmt kind is %q, want return", nodeKind(t, ret))
	}
	results := listField(t, ret, "results")
	if len(results) != 1 {
		t.Fatalf("return has %d results, want 1 (the multi-valued call)", len(results))
	}
	if nodeKind(t, results[0]) != "call" {
		t.Fatalf("return result kind is %q, want call", nodeKind(t, results[0]))
	}
	tupleResults(t, doc, nodeFields(t, results[0])["type"], 2)
}

// Positive controls: matched-count shapes must keep emitting as before,
// so the factory's fail-fast checks cannot be justified as "the emitter
// never produces multi shapes anyway".
func TestMatchedMultiAssignAndVarDeclEmit(t *testing.T) {
	doc := emitDoc(t, "package main\nfunc main() { a, b := 1, 2; var c, d = 3, 4; _, _, _, _ = a, b, c, d }\n")
	stmts := bodyStmts(t, funcByName(t, doc, "main"))
	if got := len(listField(t, stmts[0], "lhs")); got != 2 {
		t.Fatalf("matched assign has %d lhs, want 2", got)
	}
	if got := len(listField(t, stmts[0], "rhs")); got != 2 {
		t.Fatalf("matched assign has %d rhs, want 2", got)
	}
	f := nodeFields(t, stmts[1])
	names, ok := f["names"].([]string)
	if !ok || len(names) != 2 {
		t.Fatalf("matched vardecl names are %#v, want 2", f["names"])
	}
	values, ok := f["values"].([]any)
	if !ok || len(values) != 2 {
		t.Fatalf("matched vardecl has %d values, want 2", len(values))
	}
}
