// Object-table tests: constants and type names must be described honestly.
//
// These are the positive counterpart of the P1-1 fix: describeObject's old
// default branch emitted every non-Var, non-Func object as a value-less
// "var", silently dropping constant values (`const k = 42`, `math.Pi`,
// `true`) and type names (`time.Duration` in a conversion). Each test
// below fails on the old code (kind "var" with no value) and passes with
// the fix.
//
// Each test is demonstrably capable of failing: the assertions pin the
// exact object kinds the fix introduces.
package emit

import (
	"go/ast"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/parser"
)

// emitProgram runs the full pipeline (parse → check → emit) on src and
// returns the document. A loud emitter error fails the test: these are
// positive tests, every program here must emit.
func emitProgram(t *testing.T, src string) *Document {
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

// findObject returns the object-table entry named name, or nil.
func findObject(doc *Document, name string) map[string]any {
	for _, o := range doc.Objects {
		m, ok := o.(map[string]any)
		if !ok {
			continue
		}
		if m["name"] == name {
			return m
		}
	}
	return nil
}

// TestConstObjectInlined: a declared constant used in the body must be
// described as a const with its inlined value — not as a value-less var.
func TestConstObjectInlined(t *testing.T) {
	doc := emitProgram(t, `package main
const k = 42
func main() { println(k) }
`)
	obj := findObject(doc, "k")
	if obj == nil {
		t.Fatal("object table has no entry for k")
	}
	if obj["kind"] != "const" {
		t.Fatalf("k: kind = %v, want const (was silently a value-less var)", obj["kind"])
	}
	if obj["value"] != "42" {
		t.Fatalf("k: value = %v, want 42 (constant value must be inlined)", obj["value"])
	}
}

// TestUniverseConstInlined: universe constants (true/false) are *types.Const
// too — they must not become value-less vars either.
func TestUniverseConstInlined(t *testing.T) {
	doc := emitProgram(t, `package main
func main() {
	if true {
		println(1)
	}
}
`)
	obj := findObject(doc, "true")
	if obj == nil {
		t.Fatal("object table has no entry for true")
	}
	if obj["kind"] != "const" {
		t.Fatalf("true: kind = %v, want const", obj["kind"])
	}
	if obj["value"] != "true" {
		t.Fatalf("true: value = %v, want true", obj["value"])
	}
}

// TestTypeNameObjectReference: a type name in value position (here the fun
// of a conversion) must be described as a type reference, not a var.
func TestTypeNameObjectReference(t *testing.T) {
	doc := emitProgram(t, `package main
type MyInt int
func main() {
	x := MyInt(42)
	println(x)
}
`)
	obj := findObject(doc, "MyInt")
	if obj == nil {
		t.Fatal("object table has no entry for MyInt")
	}
	if obj["kind"] != "typename" {
		t.Fatalf("MyInt: kind = %v, want typename", obj["kind"])
	}
	if _, ok := obj["type"]; !ok {
		t.Fatal("MyInt: typename object carries no type reference")
	}
}

// TestPkgRefConstInlined: math.Pi reaches declareObject through the pkgref
// path (emitSelector) as a *types.Const — it must be inlined, not var-ified.
func TestPkgRefConstInlined(t *testing.T) {
	doc := emitProgram(t, `package main
import "math"
func main() { println(math.Pi) }
`)
	obj := findObject(doc, "Pi")
	if obj == nil {
		t.Fatal("object table has no entry for Pi")
	}
	if obj["kind"] != "const" {
		t.Fatalf("Pi: kind = %v, want const", obj["kind"])
	}
	// The value must be inlined (go/constant's exact form — a rational
	// like 31415…/100000… for Pi). The regression was a value-less var.
	v, _ := obj["value"].(string)
	if v == "" {
		t.Fatal("Pi: const object carries no inlined value")
	}
}

// TestAliasUnaliased: aliases (including the universe aliases any/byte/rune)
// are transparent — the type table must carry the actual type, never a
// silent basic/"unknown" fallback.
func TestAliasUnaliased(t *testing.T) {
	for _, src := range []string{
		"package main\ntype A = int\nfunc main() {\n\tvar x A = 1\n\tprintln(x)\n}\n",
		"package main\nfunc main() {\n\tvar x any = 1\n\tprintln(x)\n}\n",
	} {
		doc := emitProgram(t, src)
		for _, typ := range doc.Types {
			m, ok := typ.(map[string]any)
			if !ok {
				continue
			}
			if m["kind"] == "basic" {
				if d, ok := m["detail"].(map[string]any); ok && d["class"] == "unknown" {
					t.Fatalf("type table contains silent unknown fallback: %v", m)
				}
			}
		}
	}
}

// TestKeyedCompositeLiteralEmits: KeyValueExpr is the one documented
// exception to exprType's fail-loud rule — go/types never records a type
// for a composite-literal element, so its null type is by design. This
// guards the exception against regressions in either direction.
func TestKeyedCompositeLiteralEmits(t *testing.T) {
	doc := emitProgram(t, `package main
func main() {
	m := map[string]int{"a": 1}
	println(m["a"])
}
`)
	found := false
	var walk func(n any)
	walk = func(n any) {
		switch v := n.(type) {
		case map[string]any:
			if v["kind"] == "keyvalue" {
				found = true
				fields, _ := v["fields"].(map[string]any)
				if fields["type"] != nil {
					t.Fatalf("keyvalue node type = %v, want null by design", fields["type"])
				}
			}
			for _, c := range v {
				walk(c)
			}
		case []any:
			for _, c := range v {
				walk(c)
			}
		}
	}
	for _, f := range doc.Funcs {
		walk(f)
	}
	if !found {
		t.Fatal("no keyvalue node in emitted document")
	}
}

// TestExprTypeFailsLoudlyOnMissingType: exprType must fail loudly when
// go/types recorded no type for an expression — never a silent nil.
// (White-box: drives exprType directly with an empty type map.)
func TestExprTypeFailsLoudlyOnMissingType(t *testing.T) {
	e := &Emitter{
		info: &checker.Result{
			Info: &types.Info{Types: map[ast.Expr]types.TypeAndValue{}},
		},
	}
	if _, err := e.exprType(&ast.Ident{Name: "zzz"}); err == nil {
		t.Fatal("exprType: expected loud failure for expression with no recorded type, got nil")
	} else if !strings.Contains(err.Error(), "no type recorded") {
		t.Fatalf("exprType: error %q does not contain %q", err.Error(), "no type recorded")
	}
	// The documented exception: KeyValueExpr keeps a null type by design.
	kv := &ast.KeyValueExpr{Key: &ast.BasicLit{Value: `"a"`}, Value: &ast.BasicLit{Value: "1"}}
	typ, err := e.exprType(kv)
	if err != nil {
		t.Fatalf("exprType(KeyValueExpr): expected (nil, nil) by design, got error %v", err)
	}
	if typ != nil {
		t.Fatalf("exprType(KeyValueExpr): expected nil type by design, got %v", typ)
	}
}
