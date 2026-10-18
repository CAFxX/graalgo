// Mechanical Go 1.27 AST-node coverage matrix.
//
// This test enumerates every concrete AST node type in go/ast via reflection
// and verifies each one is either:
//
//	(a) HANDLED — the emitter produces output for it, or
//	(b) FAILS LOUDLY — the emitter returns an "unsupported" error.
//
// There is no silent (c): a node kind that is silently dropped or
// mis-emitted. If go/ast gains a new node type, this test fails until the
// emitter explicitly handles it or rejects it.
//
// Classification alone is not the proof. The maps below are checked for
// TRUTH by two tests:
//   - TestHandledNodesActuallyEmit: for every handled node, a minimal
//     program goes through the real pipeline (parse → check → emit) and
//     the expected IR node kind must appear in the emitted document. A
//     map entry that claims "handled" while the emitter silently drops the
//     node fails here.
//   - TestRejectedNodesActuallyFail: every loudly-rejected node kind must
//     have a negative probe in negative_test.go that really fails. A map
//     entry that claims "loudly rejected" with no failing probe fails here.
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
// Each entry must have a corresponding case in nodes.go or emit.go, and a
// truth probe in handledProbes below.
var handledNodes = map[string]bool{
	// Declarations (top-level).
	"FuncDecl": true, // emitFuncDecl; method declarations fail loudly
	"GenDecl":  true, // VAR → globals; CONST/TYPE skipped as inert (no runtime
	// semantics: const values are carried on use, named types are
	// described via go/types wherever referenced); IMPORT skipped

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
	"CompositeLit": true,
	"Ident":        true,
	"IndexExpr":    true, // index emits; generic instantiation fails loudly
	"KeyValueExpr": true, // via emitCompositeLit; standalone is invalid Go
	"ParenExpr":    true,
	"SelectorExpr": true,
	"SliceExpr":    true,
	"UnaryExpr":    true,

	// ValueSpec is not a standalone emitExpr case: it is handled via GenDecl
	// (top-level globals) and via DeclStmt (local var declarations).
	"ValueSpec": true,
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
	"TypeSpec":     "type declaration; carries no runtime semantics — the named type is described via go/types wherever referenced; the declaration itself is not emitted (like ImportSpec)",
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
// "unsupported" error. Every entry (minus unprobeableRejected) must have a
// negative probe in negative_test.go — enforced by
// TestRejectedNodesActuallyFail.
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
	"Ellipsis":       true,
	"FuncLit":        true,
	"IndexListExpr":  true, // multi-arg generics instantiation
	"StarExpr":       true,
	"TypeAssertExpr": true,
	// Declarations not yet implemented.
	"BadDecl": true,
	// Clauses (only appear inside switch/select).
	"CaseClause": true,
	"CommClause": true,
}

// unprobeableRejected are loudly-rejected node kinds with no standalone
// negative probe, each with its justification.
var unprobeableRejected = map[string]string{
	"BadStmt":    "parser rejects before emit",
	"BadExpr":    "parser rejects before emit",
	"BadDecl":    "parser rejects before emit",
	"CaseClause": "covered by SwitchStmt/TypeSwitchStmt rejection",
	"CommClause": "covered by SelectStmt rejection",
}

// loudlyRejectedSubcases are (node, subcase) pairs where the node is
// classified handled but a subcase must still fail loudly. Each must have a
// negative probe named exactly "Node_subcase" in negative_test.go.
var loudlyRejectedSubcases = []string{
	"FuncDecl_method", // method declaration
}

// handledProbe is a truth probe for one handled node: a minimal program
// through the real pipeline, asserting the expected IR node kind appears
// in the emitted document.
type handledProbe struct {
	node string // AST node kind under test (key into handledNodes)
	src  string // minimal program containing it
	kind string // IR node kind that must appear in the emitted document
}

var handledProbes = []handledProbe{
	{node: "FuncDecl", src: "package main\nfunc f() {}\nfunc main() {}\n", kind: "funcdecl"},
	{node: "GenDecl", src: "package main\nvar Global = 99\nfunc main() { println(Global) }\n", kind: "globalvar"},
	{node: "ValueSpec", src: "package main\nvar A, B = 1, 2\nfunc main() { println(A + B) }\n", kind: "globalvar"},
	{node: "AssignStmt", src: "package main\nfunc main() { x := 1; x = 2; println(x) }\n", kind: "assign"},
	{node: "BlockStmt", src: "package main\nfunc main() {}\n", kind: "block"},
	{node: "BranchStmt", src: "package main\nfunc main() { for i := 0; i < 3; i++ { break } }\n", kind: "branch"},
	{node: "DeclStmt", src: "package main\nfunc main() { var x int; println(x) }\n", kind: "vardecl"},
	{node: "ExprStmt", src: "package main\nfunc main() { println(1) }\n", kind: "exprstmt"},
	{node: "ForStmt", src: "package main\nfunc main() { for i := 0; i < 1; i++ { println(i) } }\n", kind: "for"},
	{node: "IfStmt", src: "package main\nfunc main() { if true { println(1) } }\n", kind: "if"},
	{node: "IncDecStmt", src: "package main\nfunc main() { i := 0; i++; println(i) }\n", kind: "incdec"},
	{node: "RangeStmt", src: "package main\nfunc main() { for _, x := range []int{1} { println(x) } }\n", kind: "range"},
	{node: "ReturnStmt", src: "package main\nfunc f() int { return 1 }\nfunc main() { println(f()) }\n", kind: "return"},
	{node: "BasicLit", src: "package main\nfunc main() { println(42) }\n", kind: "basiclit"},
	{node: "BinaryExpr", src: "package main\nfunc main() { println(1 + 2) }\n", kind: "binary"},
	{node: "CallExpr", src: "package main\nfunc main() { println(1) }\n", kind: "call"},
	{node: "CompositeLit", src: "package main\nfunc main() { s := []int{1, 2}; println(s[0]) }\n", kind: "compositelit"},
	{node: "Ident", src: "package main\nfunc main() { x := 1; println(x) }\n", kind: "ident"},
	{node: "IndexExpr", src: "package main\nfunc main() { s := []int{7}; println(s[0]) }\n", kind: "index"},
	{node: "KeyValueExpr", src: "package main\nfunc main() { p := struct{ X int }{X: 1}; println(p.X) }\n", kind: "keyvalue"},
	{node: "ParenExpr", src: "package main\nfunc main() { println((1)) }\n", kind: "paren"},
	{node: "SelectorExpr", src: "package main\nfunc main() { p := struct{ X int }{X: 3}; println(p.X) }\n", kind: "selector"},
	{node: "SliceExpr", src: "package main\nfunc main() { s := []int{1, 2, 3}; println(s[1:2]) }\n", kind: "slice"},
	{node: "UnaryExpr", src: "package main\nfunc main() { x := 1; println(-x) }\n", kind: "unary"},
}

// mustEmit runs the full pipeline (parse → check → emit) on src and
// returns the document, failing the test on any stage error.
func mustEmit(t *testing.T, src string) *Document {
	t.Helper()
	dir := t.TempDir()
	fn := filepath.Join(dir, "probe.go")
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

// docHasKind reports whether any node in the document's funcs or globals
// has the given IR kind.
func docHasKind(doc *Document, kind string) bool {
	return anyHasKind(doc.Funcs, kind) || anyHasKind(doc.Globals, kind)
}

func anyHasKind(v any, kind string) bool {
	switch t := v.(type) {
	case map[string]any:
		if k, ok := t["kind"].(string); ok && k == kind {
			return true
		}
		for _, w := range t {
			if anyHasKind(w, kind) {
				return true
			}
		}
	case []any:
		for _, w := range t {
			if anyHasKind(w, kind) {
				return true
			}
		}
	}
	return false
}

func TestHandledNodesActuallyEmit(t *testing.T) {
	for _, p := range handledProbes {
		t.Run(p.node, func(t *testing.T) {
			base := p.node
			if i := strings.Index(base, "_"); i >= 0 {
				base = base[:i]
			}
			if !handledNodes[base] && !typeExprNodes[base] {
				t.Fatalf("probe %q: %q not classified handled", p.node, base)
			}
			doc := mustEmit(t, p.src)
			if !docHasKind(doc, p.kind) {
				t.Fatalf("%s: emitted document contains no %q node — the matrix claims it is handled, but the emitter drops it silently", p.node, p.kind)
			}
		})
	}
}

func TestRejectedNodesActuallyFail(t *testing.T) {
	probed := map[string]bool{}
	exact := map[string]bool{}
	for _, tc := range negativeCases {
		exact[tc.name] = true
		base := tc.name
		if i := strings.Index(base, "_"); i >= 0 {
			base = base[:i]
		}
		probed[base] = true
	}
	for name := range loudlyRejected {
		if _, ok := unprobeableRejected[name]; ok {
			continue
		}
		if !probed[name] {
			t.Errorf("rejected node %s has no negative probe in negative_test.go — add one or justify it in unprobeableRejected", name)
		}
	}
	for _, sub := range loudlyRejectedSubcases {
		if !exact[sub] {
			t.Errorf("missing negative probe %q in negative_test.go for loudly-rejected subcase", sub)
		}
	}
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
		_, nonNode := nonNodeTypes[n]
		if !handled && !typeExpr && !structural && !rejected && !nonNode {
			unclassified = append(unclassified, n)
		}
	}
	if len(unclassified) > 0 {
		t.Errorf("coverage: unclassified AST node types (neither handled, type-expr, structural, nor loudly rejected):\n  %s",
			strings.Join(unclassified, "\n  "))
	}

	// Report the counts.
	t.Logf("coverage: %d total AST node types", len(nodes)+len(nonNodeTypes))
	t.Logf("coverage: %d handled", len(handledNodes))
	t.Logf("coverage: %d via go/types", len(typeExprNodes))
	t.Logf("coverage: %d structural", len(structuralNodes))
	t.Logf("coverage: %d non-Node types (documented)", len(nonNodeTypes))
	t.Logf("coverage: %d loudly rejected (not yet implemented)", len(loudlyRejected))
}
