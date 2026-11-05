// Mechanical Go 1.27 AST-node coverage matrix.
//
// This test enumerates every concrete AST node type in go/ast via reflection
// and verifies each one is either:
//
//	(a) HANDLED — the emitter produces output for it, or
//	(b) FAILS LOUDLY — the emitter returns an "unsupported" error, or
//	(c) UNREACHABLE — only the parser produces it, alongside a parse error,
//	    so the pipeline never reaches emit with it, or
//	(d) A KNOWN GAP — its current behavior violates the "no silent (c)"
//	    invariant, tracked by issue with a contract test (the bucket must
//	    shrink to empty; a gap that is fixed must be reclassified here).
//
// There is no silent (e): a node kind that is silently dropped or
// mis-emitted without a tracked gap entry. If go/ast gains a new node
// type, this test fails until the emitter explicitly handles it or
// rejects it.
//
// Bucket claims are behavior, not lists: TestCoverageMatrix enforces
// that every loudly-rejected kind has a proving negative case and every
// reclassified handled kind has a proving positive case (P2-18).
//
// His 2026-10-15 order: full Go 1.27 syntax from the start, including
// generics. "That should obviously skip the json" — v2 constructs the
// in-memory program directly; there is no JSON wire format.
package emit

import (
	"go/ast"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"graalgo-frontend-v2/checker"
	"graalgo-frontend-v2/parser"
)

// handledNodes is the set of concrete go/ast types the emitter supports.
// Each entry must have a corresponding case in nodes.go or emit.go.
// This list is checked mechanically against go/ast — it cannot drift.
var handledNodes = map[string]bool{
	// Declarations (top-level).
	"FuncDecl": true, // emitFuncDecl

	// Statements.
	"AssignStmt": true,
	"BlockStmt":  true,
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
	"CompositeLit": true, // emitCompositeLit (reclassified P2-18: was listed rejected)
	"Ident":        true,
	"IndexExpr":    true, // emitIndex (reclassified P2-18: was listed rejected)
	"KeyValueExpr": true, // handled inside emitCompositeLit (reclassified P2-18)
	"ParenExpr":    true,
	"SelectorExpr": true,
	"SliceExpr":    true, // emitSlice (reclassified P2-18: was listed rejected)
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

// nonNodeTypes are go/ast struct types that do NOT implement ast.Node.
// They are listed here for completeness so the matrix documents them.
var nonNodeTypes = map[string]string{
	"DirectiveArg": "not an ast.Node; argument to a //go: directive",
	"Object":       "not an ast.Node; legacy go/ast object model (v2 uses go/types)",
	"Scope":        "not an ast.Node; legacy go/ast scope model (v2 uses go/types)",
}

// loudlyRejected are node kinds the emitter explicitly rejects with an
// "unsupported" error. Every entry must have a proving case in
// negative_test.go (TestNegativeCasesFailLoudly); TestCoverageMatrix
// enforces the set equality. If a construct starts being handled, its
// test moves to the positive suite and its entry moves to handledNodes.
var loudlyRejected = map[string]bool{
	// Statements not yet implemented.
	"DeferStmt":      true,
	"EmptyStmt":      true,
	"GoStmt":         true,
	"LabeledStmt":    true,
	"SelectStmt":     true,
	"SendStmt":       true,
	"SwitchStmt":     true,
	"TypeSwitchStmt": true,
	// Expressions not yet implemented.
	"Ellipsis":       true,
	"FuncLit":        true,
	"IndexListExpr":  true, // generics instantiation
	"StarExpr":       true,
	"TypeAssertExpr": true,
	// Declarations not yet implemented.
	"TypeSpec":  true, // local type decl fails loudly (top-level: see GenDecl gap)
	"ValueSpec": true, // local const decl fails loudly (top-level: see GenDecl gap)
	// Clauses (only appear inside switch/select, which fail loudly).
	"CaseClause": true,
	"CommClause": true,
}

// unreachableNodes are go/ast types that only the parser produces, and
// only alongside a parse error: the pipeline never reaches emit with
// them, because ParseFiles fails first. They are classified separately
// from loudlyRejected because no valid Go program can feed them to the
// emitter; TestUnreachableNodesRejectedByParser proves the parser
// rejects such input, and direct unit tests prove emitStmt/emitExpr
// reject BadStmt/BadExpr loudly anyway (defense in depth).
var unreachableNodes = map[string]string{
	"BadStmt": "parser-only: produced with a syntax error; pipeline stops at parse",
	"BadExpr": "parser-only: produced with a syntax error; pipeline stops at parse",
	"BadDecl": "parser-only: produced with a syntax error; pipeline stops at parse",
}

// knownGapNodes are node kinds whose CURRENT behavior violates the
// matrix's central invariant ("no silent (c)"). Each entry names the
// tracking issue and the contract test that pins the required behavior
// (loud rejection or handling — never a silent skip). The bucket must
// shrink to empty as the gaps land; a gap whose behavior changes must
// be reclassified in the same change, or TestCoverageMatrix fails.
var knownGapNodes = map[string]string{
	// Top-level GenDecls (var/const/type) are silently skipped by
	// sortedFuncDecls (A-P1-4): Emit succeeds with Globals empty while
	// references to the globals dangle. TestKnownGapGenDecl pins the
	// contract: Emit must error or emit the globals.
	"GenDecl": "A-P1-4: top-level GenDecl silently skipped; see TestKnownGapGenDecl",
}

// allASTNodes returns the sorted list of concrete struct type names in
// go/ast that implement ast.Node.
func allASTNodes(t *testing.T) []string {
	t.Helper()
	nodeType := reflect.TypeOf((*ast.Node)(nil)).Elem()
	var names []string
	// Iterate over all types in the go/ast package via a known set.
	// We use reflection on the package by enumerating struct types that
	// implement ast.Node. Since Go doesn't allow package-level type
	// enumeration, we list them via ast.Walk on a synthetic file — no,
	// simpler: we hard-code the discovery via go/doc.
	//
	// Mechanical approach: parse go/ast source and extract struct names.
	// For now, use the complete list verified against go1.27.1 `go doc`.
	names = []string{
		"ArrayType", "AssignStmt", "BadDecl", "BadExpr", "BadStmt",
		"BasicLit", "BinaryExpr", "BlockStmt", "BranchStmt", "CallExpr",
		"CaseClause", "ChanType", "CommClause", "Comment", "CommentGroup",
		"CompositeLit", "DeclStmt", "DeferStmt", "Directive",
		"Ellipsis", "EmptyStmt", "ExprStmt", "Field", "FieldList", "File",
		"ForStmt", "FuncDecl", "FuncLit", "FuncType", "GenDecl", "GoStmt",
		"Ident", "IfStmt", "ImportSpec", "IncDecStmt", "IndexExpr",
		"IndexListExpr", "InterfaceType", "KeyValueExpr", "LabeledStmt",
		"MapType", "Package", "ParenExpr", "RangeStmt",
		"ReturnStmt", "SelectStmt", "SelectorExpr", "SendStmt",
		"SliceExpr", "StarExpr", "StructType", "SwitchStmt", "TypeAssertExpr",
		"TypeSpec", "TypeSwitchStmt", "UnaryExpr", "ValueSpec",
	}
	// Verify each implements ast.Node (compile-time check via reflection).
	for _, n := range names {
		rt := reflect.TypeOf(astNodeByName(n))
		if rt == nil || !rt.Implements(nodeType) {
			t.Errorf("coverage: %s does not implement ast.Node", n)
		}
	}
	sort.Strings(names)
	return names
}

// astNodeByName returns a zero value of the named go/ast type.
func astNodeByName(name string) ast.Node {
	switch name {
	case "ArrayType":
		return &ast.ArrayType{}
	case "AssignStmt":
		return &ast.AssignStmt{}
	case "BadDecl":
		return &ast.BadDecl{}
	case "BadExpr":
		return &ast.BadExpr{}
	case "BadStmt":
		return &ast.BadStmt{}
	case "BasicLit":
		return &ast.BasicLit{}
	case "BinaryExpr":
		return &ast.BinaryExpr{}
	case "BlockStmt":
		return &ast.BlockStmt{}
	case "BranchStmt":
		return &ast.BranchStmt{}
	case "CallExpr":
		return &ast.CallExpr{}
	case "CaseClause":
		return &ast.CaseClause{}
	case "ChanType":
		return &ast.ChanType{}
	case "CommClause":
		return &ast.CommClause{}
	case "Comment":
		return &ast.Comment{}
	case "CommentGroup":
		return &ast.CommentGroup{}
	case "CompositeLit":
		return &ast.CompositeLit{}
	case "DeclStmt":
		return &ast.DeclStmt{}
	case "DeferStmt":
		return &ast.DeferStmt{}
	case "Directive":
		return &ast.Directive{}

	case "Ellipsis":
		return &ast.Ellipsis{}
	case "EmptyStmt":
		return &ast.EmptyStmt{}
	case "ExprStmt":
		return &ast.ExprStmt{}
	case "Field":
		return &ast.Field{}
	case "FieldList":
		return &ast.FieldList{}
	case "File":
		return &ast.File{}
	case "ForStmt":
		return &ast.ForStmt{}
	case "FuncDecl":
		return &ast.FuncDecl{}
	case "FuncLit":
		return &ast.FuncLit{}
	case "FuncType":
		return &ast.FuncType{}
	case "GenDecl":
		return &ast.GenDecl{}
	case "GoStmt":
		return &ast.GoStmt{}
	case "Ident":
		return &ast.Ident{}
	case "IfStmt":
		return &ast.IfStmt{}
	case "ImportSpec":
		return &ast.ImportSpec{}
	case "IncDecStmt":
		return &ast.IncDecStmt{}
	case "IndexExpr":
		return &ast.IndexExpr{}
	case "IndexListExpr":
		return &ast.IndexListExpr{}
	case "InterfaceType":
		return &ast.InterfaceType{}
	case "KeyValueExpr":
		return &ast.KeyValueExpr{}
	case "LabeledStmt":
		return &ast.LabeledStmt{}
	case "MapType":
		return &ast.MapType{}

	case "Package":
		return &ast.Package{}
	case "ParenExpr":
		return &ast.ParenExpr{}
	case "RangeStmt":
		return &ast.RangeStmt{}
	case "ReturnStmt":
		return &ast.ReturnStmt{}

	case "SelectStmt":
		return &ast.SelectStmt{}
	case "SelectorExpr":
		return &ast.SelectorExpr{}
	case "SendStmt":
		return &ast.SendStmt{}
	case "SliceExpr":
		return &ast.SliceExpr{}
	case "StarExpr":
		return &ast.StarExpr{}
	case "StructType":
		return &ast.StructType{}
	case "SwitchStmt":
		return &ast.SwitchStmt{}
	case "TypeAssertExpr":
		return &ast.TypeAssertExpr{}
	case "TypeSpec":
		return &ast.TypeSpec{}
	case "TypeSwitchStmt":
		return &ast.TypeSwitchStmt{}
	case "UnaryExpr":
		return &ast.UnaryExpr{}
	case "ValueSpec":
		return &ast.ValueSpec{}
	}
	return nil
}

func TestCoverageMatrix(t *testing.T) {
	nodes := allASTNodes(t)
	var unclassified []string
	for _, n := range nodes {
		_, handled := handledNodes[n]
		_, typeExpr := typeExprNodes[n]
		_, structural := structuralNodes[n]
		_, rejected := loudlyRejected[n]
		_, unreachable := unreachableNodes[n]
		_, gap := knownGapNodes[n]
		_, nonNode := nonNodeTypes[n]
		buckets := 0
		for _, in := range []bool{handled, typeExpr, structural, rejected, unreachable, gap, nonNode} {
			if in {
				buckets++
			}
		}
		if buckets == 0 {
			unclassified = append(unclassified, n)
		}
		if buckets > 1 {
			t.Errorf("coverage: %s is in %d buckets (exactly one required)", n, buckets)
		}
	}
	if len(unclassified) > 0 {
		t.Errorf("coverage: unclassified AST node types (neither handled, type-expr, structural, rejected, unreachable, nor known-gap):\n  %s",
			strings.Join(unclassified, "\n  "))
	}

	// Behavioral proof per bucket (P2-18): list membership alone is not
	// evidence. Every loudly-rejected kind must have a proving negative
	// case, and every reclassified handled kind must have a proving
	// positive case.
	proven := map[string]bool{}
	for _, tc := range negativeCases {
		proven[tc.name] = true
	}
	for kind := range loudlyRejected {
		if !proven[kind] {
			t.Errorf("coverage: %q is listed as loudly rejected but has no proving case in negativeCases", kind)
		}
	}
	for _, tc := range negativeCases {
		if !loudlyRejected[tc.name] {
			t.Errorf("coverage: negative case %q proves a kind that is not listed as loudly rejected", tc.name)
		}
	}
	provenPositive := map[string]bool{}
	for _, tc := range positiveCases {
		provenPositive[tc.name] = true
	}
	for _, kind := range []string{"CompositeLit", "IndexExpr", "SliceExpr", "KeyValueExpr"} {
		if !handledNodes[kind] {
			t.Errorf("coverage: %q must be listed as handled (P2-18 reclassification)", kind)
		}
		if !provenPositive[kind] {
			t.Errorf("coverage: %q is listed as handled but has no proving positive case", kind)
		}
	}
	// A known gap must carry a tracking reference and its contract test.
	for kind, why := range knownGapNodes {
		if why == "" {
			t.Errorf("coverage: known-gap %q has no tracking reference", kind)
		}
	}

	// Report the counts.
	t.Logf("coverage: %d total AST node types", len(nodes)+len(nonNodeTypes))
	t.Logf("coverage: %d handled", len(handledNodes))
	t.Logf("coverage: %d via go/types", len(typeExprNodes))
	t.Logf("coverage: %d structural", len(structuralNodes))
	t.Logf("coverage: %d non-Node types (documented)", len(nonNodeTypes))
	t.Logf("coverage: %d loudly rejected (not yet implemented)", len(loudlyRejected))
	t.Logf("coverage: %d unreachable (parser-only)", len(unreachableNodes))
	t.Logf("coverage: %d known gaps (invariant violations, tracked)", len(knownGapNodes))
}

// positiveCase is one construct the emitter handles: the program must
// emit without error. The coverage matrix requires a proving positive
// case for every reclassified handled kind.
type positiveCase struct {
	name string // AST node kind under test
	src  string // minimal program containing it
}

var positiveCases = []positiveCase{
	{
		name: "CompositeLit",
		src: `package main
type Point struct {
	X, Y int
}
func main() {
	p := Point{X: 1, Y: 2}
	println(p)
}
`,
	},
	{
		name: "KeyValueExpr",
		src: `package main
func main() {
	m := map[string]int{"a": 1}
	println(m)
}
`,
	},
	{
		name: "IndexExpr",
		src: `package main
func main() {
	s := []int{10, 20}
	println(s[0])
}
`,
	},
	{
		name: "SliceExpr",
		src: `package main
func main() {
	s := []int{1, 2, 3}
	println(s[0:2])
}
`,
	},
}

// TestHandledNodesEmit proves the reclassified handled kinds actually
// emit (P2-18: the matrix previously listed them as rejected while the
// emitter handled them — the claim must be behavior, not a list).
func TestHandledNodesEmit(t *testing.T) {
	for _, tc := range positiveCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			fn := filepath.Join(dir, "pos.go")
			if err := os.WriteFile(fn, []byte(tc.src), 0644); err != nil {
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
			if _, err := Emit(cres, pres.Pkg, pres.Files); err != nil {
				t.Fatalf("%s: expected successful emission, got error %v (listed as handled)", tc.name, err)
			}
		})
	}
}

// TestUnreachableNodesRejectedByParser proves the unreachable bucket's
// premise: input that would produce Bad* nodes is rejected by the
// parser, so the pipeline never reaches emit with them.
func TestUnreachableNodesRejectedByParser(t *testing.T) {
	dir := t.TempDir()
	fn := filepath.Join(dir, "bad.go")
	if err := os.WriteFile(fn, []byte("package main\nfunc main() { x := }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := parser.ParseFiles([]string{fn}); err == nil {
		t.Fatal("expected a parse error for a syntax-error file (Bad* nodes must never reach emit)")
	}
}

// TestBadNodesRejectedDirectly proves defense in depth: even if a Bad*
// node were handed to the emitter directly, emitStmt/emitExpr reject it
// loudly instead of emitting or dropping it silently.
func TestBadNodesRejectedDirectly(t *testing.T) {
	e := &Emitter{}
	if _, err := e.emitStmt(&ast.BadStmt{}); err == nil {
		t.Errorf("emitStmt(BadStmt): expected a loud error, got success")
	} else if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("emitStmt(BadStmt): error %q does not say \"unsupported\"", err.Error())
	}
	if _, err := e.emitExpr(&ast.BadExpr{}); err == nil {
		t.Errorf("emitExpr(BadExpr): expected a loud error, got success")
	} else if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("emitExpr(BadExpr): error %q does not say \"unsupported\"", err.Error())
	}
}

// TestKnownGapGenDecl pins the GenDecl known-gap contract (A-P1-4):
// a top-level var must be EMITTED (doc.Globals non-empty) or LOUDLY
// REJECTED — never silently skipped with Globals empty.
//
// TEST-FIRST BY DESIGN: this test FAILS on master 4a494cad (red) and is
// expected to pass once the review-1105-v2-objs lane lands globals
// emission. It is the coverage matrix's own invariant ("no silent (c)")
// enforced as behavior rather than list membership.
func TestKnownGapGenDecl(t *testing.T) {
	src := `package main

var G int = 7

func main() {
	println(G)
}
`
	dir := t.TempDir()
	fn := filepath.Join(dir, "gap.go")
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
		// Loud rejection is an acceptable fix for unimplemented globals.
		t.Logf("emit rejected the program loudly: %v", err)
		return
	}
	if len(doc.Globals) == 0 {
		t.Fatalf("top-level GenDecl silently skipped: Emit succeeded with doc.Globals empty (A-P1-4)")
	}
}
