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

// TestMRFuncValueUserSelectorVar pins the A1 regression (independent
// review 2026-10-17): a package-level `var openFile = os.Open` in the USER
// package keeps its declaration — the global exists, its initializer
// lowers to the pair-returning __-helper ident, and a call through the
// variable desugars via the linked pair info. Before the fix,
// etaExpandMRFuncValues deleted the declaration while dropping the alias
// map, so the global vanished and every use dangled.
func TestMRFuncValueUserSelectorVar(t *testing.T) {
	doc := exportFuncValueProg(t, `package main

import "os"

var openFile = os.Open

func main() {
	f, err := openFile("x")
	if err == nil {
		f.Close()
	}
}
`)
	init := findGlobalInitByName(t, doc, "openFile")
	if init["kind"] != "ident" {
		t.Fatalf("openFile initializer kind = %q, want ident", init["kind"])
	}
	if name := init["fields"].(map[string]any)["name"]; name != "__open" {
		t.Fatalf("openFile initializer ident name = %v, want __open", name)
	}
	if !funcNames(doc)["__open"] {
		t.Fatal("no __open funcdecl in wire doc")
	}
	// The call through openFile desugars: a __p5mr_N temp holds the pair
	// call, then field extractions assign f and err.
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
		if fun["fields"].(map[string]any)["name"] != "openFile" {
			continue
		}
		found = true
	}
	if !found {
		t.Fatal("no desugared __p5mr_N := openFile() pair-temp vardecl found")
	}
}

// TestMRFuncValueMultiNameSpec pins the multi-name ValueSpec handling: in
// `var lstatFn, answer = os.Lstat, 42` only the aliased name is linked to
// the pair info; `answer` keeps its plain declaration and initializer.
// (The disk-path split in etaExpandMRFuncValues is covered by
// TestEtaExpandDiskModeSplitsMultiNameSpec below.)
func TestMRFuncValueMultiNameSpec(t *testing.T) {
	doc := exportFuncValueProg(t, `package main

import "os"

var lstatFn, answer = os.Lstat, 42

func main() {
	fi, err := lstatFn("x")
	_, _ = fi, err
	println(answer)
}
`)
	lstatInit := findGlobalInitByName(t, doc, "lstatFn")
	if lstatInit["kind"] != "ident" {
		t.Fatalf("lstatFn initializer kind = %q, want ident", lstatInit["kind"])
	}
	if name := lstatInit["fields"].(map[string]any)["name"]; name != "__lstat" {
		t.Fatalf("lstatFn initializer ident name = %v, want __lstat", name)
	}
	// answer keeps its declaration with the plain 42 initializer.
	answerInit := findGlobalInitByName(t, doc, "answer")
	if answerInit["kind"] != "basiclit" {
		t.Fatalf("answer initializer kind = %q, want basiclit", answerInit["kind"])
	}
	if val := answerInit["fields"].(map[string]any)["value"]; val != "42" {
		t.Fatalf("answer initializer value = %v, want 42", val)
	}
}

// typecheckEtaSrc parses and type-checks src (package p) with the frontend's
// overlay importer, so real package-qualified references (os.Lstat) resolve.
func typecheckEtaSrc(t *testing.T, src string) (*ast.File, *types.Info, *types.Package) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, parser.AllErrors)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	info := &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
	}
	conf := types.Config{Importer: overlayImporter{}}
	pkg, err := conf.Check("p", fset, []*ast.File{f}, info)
	if err != nil {
		t.Fatalf("typecheck: %v", err)
	}
	return f, info, pkg
}

// valueSpecNames returns the declared names of every *ast.ValueSpec in f,
// in order.
func valueSpecNames(f *ast.File) [][]string {
	var out [][]string
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			var names []string
			for _, n := range vs.Names {
				names = append(names, n.Name)
			}
			out = append(out, names)
		}
	}
	return out
}

// TestEtaExpandDiskModeSplitsMultiNameSpec is a direct unit test of
// etaExpandMRFuncValues in disk mode (removeAliasedSpecs=true): only the
// names aliased to a multi-result Func are recorded and dropped; the rest
// of a multi-name ValueSpec survives intact. In user/synthetic mode
// (removeAliasedSpecs=false) the AST is untouched.
func TestEtaExpandDiskModeSplitsMultiNameSpec(t *testing.T) {
	const src = `package p

import "os"

var solo = os.Lstat

var multi, answer = os.Lstat, 42

var both, dropped = os.Lstat, os.Hostname
`
	f, info, pkg := typecheckEtaSrc(t, src)
	_, aliases := etaExpandMRFuncValues([]*ast.File{f}, info, pkg, true)
	// solo, multi, both, dropped aliased; answer not.
	if len(aliases) != 4 {
		t.Fatalf("aliases = %d entries, want 4 (solo, multi, both, dropped)", len(aliases))
	}
	for vr, fn := range aliases {
		if fn.Pkg() == nil || fn.Pkg().Path() != "os" || (fn.Name() != "Lstat" && fn.Name() != "Hostname") {
			t.Fatalf("alias %s -> %v.%s, want os.Lstat or os.Hostname", vr.Name(), fn.Pkg(), fn.Name())
		}
		switch vr.Name() {
		case "solo", "multi", "both", "dropped":
		default:
			t.Fatalf("unexpected alias for %q", vr.Name())
		}
	}
	// solo's spec removed entirely; multi's spec keeps `var answer = 42`;
	// both/dropped spec removed entirely.
	var specs [][]string
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs := spec.(*ast.ValueSpec)
			var names []string
			var values []string
			for _, n := range vs.Names {
				names = append(names, n.Name)
			}
			for _, v := range vs.Values {
				values = append(values, types.ExprString(v))
			}
			specs = append(specs, append(names, "="+strings.Join(values, ",")))
		}
	}
	if len(specs) != 1 || len(specs[0]) != 2 || specs[0][0] != "answer" || specs[0][1] != "=42" {
		t.Fatalf("remaining specs = %q, want [[answer =42]]", specs)
	}

	// User/synthetic mode leaves the AST alone and records no aliases.
	f2, info2, pkg2 := typecheckEtaSrc(t, src)
	_, aliases2 := etaExpandMRFuncValues([]*ast.File{f2}, info2, pkg2, false)
	if len(aliases2) != 0 {
		t.Fatalf("user-mode aliases = %d entries, want 0", len(aliases2))
	}
	got := valueSpecNames(f2)
	want := [][]string{{"solo"}, {"multi", "answer"}, {"both", "dropped"}}
	if len(got) != len(want) {
		t.Fatalf("user-mode specs = %q, want %q", got, want)
	}
	for i := range want {
		if strings.Join(got[i], ",") != strings.Join(want[i], ",") {
			t.Fatalf("user-mode specs = %q, want %q", got, want)
		}
	}
}

// TestSyntheticPollAcceptFuncKept pins the synthetic-path half of the A1
// fix (independent review 2026-10-17): buildSyntheticPackage must not
// remove the `var AcceptFunc ... = syscall.Accept` declaration from the
// synthetic internal/poll AST (the live instance cited in the review).
func TestSyntheticPollAcceptFuncKept(t *testing.T) {
	sp := getSyntheticPackage("internal/poll")
	found := false
	for _, f := range sp.files {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, n := range vs.Names {
					if n.Name == "AcceptFunc" {
						found = true
					}
				}
			}
		}
	}
	if !found {
		t.Fatal("internal/poll AcceptFunc ValueSpec removed from synthetic AST")
	}
}

// TestSyntheticSkipNarrowing pins the skip-initializer narrowing in
// importSyntheticPackage: a multi-result function WITH a pair form
// (syscall.Accept -> __accept) must not be skipped, while a fail-fast
// stub with no pair form (time.ParseDuration) still is. The predicate is
// mrPlanForFuncValue: non-nil plan means the value-position lowering
// applies.
func TestSyntheticSkipNarrowing(t *testing.T) {
	e := &exporter{}
	syscallPkg := getSyntheticPackage("syscall")
	acceptObj, ok := syscallPkg.pkg.Scope().Lookup("Accept").(*types.Func)
	if !ok {
		t.Fatal("syscall.Accept not found in synthetic syscall scope")
	}
	if plan := e.mrPlanForFuncValue(acceptObj); plan == nil {
		t.Fatal("mrPlanForFuncValue(syscall.Accept) = nil, want non-nil (has __accept helper)")
	}
	timePkg := getSyntheticPackage("time")
	parseObj, ok := timePkg.pkg.Scope().Lookup("ParseDuration").(*types.Func)
	if !ok {
		t.Fatal("time.ParseDuration not found in synthetic time scope")
	}
	if plan := e.mrPlanForFuncValue(parseObj); plan != nil {
		t.Fatal("mrPlanForFuncValue(time.ParseDuration) non-nil, want nil (fail-fast stub, no pair form)")
	}
}
