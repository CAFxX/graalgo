// Mechanical Go AST-node coverage matrix.
//
// The node list is DERIVED from the go/ast package sources: every struct
// type declaring both Pos() and End() methods. ast.Node is exactly
// { Pos() token.Pos; End() token.Pos }, so this is precisely the set of
// types implementing ast.Node — nothing is hand-enumerated. If go/ast gains
// a node type, it appears here and this test fails until the new type is
// classified.
//
// Each classification is asserted against actual emitter behavior, so the
// matrix cannot drift from the emitter:
//   - "handled" / "via go/types": a positive probe — a minimal program run
//     through parse → check → emit — must succeed, and the probe's parsed
//     AST must actually contain the node kind (a probe that stops
//     exercising its node fails).
//   - "loudly rejected": a negative probe must fail with an "unsupported"
//     error — src-based cases in negative_test.go (whose parsed AST must
//     contain the node kind), synthetic-node direct probes below, or a
//     documented shared probe.
//
// His 2026-10-15 order: full Go 1.27 syntax from the start, including
// generics. "That should obviously skip the json" — v2 constructs the
// in-memory program directly; there is no JSON wire format.
package emit

import (
	"go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/parser"
)

// handledNodes is the set of concrete go/ast types the emitter supports.
// Each entry has a positive probe in positiveProbes below; the probe test
// fails if the emitter ever stops handling the node.
var handledNodes = map[string]bool{
	// Declarations (top-level).
	"FuncDecl": true, // emitFuncDecl

	// Statements.
	"AssignStmt": true,
	"BlockStmt":  true, // via emitBlock
	"BranchStmt": true,
	"DeclStmt":   true, // var only; const/type decls fail loudly
	"ExprStmt":   true,
	"ForStmt":    true,
	"IfStmt":     true,
	"IncDecStmt": true,
	"RangeStmt":  true,
	"ReturnStmt": true,

	// Expressions.
	"BasicLit":     true,
	"BinaryExpr":   true,
	"CallExpr":     true,
	"CompositeLit": true, // emitCompositeLit
	"Ident":        true,
	"IndexExpr":    true, // emitIndex; generic instantiation fails loudly
	"KeyValueExpr": true, // inside emitCompositeLit only; a bare KeyValueExpr is rejected
	"ParenExpr":    true,
	"SelectorExpr": true,
	"SliceExpr":    true, // emitSlice
	"UnaryExpr":    true,

	// Type expressions handled via go/types (not AST switch).
	// These appear as types, not as standalone emitExpr cases:
	// ArrayType, ChanType, FuncType, InterfaceType, MapType, StructType
	// are resolved through e.typeID(tv.Type) / e.exprType().
}

// typeExprNodes are AST type expressions that never reach emitExpr's switch
// because they are handled via go/types type IDs. They are covered.
var typeExprNodes = map[string]bool{
	"ArrayType":     true,
	"ChanType":      true,
	"FuncType":      true,
	"InterfaceType": true,
	"MapType":       true,
	"StructType":    true,
}

// structuralNodes are go/ast types that are not emittable nodes in the
// pipeline's sense: they are containers, metadata, or handled implicitly.
// Each must have a justification.
var structuralNodes = map[string]string{
	"Comment":      "documentation only; not semantic",
	"CommentGroup": "documentation only; not semantic",
	"Field":        "handled via FieldList in signatures",
	"FieldList":    "handled via FuncType params/results",
	"File":         "top-level container; iterated in emitDocument",
	"Package":      "top-level container; iterated in emitDocument",
	"ImportSpec":   "import resolution is checker's job; not emitted",
	"Directive":    "compiler directives (//go:...); preserved via go/parser, not emitted as IR",
}

// nonNodeTypes are exported go/ast struct types that do NOT implement
// ast.Node (no Pos/End methods). This map is asserted equal to the derived
// set, so a new exported non-node struct forces documentation here.
var nonNodeTypes = map[string]string{
	"DirectiveArg": "not an ast.Node; argument to a //go: directive",
	"Object":       "not an ast.Node; legacy go/ast object model (v2 uses go/types)",
	"Scope":        "not an ast.Node; legacy go/ast scope model (v2 uses go/types)",
}

// loudlyRejected are node kinds the emitter explicitly rejects with an
// "unsupported" error. Every entry has a negative probe: src-based cases in
// negative_test.go, direct synthetic-node probes below, or a documented
// shared probe in probeAliases. If a construct starts being handled, its
// probe fails and the entry moves to handledNodes.
var loudlyRejected = map[string]bool{
	// Statements not yet implemented.
	"BadStmt":        true,
	"DeferStmt":      true,
	"EmptyStmt":      true,
	"GoStmt":         true,
	"LabeledStmt":    true,
	"SelectStmt":     true,
	"SendStmt":       true,
	"SwitchStmt":     true,
	"TypeSwitchStmt": true,
	// Expressions not yet implemented.
	"BadExpr":        true,
	"Ellipsis":       true, // variadic spread; ...T in signatures goes through go/types
	"FuncLit":        true,
	"IndexListExpr":  true, // multi-arg generics instantiation
	"StarExpr":       true, // dereference; pointer types go through go/types
	"TypeAssertExpr": true,
	// Declarations not yet implemented.
	"BadDecl":   true,
	"GenDecl":   true, // const/type decls in statement position
	"TypeSpec":  true, // rejected via the GenDecl path in emitDeclStmt
	"ValueSpec": true, // rejected via the GenDecl path in emitDeclStmt
	// Clauses (only appear inside switch/select).
	"CaseClause": true,
	"CommClause": true,
}

// probeAliases documents rejected nodes whose negative probe is shared with
// another node's probe because the emitter rejects them together.
var probeAliases = map[string]string{
	// A const/type ValueSpec is never dispatched on its own: emitDeclStmt
	// rejects the enclosing GenDecl, so the GenDecl probe exercises both.
	"ValueSpec": "GenDecl",
}

// positiveProbes maps each handled / via-go-types node kind to a minimal
// program exercising it. Every probe must (a) parse to an AST that actually
// contains the node kind, and (b) emit successfully. The probe test enforces
// both, so neither the matrix nor the probes can drift silently.
var positiveProbes = map[string]string{
	// Declarations.
	"FuncDecl": `package main
func main() {}
`,
	// Statements.
	"AssignStmt": `package main
func main() {
	x := 1
	x = 2
	_ = x
}
`,
	"BlockStmt": `package main
func main() {}
`,
	"BranchStmt": `package main
func main() {
	for {
		break
	}
}
`,
	"DeclStmt": `package main
func main() {
	var x int
	_ = x
}
`,
	"ExprStmt": `package main
func main() {
	println("x")
}
`,
	"ForStmt": `package main
func main() {
	for i := 0; i < 3; i++ {
		_ = i
	}
}
`,
	"IfStmt": `package main
func main() {
	x := 1
	if x > 0 {
		_ = x
	}
}
`,
	"IncDecStmt": `package main
func main() {
	i := 0
	i++
	_ = i
}
`,
	"RangeStmt": `package main
func main() {
	for _, v := range []int{1, 2} {
		_ = v
	}
}
`,
	"ReturnStmt": `package main
func fortyTwo() int {
	return 42
}
func main() {
	_ = fortyTwo()
}
`,
	// Expressions.
	"BasicLit": `package main
func main() {
	_ = 42
}
`,
	"BinaryExpr": `package main
func main() {
	_ = 1 + 2
}
`,
	"CallExpr": `package main
func main() {
	println("x")
}
`,
	"CompositeLit": `package main
func main() {
	_ = []int{1, 2}
}
`,
	"Ident": `package main
func main() {
	x := 1
	_ = x
}
`,
	"IndexExpr": `package main
func main() {
	s := []int{1, 2}
	_ = s[0]
}
`,
	"KeyValueExpr": `package main
type Point struct{ X, Y int }
func main() {
	_ = Point{X: 1, Y: 2}
}
`,
	"ParenExpr": `package main
func main() {
	_ = (1 + 2)
}
`,
	"SelectorExpr": `package main
type Point struct{ X int }
func main() {
	p := Point{X: 1}
	_ = p.X
}
`,
	"SliceExpr": `package main
func main() {
	s := []int{1, 2, 3}
	_ = s[0:2]
}
`,
	"UnaryExpr": `package main
func main() {
	x := 1
	_ = -x
}
`,
	// Type expressions (via go/types, not AST dispatch).
	"ArrayType": `package main
func main() {
	var a [2]int
	_ = a
}
`,
	"ChanType": `package main
func main() {
	var c chan int
	_ = c
}
`,
	"FuncType": `package main
func main() {
	var f func(int) int
	_ = f
}
`,
	"InterfaceType": `package main
func main() {
	var i interface{ M() }
	_ = i
}
`,
	"MapType": `package main
func main() {
	var m map[string]int
	_ = m
}
`,
	"StructType": `package main
func main() {
	var s struct{ F int }
	_ = s
}
`,
}

// directNegativeProbes are rejected node kinds that cannot be produced by
// parsing a well-formed program (they arise only from parse errors or as
// sub-structure of other nodes), so they are fed to the emitter directly as
// synthetic nodes. Each must fail with an "unsupported" error — never a
// silent drop, never a panic.
var directNegativeProbes = map[string]func() error{
	"BadStmt": func() error {
		_, err := (&Emitter{}).emitStmt(&ast.BadStmt{})
		return err
	},
	"BadExpr": func() error {
		_, err := (&Emitter{}).emitExpr(&ast.BadExpr{})
		return err
	},
	"BadDecl": func() error {
		_, err := (&Emitter{}).emitDeclStmt(&ast.DeclStmt{Decl: &ast.BadDecl{}})
		return err
	},
	"CaseClause": func() error {
		_, err := (&Emitter{}).emitStmt(&ast.CaseClause{})
		return err
	},
	"CommClause": func() error {
		_, err := (&Emitter{}).emitStmt(&ast.CommClause{})
		return err
	},
}

// goASTDecls parses the go/ast package sources of the running toolchain and
// returns the set of declared struct type names and, per type, the set of
// declared method names. Purely syntactic: no hand-written type list.
func goASTDecls(t *testing.T) (structs map[string]bool, methods map[string]map[string]bool) {
	t.Helper()
	dir := filepath.Join(runtime.GOROOT(), "src", "go", "ast")
	fset := token.NewFileSet()
	pkgs, err := goparser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("coverage: cannot parse go/ast sources in %s: %v", dir, err)
	}
	structs = map[string]bool{}
	methods = map[string]map[string]bool{}
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			for _, decl := range f.Decls {
				switch d := decl.(type) {
				case *ast.GenDecl:
					for _, spec := range d.Specs {
						ts, ok := spec.(*ast.TypeSpec)
						if !ok {
							continue
						}
						if _, ok := ts.Type.(*ast.StructType); ok {
							structs[ts.Name.Name] = true
						}
					}
				case *ast.FuncDecl:
					if d.Recv == nil || len(d.Recv.List) == 0 {
						continue
					}
					var base string
					switch rt := d.Recv.List[0].Type.(type) {
					case *ast.Ident:
						base = rt.Name
					case *ast.StarExpr:
						if id, ok := rt.X.(*ast.Ident); ok {
							base = id.Name
						}
					}
					if base == "" {
						continue
					}
					if methods[base] == nil {
						methods[base] = map[string]bool{}
					}
					methods[base][d.Name.Name] = true
				}
			}
		}
	}
	if len(structs) == 0 {
		t.Fatalf("coverage: derived zero struct types from go/ast in %s", dir)
	}
	return structs, methods
}

// goASTNodeNames derives the complete list of concrete go/ast node types:
// every struct type declaring both Pos() and End(). Since ast.Node is
// exactly { Pos() token.Pos; End() token.Pos }, this is precisely the set of
// types implementing ast.Node. A new node type in a future go/ast appears
// here automatically and fails the matrix until classified.
func goASTNodeNames(t *testing.T) []string {
	t.Helper()
	structs, methods := goASTDecls(t)
	var names []string
	for s := range structs {
		if methods[s]["Pos"] && methods[s]["End"] {
			names = append(names, s)
		}
	}
	sort.Strings(names)
	return names
}

// goASTExportedNonNodes derives the exported go/ast struct types that do
// NOT implement ast.Node, for the completeness documentation in
// nonNodeTypes.
func goASTExportedNonNodes(t *testing.T) []string {
	t.Helper()
	structs, methods := goASTDecls(t)
	var names []string
	for s := range structs {
		if !ast.IsExported(s) {
			continue
		}
		if !(methods[s]["Pos"] && methods[s]["End"]) {
			names = append(names, s)
		}
	}
	sort.Strings(names)
	return names
}

// emitProgram runs the full v2 pipeline (parse → check → emit) on one
// in-memory source file and returns the document or the emission error.
// Shared with negative_test.go.
func emitProgram(t *testing.T, src string) (*Document, error) {
	t.Helper()
	dir := t.TempDir()
	fn := filepath.Join(dir, "prog.go")
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

// astKindsIn parses src and returns the set of concrete go/ast node type
// names it contains.
func astKindsIn(t *testing.T, src string) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	f, err := goparser.ParseFile(fset, "probe.go", src, 0)
	if err != nil {
		t.Fatalf("probe source does not parse: %v", err)
	}
	kinds := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			return true
		}
		kinds[reflect.TypeOf(n).Elem().Name()] = true
		return true
	})
	return kinds
}

// probeKindOf maps a negative-case name to the AST kind it exercises.
// Suffixed names (e.g. "IndexExpr_generics") denote sub-cases of the kind
// before the underscore.
func probeKindOf(name string) string {
	if i := strings.Index(name, "_"); i >= 0 {
		return name[:i]
	}
	return name
}

func TestCoverageMatrix(t *testing.T) {
	nodes := goASTNodeNames(t)
	nodeSet := map[string]bool{}
	for _, n := range nodes {
		nodeSet[n] = true
	}

	// Every go/ast node type is classified exactly once.
	var unclassified []string
	for _, n := range nodes {
		_, handled := handledNodes[n]
		_, typeExpr := typeExprNodes[n]
		_, structural := structuralNodes[n]
		_, rejected := loudlyRejected[n]
		count := 0
		for _, b := range []bool{handled, typeExpr, structural, rejected} {
			if b {
				count++
			}
		}
		switch {
		case count == 0:
			unclassified = append(unclassified, n)
		case count > 1:
			t.Errorf("coverage: %s classified %d times (must be exactly once)", n, count)
		}
	}
	if len(unclassified) > 0 {
		t.Errorf("coverage: unclassified AST node types (neither handled, type-expr, structural, nor loudly rejected):\n  %s",
			strings.Join(unclassified, "\n  "))
	}

	// No classification entry is a phantom: every key must be a real
	// go/ast node type.
	for _, m := range []map[string]bool{handledNodes, typeExprNodes, loudlyRejected} {
		for name := range m {
			if !nodeSet[name] {
				t.Errorf("coverage: phantom classification %q (not a go/ast node type)", name)
			}
		}
	}
	for name := range structuralNodes {
		if !nodeSet[name] {
			t.Errorf("coverage: phantom structural classification %q", name)
		}
	}

	// nonNodeTypes documents exactly the derived exported non-node structs.
	derivedNonNodes := goASTExportedNonNodes(t)
	if len(derivedNonNodes) != len(nonNodeTypes) {
		t.Errorf("coverage: nonNodeTypes has %d entries, derived %d (%v)",
			len(nonNodeTypes), len(derivedNonNodes), derivedNonNodes)
	}
	for _, n := range derivedNonNodes {
		if _, ok := nonNodeTypes[n]; !ok {
			t.Errorf("coverage: derived non-node type %q not documented in nonNodeTypes", n)
		}
	}

	// Every handled / via-go-types node has a positive probe, and no probe
	// is claimed for a rejected node.
	for name := range handledNodes {
		if positiveProbes[name] == "" {
			t.Errorf("coverage: handled node %s has no positive probe", name)
		}
		if loudlyRejected[name] {
			t.Errorf("coverage: %s is both handled and rejected", name)
		}
	}
	for name := range typeExprNodes {
		if positiveProbes[name] == "" {
			t.Errorf("coverage: type-expr node %s has no positive probe", name)
		}
	}
	for name := range positiveProbes {
		if !handledNodes[name] && !typeExprNodes[name] {
			t.Errorf("coverage: positive probe for unhandled node %s", name)
		}
	}

	// Every loudly-rejected node has a negative probe: a src-based case in
	// negative_test.go, a direct synthetic-node probe, or a documented
	// shared probe.
	srcProbeKinds := map[string]bool{}
	for _, tc := range negativeCases {
		srcProbeKinds[probeKindOf(tc.name)] = true
	}
	for name := range loudlyRejected {
		if srcProbeKinds[name] || directNegativeProbes[name] != nil || probeAliases[name] != "" {
			continue
		}
		t.Errorf("coverage: rejected node %s has no negative probe", name)
	}
	// Probe aliases must point at a real src-based probe.
	for name, alias := range probeAliases {
		if !srcProbeKinds[alias] {
			t.Errorf("coverage: probe alias %s -> %s has no src-based probe", name, alias)
		}
		if !loudlyRejected[name] {
			t.Errorf("coverage: probe alias for non-rejected node %s", name)
		}
	}

	t.Logf("coverage: %d total AST node types (derived from go/ast)", len(nodes))
	t.Logf("coverage: %d handled", len(handledNodes))
	t.Logf("coverage: %d via go/types", len(typeExprNodes))
	t.Logf("coverage: %d structural", len(structuralNodes))
	t.Logf("coverage: %d non-Node types (documented)", len(nonNodeTypes))
	t.Logf("coverage: %d loudly rejected (not yet implemented)", len(loudlyRejected))
}

// TestCoveragePositiveProbes asserts every handled classification against
// actual emitter behavior: the probe's AST must contain the node kind, and
// the full pipeline must emit it successfully. If the emitter drops or
// starts rejecting a node, its probe fails here.
func TestCoveragePositiveProbes(t *testing.T) {
	for _, name := range sortedKeys(positiveProbes) {
		src := positiveProbes[name]
		t.Run(name, func(t *testing.T) {
			if !astKindsIn(t, src)[name] {
				t.Fatalf("positive probe for %s does not contain a %s node", name, name)
			}
			doc, err := emitProgram(t, src)
			if err != nil {
				t.Fatalf("positive probe for %s: emit failed: %v", name, err)
			}
			if doc == nil || len(doc.Funcs) == 0 {
				t.Fatalf("positive probe for %s: emitted no functions", name)
			}
		})
	}
}

// TestCoverageNegativeProbesAnchor asserts every src-based negative case
// actually exercises the AST kind it claims: the parsed probe AST must
// contain the kind. (Loud failure itself is asserted by
// TestNegativeCasesFailLoudly in negative_test.go.)
func TestCoverageNegativeProbesAnchor(t *testing.T) {
	for _, tc := range negativeCases {
		kind := probeKindOf(tc.name)
		t.Run(tc.name, func(t *testing.T) {
			if !astKindsIn(t, tc.src)[kind] {
				t.Fatalf("negative probe %s does not contain a %s node", tc.name, kind)
			}
		})
	}
}

// TestCoverageNegativeDirectProbes asserts the synthetic-node rejections
// against actual emitter behavior: each must fail with an "unsupported"
// error — never a silent drop, never a panic.
func TestCoverageNegativeDirectProbes(t *testing.T) {
	for _, name := range sortedKeys(directNegativeProbes) {
		probe := directNegativeProbes[name]
		t.Run(name, func(t *testing.T) {
			err := probe()
			if err == nil {
				t.Fatalf("%s: expected loud failure, got success (silent acceptance)", name)
			}
			if !strings.Contains(err.Error(), "unsupported") {
				t.Fatalf("%s: error %q does not contain %q (must fail with 'unsupported')", name, err.Error(), "unsupported")
			}
			if strings.Contains(err.Error(), "panic") || strings.Contains(err.Error(), "nil pointer") {
				t.Fatalf("%s: error %q looks like a crash, not a clean rejection", name, err.Error())
			}
		})
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
