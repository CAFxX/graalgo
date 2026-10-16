package main

// P3-M10 (PKG-035): exporter tests for multi-result function values in
// value position.
//
// Red-green: before the fix, a multi-result named function used as a
// value is a scope error — for synthetic table-driven functions (e.g.
// syscall.Accept) "in value position is not supported in frontend/v1
// scope (M5): multi-result function values require v2"; for user
// functions the value forms but a call through the variable fails with
// "multi-valued call result is not supported in frontend/v1 scope
// (WS D)". After the fix, the value is the pair-returning implementation
// (the __-helper for table-driven functions, the transformed funcdecl
// for user functions) and call sites through a variable holding it
// desugar exactly like direct calls (userMRVars linking).
//
//  1. TestMRFuncValueSyntheticAccept pins the exact reported construct:
//     var AcceptFunc func(int) (int, syscall.Sockaddr, error) =
//     syscall.Accept — the initializer lowers to an ident referencing
//     the __accept helper's funcdecl.
//  2. TestMRFuncValueUserPackageVar pins a package-level
//     var F func() (int, error) = two plus a, err := F(): the call
//     desugars through a __p5mr_N pair temp.
//  3. TestMRFuncValueUserDefine pins the same for F := two in main.
//  4. TestMRFuncValueFailFastKept pins the boundary: an M5-kind
//     multi-result function with no drop-in __-helper value form
//     (time.ParseDuration) in value position keeps the honest M5
//     compile-time error, exactly as before.

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"
)

// exportFuncValueProg exports src, failing the test on export error, and
// returns the decoded wire doc.
func exportFuncValueProg(t *testing.T, src string) map[string]any {
	t.Helper()
	out, err := ExportFile(writeIfaceProg(t, src))
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	return decodeExport(t, out)
}

// findGlobalInitByName returns the init node of the package-level global
// whose object-table name matches (synthetic package vars land in the
// doc's "globals" array, not as vardecl nodes).
func findGlobalInitByName(t *testing.T, doc map[string]any, name string) map[string]any {
	t.Helper()
	objects, _ := doc["objects"].([]any)
	globals, _ := doc["globals"].([]any)
	for _, g := range globals {
		gm, _ := g.(map[string]any)
		oidF, _ := gm["obj"].(float64)
		oid := int(oidF)
		if oid < 0 || oid >= len(objects) {
			continue
		}
		om, _ := objects[oid].(map[string]any)
		if om["name"] == name {
			init, _ := gm["init"].(map[string]any)
			if init == nil {
				t.Fatalf("global %q has no init", name)
			}
			return init
		}
	}
	t.Fatalf("no global named %q", name)
	return nil
}

// funcNames returns the set of funcdecl names in the wire doc.
func funcNames(doc map[string]any) map[string]bool {
	out := map[string]bool{}
	for _, f := range findAllKind(doc, "funcdecl") {
		if name, _ := f["fields"].(map[string]any)["name"].(string); name != "" {
			out[name] = true
		}
	}
	return out
}

func TestMRFuncValueSyntheticAccept(t *testing.T) {
	doc := exportFuncValueProg(t, `package main

import "syscall"

var AcceptFunc func(int) (int, syscall.Sockaddr, error) = syscall.Accept

func main() {}
`)
	// The var's initializer is an ident referencing the __accept
	// helper's funcdecl — the pair-returning implementation a direct
	// syscall.Accept(fd) call desugars to.
	init := findGlobalInitByName(t, doc, "AcceptFunc")
	if init["kind"] != "ident" {
		t.Fatalf("AcceptFunc initializer kind = %q, want ident", init["kind"])
	}
	if name := init["fields"].(map[string]any)["name"]; name != "__accept" {
		t.Fatalf("AcceptFunc initializer ident name = %v, want __accept", name)
	}
	if !funcNames(doc)["__accept"] {
		t.Fatal("no __accept funcdecl in wire doc")
	}
}

func TestMRFuncValueUserPackageVar(t *testing.T) {
	doc := exportFuncValueProg(t, `package main

func two() (int, error) { return 1, nil }

var F func() (int, error) = two

func main() {
	a, err := F()
	println(a, err == nil)
}
`)
	// The call through F desugars: a __p5mr_N temp holds the pair call,
	// then field extractions assign a and err.
	found := false
	for _, n := range findAllKind(doc, "vardecl") {
		names, _ := n["fields"].(map[string]any)["names"].([]any)
		if len(names) != 1 {
			continue
		}
		name, _ := names[0].(string)
		if !strings.HasPrefix(name, "__p5mr_") {
			continue
		}
		vals, _ := n["fields"].(map[string]any)["values"].([]any)
		if len(vals) != 1 {
			continue
		}
		call, _ := vals[0].(map[string]any)
		if call["kind"] != "call" {
			continue
		}
		fun, _ := call["fields"].(map[string]any)["fun"].(map[string]any)
		if fun == nil || fun["kind"] != "ident" {
			continue
		}
		if fun["fields"].(map[string]any)["name"] != "F" {
			continue
		}
		found = true
	}
	if !found {
		t.Fatal("no desugared __p5mr_N := F() pair-temp vardecl found")
	}
}

func TestMRFuncValueUserDefine(t *testing.T) {
	doc := exportFuncValueProg(t, `package main

func two() (int, error) { return 2, nil }

func main() {
	F := two
	a, err := F()
	println(a, err == nil)
}
`)
	found := false
	for _, n := range findAllKind(doc, "vardecl") {
		names, _ := n["fields"].(map[string]any)["names"].([]any)
		if len(names) != 1 {
			continue
		}
		name, _ := names[0].(string)
		if !strings.HasPrefix(name, "__p5mr_") {
			continue
		}
		vals, _ := n["fields"].(map[string]any)["values"].([]any)
		if len(vals) != 1 {
			continue
		}
		call, _ := vals[0].(map[string]any)
		if call["kind"] != "call" {
			continue
		}
		fun, _ := call["fields"].(map[string]any)["fun"].(map[string]any)
		if fun == nil || fun["kind"] != "ident" {
			continue
		}
		if fun["fields"].(map[string]any)["name"] != "F" {
			continue
		}
		found = true
	}
	if !found {
		t.Fatal("no desugared __p5mr_N := F() pair-temp vardecl found")
	}
}

func TestMRFuncValueFailFastKept(t *testing.T) {
	// time.ParseDuration is an M5-kind multi-result function whose
	// __-helper is not a drop-in value form (and it is a fail-fast stub
	// in the value-position tables): value position keeps the honest
	// compile-time M5 error, exactly as before this change.
	_, err := ExportFile(writeIfaceProg(t, `package main

import "time"

var F func(string) (time.Duration, error) = time.ParseDuration

func main() {}
`))
	if err == nil {
		t.Fatal("expected M5 scope error for time.ParseDuration in value position, got nil")
	}
	if !strings.Contains(err.Error(), "multi-result function values require v2") {
		t.Fatalf("error = %q, want the M5 value-position scope error", err.Error())
	}
}

// TestEtaExpandMRFuncValueSpecTrimsOnlyAliasedPair is the regression test
// for the whole-ValueSpec removal bug in etaExpandMRFuncValues: given
// `var lstat, answer = os.Lstat, 42`, the alias lstat -> os.Lstat must be
// recorded while ONLY that (name, value) pair is dropped from the spec.
// The old code queued the whole spec for removal, silently deleting the
// sibling declaration `answer` (the exporter then emitted no vardecl for
// it, leaving dangling references downstream).
func TestEtaExpandMRFuncValueSpecTrimsOnlyAliasedPair(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", `package p

import "os"

var lstat, answer = os.Lstat, 42
`, 0)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	// Mirror the disk-import pipeline order: MR transform, type-check,
	// then the eta-expansion pass.
	transformDiskMultiResult("example.com/p", []*ast.File{f})
	info := &types.Info{
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
	}
	conf := types.Config{Importer: importer.Default()}
	pkg, err := conf.Check("example.com/p", fset, []*ast.File{f}, info)
	if err != nil {
		t.Fatalf("type error: %v", err)
	}
	_, aliases := etaExpandMRFuncValues([]*ast.File{f}, info, pkg)

	// 1. The alias handling still works: lstat -> os.Lstat is recorded.
	var lstatVar *types.Var
	for id, obj := range info.Defs {
		if v, ok := obj.(*types.Var); ok && id.Name == "lstat" {
			lstatVar = v
		}
	}
	if lstatVar == nil {
		t.Fatal("no *types.Var for lstat in info.Defs")
	}
	aliased, ok := aliases[lstatVar]
	if !ok {
		t.Fatal("alias for lstat not recorded")
	}
	if aliased.Name() != "Lstat" || aliased.Pkg() == nil || aliased.Pkg().Path() != "os" {
		t.Fatalf("alias target = %v, want os.Lstat", aliased)
	}

	// 2. The sibling declaration survives: exactly one var ValueSpec
	// remains, declaring `answer` initialized to 42, with no mention of
	// `lstat`.
	var specs []*ast.ValueSpec
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, s := range gd.Specs {
			if vs, ok := s.(*ast.ValueSpec); ok {
				specs = append(specs, vs)
			}
		}
	}
	if len(specs) != 1 {
		t.Fatalf("got %d var ValueSpecs, want 1 (the surviving sibling)", len(specs))
	}
	vs := specs[0]
	if len(vs.Names) != 1 || vs.Names[0].Name != "answer" {
		t.Fatalf("surviving spec names = %v, want [answer]", vs.Names)
	}
	if len(vs.Values) != 1 {
		t.Fatalf("surviving spec has %d values, want 1", len(vs.Values))
	}
	lit, ok := vs.Values[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.INT || lit.Value != "42" {
		t.Fatalf("surviving spec value = %#v, want INT 42", vs.Values[0])
	}
}

// TestMRFuncValueSiblingDeclarationExports is the end-to-end companion:
// the sibling of an aliased multi-result function value must still be
// exported (as a package-level global). The aliased name's declaration
// must be gone — it is served by the alias map, not by an export node.
// (The alias var itself is unused here; the alias map is consumed by the
// disk-import lane, which ExportFile does not wire.)
func TestMRFuncValueSiblingDeclarationExports(t *testing.T) {
	doc := exportFuncValueProg(t, `package main

import "os"

var lstat, answer = os.Lstat, 42

func main() {
	println(answer)
}
`)
	init := findGlobalInitByName(t, doc, "answer")
	if init["kind"] != "basiclit" {
		t.Fatalf("answer initializer kind = %q, want basiclit", init["kind"])
	}
	if val := init["fields"].(map[string]any)["value"]; val != "42" {
		t.Fatalf("answer initializer value = %v, want 42", val)
	}
	// The aliased name's declaration must be gone (it is served by the
	// alias map, not by a global): no object named "lstat" may exist.
	objects, _ := doc["objects"].([]any)
	for _, o := range objects {
		om, _ := o.(map[string]any)
		if om["name"] == "lstat" {
			t.Fatal(`aliased name "lstat" still has an exported object`)
		}
	}
}
