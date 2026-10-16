// Regression tests for constant folding and honest object kinds.
//
// A package-level `const K = 5` used to be described as {"kind":"var"}
// with no value; the backend then read an uninitialized frame slot, so
// `println(K)` silently printed 0. Function-level const declarations were
// rejected loudly, and *types.TypeName/Label/PkgName were likewise
// mislabeled "var".
//
// Constants now fold to basiclit nodes at their use sites (Go constants
// have no runtime storage), and every types.Object implementation gets its
// honest object-table kind.
package emit

import (
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/parser"
)

// emitResult runs parse → check → emit, returning the document or the error.
func emitResult(t *testing.T, src string) (*Document, error) {
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
	return Emit(cres, pres.Pkg, pres.Files)
}

// mustEmit runs the pipeline and fails the test on any stage error.
func mustEmit(t *testing.T, src string) *Document {
	t.Helper()
	doc, err := emitResult(t, src)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	return doc
}

// callArg navigates doc.Funcs[0].body.stmts[stmtIdx].(exprstmt).x.(call).args[0].
func callArg(t *testing.T, doc *Document, stmtIdx int) map[string]any {
	t.Helper()
	fn, ok := doc.Funcs[0].(map[string]any)
	if !ok {
		t.Fatal("funcs[0] is not a map")
	}
	fields := fn["fields"].(map[string]any)
	body := fields["body"].(map[string]any)
	stmts := body["fields"].(map[string]any)["stmts"].([]any)
	stmt := stmts[stmtIdx].(map[string]any)
	x := stmt["fields"].(map[string]any)["x"].(map[string]any)
	args := x["fields"].(map[string]any)["args"].([]any)
	return args[0].(map[string]any)
}

// TestConstPackageLevelFolds: a package-level named constant reads back
// its value. Before the fix the const leaked into the object table as
// {"kind":"var"} with no value (silent wrong code downstream).
func TestConstPackageLevelFolds(t *testing.T) {
	doc := mustEmit(t, `package main

const K = 5

func main() {
	println(K)
}
`)
	for _, o := range doc.Objects {
		om, ok := o.(map[string]any)
		if !ok {
			t.Fatalf("object entry is not a map: %#v", o)
		}
		if om["name"] == "K" {
			t.Fatalf("const K leaked into object table: %#v", om)
		}
	}
	arg := callArg(t, doc, 0)
	if arg["kind"] != "basiclit" {
		t.Fatalf("println arg kind = %v, want basiclit (folded const)", arg["kind"])
	}
	if v := arg["fields"].(map[string]any)["value"]; v != "5" {
		t.Fatalf("folded value = %v, want %q", v, "5")
	}
}

// TestConstFunctionLevelFolds: a function-level named constant reads back
// its value. Before the fix this failed with "unsupported decl stmt".
func TestConstFunctionLevelFolds(t *testing.T) {
	doc := mustEmit(t, `package main

func main() {
	const K = 5
	println(K)
}
`)
	arg := callArg(t, doc, 1)
	if arg["kind"] != "basiclit" {
		t.Fatalf("println arg kind = %v, want basiclit (folded const)", arg["kind"])
	}
	if v := arg["fields"].(map[string]any)["value"]; v != "5" {
		t.Fatalf("folded value = %v, want %q", v, "5")
	}
}

// TestConstValuesFold: every constant flavor folds to its source-syntax
// literal — iota, typed consts, constant expressions, strings, bools,
// floats, runes.
func TestConstValuesFold(t *testing.T) {
	doc := mustEmit(t, `package main

const (
	A = iota
	B
)
const C float64 = 2.5
const S = "hi"
const T = true
const E = 1 + 2*3
const R = 'a'

func main() {
	println(A)
	println(B)
	println(C)
	println(S)
	println(T)
	println(E)
	println(R)
}
`)
	want := []string{"0", "1", "2.5", `"hi"`, "true", "7", "97"}
	for i, w := range want {
		arg := callArg(t, doc, i)
		if arg["kind"] != "basiclit" {
			t.Fatalf("stmt %d: arg kind = %v, want basiclit", i, arg["kind"])
		}
		if v := arg["fields"].(map[string]any)["value"]; v != w {
			t.Fatalf("stmt %d: folded value = %v, want %q", i, v, w)
		}
	}
}

// TestUniverseBoolConstsFold: true/false are *types.Const in go/types, not
// builtins — they hit the same mislabeling path as named consts.
func TestUniverseBoolConstsFold(t *testing.T) {
	doc := mustEmit(t, `package main

func main() {
	println(true)
	println(false)
}
`)
	for i, w := range []string{"true", "false"} {
		arg := callArg(t, doc, i)
		if arg["kind"] != "basiclit" {
			t.Fatalf("stmt %d: arg kind = %v, want basiclit", i, arg["kind"])
		}
		if v := arg["fields"].(map[string]any)["value"]; v != w {
			t.Fatalf("stmt %d: folded value = %v, want %q", i, v, w)
		}
	}
}

// TestNilIdent: nil is *types.Nil, not a builtin — it used to be
// mislabeled "var" too. It now emits the "nil" builtin marker the backend
// already understands.
func TestNilIdent(t *testing.T) {
	doc := mustEmit(t, `package main

func main() {
	var p *int = nil
	println(p == nil)
}
`)
	// vardecl value for p.
	fn := doc.Funcs[0].(map[string]any)
	body := fn["fields"].(map[string]any)["body"].(map[string]any)
	stmts := body["fields"].(map[string]any)["stmts"].([]any)
	vd := stmts[0].(map[string]any)
	vals := vd["fields"].(map[string]any)["values"].([]any)
	nilNode, ok := vals[0].(map[string]any)
	if !ok {
		t.Fatalf("vardecl value is not a node: %#v", vals[0])
	}
	if nilNode["kind"] != "ident" || nilNode["fields"].(map[string]any)["builtin"] != "nil" {
		t.Fatalf("nil did not emit builtin marker: %#v", nilNode)
	}
}

// TestDescribeObjectKinds: every types.Object implementation gets its
// honest object-table kind — never a silent "var".
func TestDescribeObjectKinds(t *testing.T) {
	src := `package main

import "fmt"

type T struct{ X int }

const K = 5

func main() {
	_ = K
	_ = fmt.Sprint
	_ = T{}
	var v int
	_ = v
loop:
	for {
		break loop
	}
}
`
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
	byType := map[string]types.Object{}
	for _, obj := range cres.Info.Defs {
		if obj == nil {
			continue
		}
		byType[typeName(obj)] = obj
	}
	for _, obj := range cres.Info.Uses {
		if obj == nil {
			continue
		}
		byType[typeName(obj)] = obj
	}
	wantKinds := map[string]string{
		"*types.Var":      "var",
		"*types.Const":    "const",
		"*types.Func":     "func",
		"*types.TypeName": "typename",
		"*types.Label":    "label",
		"*types.PkgName":  "pkgname",
	}
	e := &Emitter{typeIDs: make(map[types.Type]int)}
	for typ, want := range wantKinds {
		obj, ok := byType[typ]
		if !ok {
			t.Fatalf("no %s object found in test program", typ)
		}
		d, err := e.describeObject(obj, false)
		if err != nil {
			t.Fatalf("describeObject(%s %q): %v", typ, obj.Name(), err)
		}
		if d["kind"] != want {
			t.Fatalf("describeObject(%s %q): kind = %v, want %q", typ, obj.Name(), d["kind"], want)
		}
	}
	// The const entry carries its value.
	d, err := e.describeObject(byType["*types.Const"], false)
	if err != nil {
		t.Fatal(err)
	}
	if d["value"] != "5" {
		t.Fatalf("const object value = %v, want %q", d["value"], "5")
	}
	// Builtin and nil objects get honest kinds too.
	d, err = e.describeObject(types.Universe.Lookup("len"), false)
	if err != nil {
		t.Fatal(err)
	}
	if d["kind"] != "builtin" {
		t.Fatalf("builtin object kind = %v, want builtin", d["kind"])
	}
	d, err = e.describeObject(types.Universe.Lookup("nil"), false)
	if err != nil {
		t.Fatal(err)
	}
	if d["kind"] != "nil" {
		t.Fatalf("nil object kind = %v, want nil", d["kind"])
	}
}

// typeName is the concrete go/types object type, for test bucketing.
func typeName(obj types.Object) string {
	switch obj.(type) {
	case *types.Var:
		return "*types.Var"
	case *types.Const:
		return "*types.Const"
	case *types.Func:
		return "*types.Func"
	case *types.TypeName:
		return "*types.TypeName"
	case *types.Label:
		return "*types.Label"
	case *types.PkgName:
		return "*types.PkgName"
	}
	return "other"
}

// TestConstImaginaryFailsLoud: complex/imaginary constants cannot be
// rendered as source-syntax literals the backend parses — loud, never
// silent.
func TestConstImaginaryFailsLoud(t *testing.T) {
	_, err := emitResult(t, `package main

const C = 1i

func main() {
	println(C)
}
`)
	if err == nil {
		t.Fatal("expected loud error for imaginary const, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported const") {
		t.Fatalf("error = %q, want it to mention \"unsupported const\"", err)
	}
}
