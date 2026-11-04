// Top-level variable emission.
//
// Regression for review NEW-P1-6: emitDocument used to collect only
// *ast.FuncDecl and hardcode Globals to an empty list, so a program
// with `var x = 5` emitted successfully with x gone — a silent drop.
// Package-level vars must now appear in Document.Globals, with their
// initializers, in source order. Package-level const declarations,
// which have no faithful representation yet (a const's value is not
// recoverable from the object table), must fail loudly instead. Type
// declarations are the exception that proves the rule: a named type's
// definition is materialized in the Types table via go/types when
// referenced, so a type declaration needs no Globals entry — that is
// proved, not assumed, by TestTopLevelTypeDeclViaTypes below.
package emit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/parser"
)

// emitTestProgram runs the full pipeline on one source file and returns
// the document or the pipeline error (parse, check, or emit).
func emitTestProgram(t *testing.T, src string) (*Document, error) {
	t.Helper()
	return emitTestFiles(t, map[string]string{"main.go": src})
}

// emitTestFiles runs the full pipeline on a multi-file package. The file
// map keys are basenames; they are written in sorted order so the
// caller's input is deterministic.
func emitTestFiles(t *testing.T, files map[string]string) (*Document, error) {
	t.Helper()
	dir := t.TempDir()
	paths := make([]string, 0, len(files))
	for name := range files {
		fn := filepath.Join(dir, name)
		if err := os.WriteFile(fn, []byte(files[name]), 0644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, fn)
	}
	// The emitter's position table indexes files by the caller's file
	// list order, so the list must be sorted for repeated emissions of
	// the same package to be comparable (same contract as the
	// determinism test's input list).
	sort.Strings(paths)
	pres, err := parser.ParseFiles(paths)
	if err != nil {
		return nil, err
	}
	cres, err := checker.Check(pres.Fset, pres.Pkg, &checker.Config{GoVersion: "go1.27"})
	if err != nil {
		return nil, err
	}
	return Emit(cres, pres.Pkg, pres.Files)
}

// globalEntry extracts one Globals entry's fields, failing the test if
// the entry is not the expected "vardecl" node shape.
func globalEntry(t *testing.T, doc *Document, i int) map[string]any {
	t.Helper()
	if i >= len(doc.Globals) {
		t.Fatalf("globals[%d] missing: only %d globals", i, len(doc.Globals))
	}
	m, ok := doc.Globals[i].(map[string]any)
	if !ok {
		t.Fatalf("globals[%d] is %T, want map", i, doc.Globals[i])
	}
	if m["kind"] != "vardecl" {
		t.Fatalf("globals[%d] kind = %v, want vardecl", i, m["kind"])
	}
	fields, ok := m["fields"].(map[string]any)
	if !ok {
		t.Fatalf("globals[%d] has no fields map", i)
	}
	return fields
}

func globalNames(t *testing.T, fields map[string]any) []string {
	t.Helper()
	// emitGlobalVar stores names as []string (as emitDeclStmt does);
	// accept that form and the generic []any form alike.
	if names, ok := fields["names"].([]string); ok {
		return names
	}
	raw, ok := fields["names"].([]any)
	if !ok {
		t.Fatalf("vardecl names = %T, want []string or []any", fields["names"])
	}
	names := make([]string, len(raw))
	for i, n := range raw {
		s, ok := n.(string)
		if !ok {
			t.Fatalf("vardecl name %d = %T, want string", i, n)
		}
		names[i] = s
	}
	return names
}

func globalObjIDs(t *testing.T, fields map[string]any) []int {
	t.Helper()
	raw, ok := fields["objs"].([]any)
	if !ok {
		t.Fatalf("vardecl objs = %T, want []any", fields["objs"])
	}
	ids := make([]int, len(raw))
	for i, o := range raw {
		id, ok := o.(int)
		if !ok {
			t.Fatalf("vardecl obj %d = %T, want int", i, o)
		}
		ids[i] = id
	}
	return ids
}

func TestTopLevelVarsEmitted(t *testing.T) {
	src := `package demo

var x = 5
var y int = 7
var a, b = 1, 2
var s string
var pts = []int{1, 2}
var first = second + 1
var second = 10

func GetX() int { return x }
func GetAll() int { return x + y + a + b + pts[0] + first + second }
func main() { println(GetX(), GetAll(), s) }
`
	doc, err := emitTestProgram(t, src)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	// One Globals entry per ValueSpec, in source order. The old code
	// produced zero entries for this program.
	wantNames := [][]string{
		{"x"}, {"y"}, {"a", "b"}, {"s"}, {"pts"}, {"first"}, {"second"},
	}
	if len(doc.Globals) != len(wantNames) {
		t.Fatalf("len(Globals) = %d, want %d", len(doc.Globals), len(wantNames))
	}
	for i, want := range wantNames {
		fields := globalEntry(t, doc, i)
		if got := globalNames(t, fields); !reflect.DeepEqual(got, want) {
			t.Fatalf("globals[%d] names = %v, want %v", i, got, want)
		}
	}
	// Initializer arity: no-initializer spec has none, multi-name spec
	// has one per name, single-name specs have one each.
	wantValueCounts := []int{1, 1, 2, 0, 1, 1, 1}
	for i, want := range wantValueCounts {
		fields := globalEntry(t, doc, i)
		values, ok := fields["values"].([]any)
		if !ok {
			t.Fatalf("globals[%d] values = %T, want []any", i, fields["values"])
		}
		if len(values) != want {
			t.Fatalf("globals[%d] values = %d entries, want %d", i, len(values), want)
		}
	}
	// The forward reference (first = second + 1, second declared later)
	// must have emitted, and as a binary expression.
	first := globalEntry(t, doc, 5)
	values := first["values"].([]any)
	if m, ok := values[0].(map[string]any); !ok || m["kind"] != "binary" {
		t.Fatalf("first initializer = %v, want a binary node", values[0])
	}
	// Object ids are consistent: the id GetX's body assigns to x is the
	// id the Globals entry for x carries. (Both go through the first
	// pass's declareObject, so a body reference cannot silently get a
	// different object than the global declaration.)
	xObjs := globalObjIDs(t, globalEntry(t, doc, 0))
	if len(xObjs) != 1 {
		t.Fatalf("x objs = %v, want one id", xObjs)
	}
	if obj, ok := doc.Objects[xObjs[0]].(map[string]any); !ok || obj["name"] != "x" {
		t.Fatalf("objects[%d] = %v, want the var x", xObjs[0], doc.Objects[xObjs[0]])
	}
	// A program whose only top-level declaration is a var still emits
	// it (the old code needed a func to produce any output at all, and
	// still dropped the var).
	only, err := emitTestProgram(t, "package demo\n\nvar x = 5\n")
	if err != nil {
		t.Fatalf("var-only program emit: %v", err)
	}
	if len(only.Globals) != 1 || len(only.Funcs) != 0 {
		t.Fatalf("var-only program: %d globals, %d funcs; want 1, 0",
			len(only.Globals), len(only.Funcs))
	}
}

func TestTopLevelVarsGroupedAndDeterministic(t *testing.T) {
	src := `package demo

var (
	x = 1
	y = 2
)
func main() { println(x + y) }
`
	doc, err := emitTestProgram(t, src)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	if len(doc.Globals) != 2 {
		t.Fatalf("len(Globals) = %d, want 2 (one per grouped spec)", len(doc.Globals))
	}
	if got := globalNames(t, globalEntry(t, doc, 0)); !reflect.DeepEqual(got, []string{"x"}) {
		t.Fatalf("globals[0] names = %v, want [x]", got)
	}
	if got := globalNames(t, globalEntry(t, doc, 1)); !reflect.DeepEqual(got, []string{"y"}) {
		t.Fatalf("globals[1] names = %v, want [y]", got)
	}

	// Globals order follows (filename, source position) across files,
	// and repeated emissions are identical.
	files := map[string]string{
		"a.go": "package demo\n\nvar alpha = 1\n",
		"b.go": "package demo\n\nvar beta = 2\nfunc main() { println(alpha + beta) }\n",
	}
	first, err := emitTestFiles(t, files)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	if got := globalNames(t, globalEntry(t, first, 0)); !reflect.DeepEqual(got, []string{"alpha"}) {
		t.Fatalf("multi-file globals[0] = %v, want [alpha]", got)
	}
	if got := globalNames(t, globalEntry(t, first, 1)); !reflect.DeepEqual(got, []string{"beta"}) {
		t.Fatalf("multi-file globals[1] = %v, want [beta]", got)
	}
	second, err := emitTestFiles(t, files)
	if err != nil {
		t.Fatalf("emit (2nd run): %v", err)
	}
	b1, _ := json.Marshal(first)
	b2, _ := json.Marshal(second)
	if string(b1) != string(b2) {
		t.Fatalf("repeated emission differs")
	}
}

func TestTopLevelConstFailsLoudly(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"const", "package demo\n\nconst c = 1\nfunc main() { println(c) }\n"},
		{"grouped-const", "package demo\n\nconst (\n\ta = 1\n\tb = 2\n)\nfunc main() { println(a + b) }\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := emitTestProgram(t, tc.src)
			if err == nil {
				t.Fatalf("expected loud failure, got success (silent drop)")
			}
			if !strings.Contains(err.Error(), "unsupported") {
				t.Fatalf("error %q does not contain %q", err.Error(), "unsupported")
			}
		})
	}
}

// TestTopLevelTypeDeclViaTypes proves the type-declaration exception:
// the declaration itself has no Globals entry, but its definition is
// not lost — a use of the type materializes a named entry in the Types
// table. If that ever stops being true, this test (not a downstream
// consumer) catches it.
func TestTopLevelTypeDeclViaTypes(t *testing.T) {
	doc, err := emitTestProgram(t, `package demo

type Point struct {
	X int
	Y int
}

func main() {
	p := Point{X: 1, Y: 2}
	println(p.X + p.Y)
}
`)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	if len(doc.Globals) != 0 {
		t.Fatalf("len(Globals) = %d, want 0 (types are not globals)", len(doc.Globals))
	}
	b, _ := json.Marshal(doc.Types)
	if !strings.Contains(string(b), "Point") {
		t.Fatalf("Types table does not contain the Point definition: %s", b)
	}
}

func TestTopLevelVarUnsupportedInitializerFailsLoudly(t *testing.T) {
	// A global whose initializer uses an unhandled construct must fail
	// in the emitter, not emit a global with the initializer dropped.
	_, err := emitTestProgram(t, `package demo

var f = func(x int) int { return x }
func main() { println(1) }
`)
	if err == nil {
		t.Fatalf("expected loud failure, got success")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("error %q does not contain %q", err.Error(), "unsupported")
	}
}
