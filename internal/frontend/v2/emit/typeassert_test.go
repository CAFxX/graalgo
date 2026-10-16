// Positive tests for type assertions and type switches.
//
// These constructs were previously loudly rejected; they now emit
// correctly. The tests feed minimal programs through the full pipeline
// (parse → check → emit) and verify the emitted IR has the expected
// structure.
package emit

import (
	"os"
	"path/filepath"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/parser"
)

func emitSource(t *testing.T, src string) *Document {
	t.Helper()
	dir := t.TempDir()
	fn := filepath.Join(dir, "test.go")
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

// findNode searches for a node with the given kind in the document.
func findNode(doc *Document, kind string) map[string]any {
	var search func(n any) map[string]any
	search = func(n any) map[string]any {
		if m, ok := n.(map[string]any); ok {
			if m["kind"] == kind {
				return m
			}
			if fields, ok := m["fields"].(map[string]any); ok {
				for _, v := range fields {
					if found := search(v); found != nil {
						return found
					}
				}
			}
			// Also search in slices.
			for _, v := range m {
				if found := search(v); found != nil {
					return found
				}
			}
		}
		if s, ok := n.([]any); ok {
			for _, v := range s {
				if found := search(v); found != nil {
					return found
				}
			}
		}
		return nil
	}
	for _, f := range doc.Funcs {
		if found := search(f); found != nil {
			return found
		}
	}
	return nil
}

func TestTypeAssertSingleValue(t *testing.T) {
	doc := emitSource(t, `package main
func main() {
	var x any = 42
	y := x.(int)
	println(y)
}
`)
	n := findNode(doc, "typeassert")
	if n == nil {
		t.Fatal("expected typeassert node in emitted IR")
	}
	fields := n["fields"].(map[string]any)
	if fields["type"] == nil {
		t.Error("typeassert should have a non-nil type for x.(int)")
	}
	// The x should be an ident.
	x := fields["x"].(map[string]any)
	if x["kind"] != "ident" {
		t.Errorf("typeassert x should be ident, got %v", x["kind"])
	}
}

func TestTypeAssertCommaOk(t *testing.T) {
	doc := emitSource(t, `package main
func main() {
	var x any = 42
	y, ok := x.(int)
	println(y, ok)
}
`)
	n := findNode(doc, "typeassert")
	if n == nil {
		t.Fatal("expected typeassert node in emitted IR")
	}
	// Comma-ok is a two-value assignment; the typeassert itself is the same.
	// The distinction is in how the parent assign handles it.
	fields := n["fields"].(map[string]any)
	if fields["type"] == nil {
		t.Error("typeassert should have a non-nil type")
	}
}

func TestTypeSwitch(t *testing.T) {
	doc := emitSource(t, `package main
func main() {
	var x any = 1
	switch v := x.(type) {
	case int:
		println(v)
	case string:
		println(v)
	default:
		println("other")
	}
}
`)
	n := findNode(doc, "typeswitch")
	if n == nil {
		t.Fatal("expected typeswitch node in emitted IR")
	}
	fields := n["fields"].(map[string]any)
	cases := fields["cases"].([]any)
	if len(cases) != 3 {
		t.Fatalf("expected 3 cases (int, string, default), got %d", len(cases))
	}
	// First case should have 1 type, second 1 type, third 0 (default).
	c0 := cases[0].(map[string]any)["fields"].(map[string]any)
	c1 := cases[1].(map[string]any)["fields"].(map[string]any)
	c2 := cases[2].(map[string]any)["fields"].(map[string]any)
	if len(c0["types"].([]any)) != 1 {
		t.Errorf("case 0 should have 1 type, got %d", len(c0["types"].([]any)))
	}
	if len(c1["types"].([]any)) != 1 {
		t.Errorf("case 1 should have 1 type, got %d", len(c1["types"].([]any)))
	}
	if len(c2["types"].([]any)) != 0 {
		t.Errorf("default case should have 0 types, got %d", len(c2["types"].([]any)))
	}
	// Guard should be present (the v := x.(type) assignment).
	if fields["guard"] == nil {
		t.Error("typeswitch should have a guard assignment")
	}
}

func TestTypeSwitchNoAssign(t *testing.T) {
	doc := emitSource(t, `package main
func main() {
	var x any = 1
	switch x.(type) {
	case int:
		println("int")
	default:
		println("other")
	}
}
`)
	n := findNode(doc, "typeswitch")
	if n == nil {
		t.Fatal("expected typeswitch node in emitted IR")
	}
	fields := n["fields"].(map[string]any)
	cases := fields["cases"].([]any)
	if len(cases) != 2 {
		t.Fatalf("expected 2 cases, got %d", len(cases))
	}
}

func TestTypeSwitchMultipleTypes(t *testing.T) {
	doc := emitSource(t, `package main
func main() {
	var x any = 1
	switch v := x.(type) {
	case int, int64:
		println(v)
	default:
	}
}
`)
	n := findNode(doc, "typeswitch")
	if n == nil {
		t.Fatal("expected typeswitch node in emitted IR")
	}
	fields := n["fields"].(map[string]any)
	cases := fields["cases"].([]any)
	c0 := cases[0].(map[string]any)["fields"].(map[string]any)
	if len(c0["types"].([]any)) != 2 {
		t.Errorf("case with int, int64 should have 2 types, got %d", len(c0["types"].([]any)))
	}
}
