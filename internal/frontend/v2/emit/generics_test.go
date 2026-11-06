// Generics and alias tests (REVIEW-2026-11-06-10 R-P1-1).
//
// The v2 emitter has no representation for generics: a generic function or
// type declaration must fail loudly at the declaration, never emit
// "successfully" with its type parameters silently dropped. Two kinds that
// look unrepresentable at first sight ARE representable and are pinned
// here as positive cases:
//   - a plain (non-generic) alias unfolds to its right-hand side, which is
//     what the alias means in Go;
//   - a call's result type is a tuple (empty for a void call), which is
//     emitted as kind "tuple" — the Java consumer maps an unknown type
//     kind to INTERFACE, so it decodes, where the old basic/unknown entry
//     for () threw in fromBasic for every non-builtin void call.
package emit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/parser"
)

// emitSource runs one program through parse → check → emit.
func emitSource(t *testing.T, src string) (*Document, error) {
	t.Helper()
	dir := t.TempDir()
	fn := filepath.Join(dir, "gen.go")
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
	return Emit(cres, pres.Pkg, pres.Files)
}

// genericDeclCase is one generic declaration that must fail loudly.
var genericDeclCases = []struct {
	name string
	src  string
	want string
}{
	{
		// The reported bug: a generic declaration with no
		// instantiation in the file used to emit "successfully".
		name: "GenericFuncDeclNoInstantiation",
		src: `package main
func id[T any](x T) T { return x }
func main() {}
`,
		want: "unsupported generic func",
	},
	{
		// The case a describeType-only fix would miss: no parameter
		// or result uses T, so no type parameter ever reaches the
		// type table. Only the explicit check in emitFuncDecl
		// catches it.
		name: "GenericFuncDeclUnusedTypeParam",
		src: `package main
func f[T any]() {}
func main() {}
`,
		want: "unsupported generic func",
	},
	{
		// A generic type declaration is otherwise invisible to the
		// emitter (only referenced types reach the type table), so
		// it would be silently dropped. emitDocument rejects it.
		name: "GenericTypeDecl",
		src: `package main
type List[T any] struct { head T }
func main() {}
`,
		want: "unsupported generic type",
	},
	{
		// A use of the generic type must not sneak past the
		// declaration check: the declaration fails first.
		name: "GenericTypeUsed",
		src: `package main
type List[T any] struct { head T }
var x List[int]
func main() { println(x.head) }
`,
		want: "unsupported generic type",
	},
	{
		// A generic alias: its right-hand side contains the type
		// parameter, so it fails at the declaration.
		name: "GenericAliasDecl",
		src: `package main
type L[T any] = []T
func main() {}
`,
		want: "unsupported generic type",
	},
	{
		// The union itself is never described (the Interface case
		// does not recurse into type sets); the generic function
		// carrying it is the rejection point.
		name: "UnionConstraint",
		src: `package main
func f[T interface{ ~int | ~string }](x T) T { return x }
func main() {}
`,
		want: "unsupported generic func",
	},
}

func TestGenericDeclarationsFailLoudly(t *testing.T) {
	for _, tc := range genericDeclCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := emitSource(t, tc.src)
			if err == nil {
				t.Fatalf("%s: expected loud failure, got success (silent acceptance)", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("%s: error %q does not contain %q", tc.name, err.Error(), tc.want)
			}
			if strings.Contains(err.Error(), "panic") || strings.Contains(err.Error(), "nil pointer") {
				t.Fatalf("%s: error %q looks like a crash, not a clean rejection", tc.name, err.Error())
			}
		})
	}
}

// typeEntry returns the type-table entry with the given id.
func typeEntry(doc *Document, id int) map[string]any {
	if id < 0 || id >= len(doc.Types) {
		return nil
	}
	m, _ := doc.Types[id].(map[string]any)
	return m
}

// assertNoUnknownClass fails if any emitted type is a basic/unknown
// placeholder: after this fix no such entry may be produced.
func assertNoUnknownClass(t *testing.T, doc *Document) {
	t.Helper()
	for i, raw := range doc.Types {
		m, _ := raw.(map[string]any)
		if m == nil {
			t.Fatalf("type %d is not a map: %#v", i, raw)
		}
		if m["kind"] == "basic" {
			detail, _ := m["detail"].(map[string]any)
			if detail["class"] == "unknown" {
				t.Fatalf("type %d is basic/unknown: %#v (must fail loudly or be represented)", i, raw)
			}
		}
	}
}

func TestNonGenericAliasUnfoldsToRHS(t *testing.T) {
	doc, err := emitSource(t, `package main
type A = int
type S struct { F A }
var x A = 3
var s S
func main() { println(x, s.F) }
`)
	if err != nil {
		t.Fatalf("plain alias program must emit, got error: %v", err)
	}
	assertNoUnknownClass(t, doc)
	// The var x must resolve to a basic int type, exactly as if it had
	// been declared `var x int`.
	found := false
	for _, raw := range doc.Objects {
		o, _ := raw.(map[string]any)
		if o == nil || o["kind"] != "var" || o["name"] != "x" {
			continue
		}
		tid, _ := o["type"].(int)
		entry := typeEntry(doc, tid)
		if entry == nil {
			t.Fatalf("var x has no type entry for id %d", tid)
		}
		if entry["kind"] != "basic" {
			t.Fatalf("var x type kind = %v, want basic (alias must unfold)", entry["kind"])
		}
		detail, _ := entry["detail"].(map[string]any)
		if detail["name"] != "int" || detail["class"] != "int" {
			t.Fatalf("var x type = %#v, want basic int", entry)
		}
		found = true
	}
	if !found {
		t.Fatalf("var x not found in objects: %#v", doc.Objects)
	}
	// The struct field of alias type must likewise be basic int.
	for _, raw := range doc.Types {
		m, _ := raw.(map[string]any)
		if m == nil || m["kind"] != "struct" {
			continue
		}
		detail, _ := m["detail"].(map[string]any)
		fields, _ := detail["fields"].([]any)
		for _, fraw := range fields {
			f, _ := fraw.(map[string]any)
			if f == nil || f["name"] != "F" {
				continue
			}
			fid, _ := f["type"].(int)
			entry := typeEntry(doc, fid)
			if entry == nil || entry["kind"] != "basic" {
				t.Fatalf("struct field F type = %#v, want basic int", entry)
			}
		}
	}
}

func TestVoidAndMultiValueCallsEmitTupleType(t *testing.T) {
	doc, err := emitSource(t, `package main
func f() {}
func g() (int, string) { return 1, "a" }
func main() {
	f()
	g()
}
`)
	if err != nil {
		t.Fatalf("void/multi-value call program must emit, got error: %v", err)
	}
	assertNoUnknownClass(t, doc)
	// One empty tuple (f's call) and one 2-element tuple (g's call).
	seen := map[int]bool{}
	for _, raw := range doc.Types {
		m, _ := raw.(map[string]any)
		if m == nil || m["kind"] != "tuple" {
			continue
		}
		detail, _ := m["detail"].(map[string]any)
		elems, _ := detail["elems"].([]any)
		seen[len(elems)] = true
	}
	if !seen[0] {
		t.Fatalf("no empty tuple type for the void call f(): types=%v", doc.Types)
	}
	if !seen[2] {
		t.Fatalf("no 2-element tuple type for the multi-value call g(): types=%v", doc.Types)
	}
}
