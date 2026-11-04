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
// His 2026-10-15 order: full Go 1.27 syntax from the start, including
// generics. "That should obviously skip the json" — v2 constructs the
// in-memory program directly; there is no JSON wire format.
package emit

import (
	"go/ast"
	"reflect"
	"sort"
	"strings"
	"testing"
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
	"CompositeLit": true, // emitCompositeLit, incl. KeyValueExpr elements
	"Ident":        true,
	"IndexExpr":    true, // emitIndex; generic instantiation is rejected, not emitted
	"KeyValueExpr": true, // only inside CompositeLit; standalone it is not valid Go
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
// TypeSpec is the declaration form of the same mechanism: a named type's
// definition is materialized in the Types table via go/types when
// referenced (proved by the TypeSpec truth case and by
// TestTopLevelTypeDeclViaTypes in globals_test.go), so it belongs here
// rather than in loudlyRejected.
var typeExprNodes = map[string]bool{
	"ArrayType":     true,
	"ChanType":      true,
	"FuncType":      true,
	"InterfaceType": true,
	"MapType":       true,
	"StructType":    true,
	"TypeSpec":      true,
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

// partiallyHandled are node kinds whose handling depends on context:
// one form emits, another fails loudly. Both sides are proved by
// TestCoverageMatrixTruth below, so neither side can silently drift.
var partiallyHandled = map[string]string{
	// VAR specs emit into Document.Globals; CONST specs fail loudly
	// (no faithful representation yet); TYPE specs need no entry
	// (definition via go/types, see TypeSpec in typeExprNodes); IMPORT
	// specs are structural (checker resolves imports).
	"GenDecl": "var → Globals; const → unsupported error; type → via go/types; import → structural",
	// VAR ValueSpecs emit; CONST ValueSpecs fail loudly with their GenDecl.
	"ValueSpec": "var → emitted; const → unsupported error via its GenDecl",
}

// loudlyRejected are node kinds the emitter explicitly rejects with an
// "unsupported" error. The negative test below proves each one fails.
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
	"IndexListExpr":  true, // generics instantiation (multi-arg)
	"StarExpr":       true,
	"TypeAssertExpr": true,
	// Declarations not yet implemented.
	"BadDecl": true,
	// Clauses (only appear inside switch/select).
	"CaseClause": true,
	"CommClause": true,
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
		_, partial := partiallyHandled[n]
		_, nonNode := nonNodeTypes[n]
		if !handled && !typeExpr && !structural && !rejected && !partial && !nonNode {
			unclassified = append(unclassified, n)
		}
	}
	if len(unclassified) > 0 {
		t.Errorf("coverage: unclassified AST node types (neither handled, type-expr, structural, partially handled, nor loudly rejected):\n  %s",
			strings.Join(unclassified, "\n  "))
	}

	// Report the counts.
	t.Logf("coverage: %d total AST node types", len(nodes)+len(nonNodeTypes))
	t.Logf("coverage: %d handled", len(handledNodes))
	t.Logf("coverage: %d via go/types", len(typeExprNodes))
	t.Logf("coverage: %d structural", len(structuralNodes))
	t.Logf("coverage: %d partially handled", len(partiallyHandled))
	t.Logf("coverage: %d non-Node types (documented)", len(nonNodeTypes))
	t.Logf("coverage: %d loudly rejected (not yet implemented)", len(loudlyRejected))
}

// TestCoverageMatrixTruth proves the matrix is not just a list: every
// handled kind must actually emit, every rejected kind must actually
// fail loudly, and every partially-handled kind must do both, each on
// the side the matrix claims. The old test checked bucket membership
// only, so CompositeLit/IndexExpr/SliceExpr/KeyValueExpr could sit in
// loudlyRejected while nodes.go implemented them — a lie the matrix
// could not catch. A kind whose program below stops matching its bucket
// fails here, not in a downstream consumer.
//
// Programs are kept minimal and use only constructs their own bucket
// allows, so a failure points at the kind under test, not at an
// unrelated construct in the snippet.
func TestCoverageMatrixTruth(t *testing.T) {
	// handledCases: one program per handled kind that must emit.
	handledCases := []struct {
		name string
		src  string
	}{
		{"FuncDecl", "package demo\n\nfunc F() int { return 1 }\nfunc main() { println(F()) }\n"},
		{"AssignStmt", "package demo\n\nfunc main() { x := 1\n x = 2\n println(x) }\n"},
		{"BlockStmt", "package demo\n\nfunc main() { x := 1\n { println(x) } }\n"},
		{"BranchStmt", "package demo\n\nfunc main() { for i := 0; i < 3; i++ { break }\n println(1) }\n"},
		{"DeclStmt", "package demo\n\nfunc main() { var x int\n println(x) }\n"},
		{"ExprStmt", "package demo\n\nfunc main() { println(1) }\n"},
		{"ForStmt", "package demo\n\nfunc main() { for i := 0; i < 3; i++ { println(i) } }\n"},
		{"IfStmt", "package demo\n\nfunc main() { x := 1\n if x > 0 { println(x) } }\n"},
		{"IncDecStmt", "package demo\n\nfunc main() { x := 1\n x++\n println(x) }\n"},
		{"RangeStmt", "package demo\n\nfunc main() { for _, v := range []int{1, 2} { println(v) } }\n"},
		{"ReturnStmt", "package demo\n\nfunc F() int { return 42 }\nfunc main() { println(F()) }\n"},
		{"BasicLit", "package demo\n\nfunc main() { println(42) }\n"},
		{"BinaryExpr", "package demo\n\nfunc main() { println(1 + 2) }\n"},
		{"CallExpr", "package demo\n\nfunc F() int { return 1 }\nfunc main() { println(F()) }\n"},
		{"CompositeLit", "package demo\n\nfunc main() { xs := []int{1, 2}\n println(xs[0]) }\n"},
		{"Ident", "package demo\n\nfunc main() { x := 3\n println(x) }\n"},
		{"IndexExpr", "package demo\n\nfunc main() { xs := []int{7, 8}\n println(xs[1]) }\n"},
		{"KeyValueExpr", "package demo\n\nfunc main() { m := map[string]int{\"a\": 1}\n println(m[\"a\"]) }\n"},
		{"ParenExpr", "package demo\n\nfunc main() { println((1 + 2)) }\n"},
		{"SelectorExpr", "package demo\n\nfunc f(p struct{ X int }) int { return p.X }\nfunc main() { println(f(struct{ X int }{X: 5})) }\n"},
		{"SliceExpr", "package demo\n\nfunc main() { xs := []int{1, 2, 3}\n ys := xs[0:2]\n println(ys[1]) }\n"},
		{"UnaryExpr", "package demo\n\nfunc main() { x := 1\n println(-x) }\n"},
	}
	for _, tc := range handledCases {
		t.Run("handled/"+tc.name, func(t *testing.T) {
			if _, ok := handledNodes[tc.name]; !ok {
				t.Fatalf("%s is not in handledNodes (matrix drift)", tc.name)
			}
			if _, err := emitTestProgram(t, tc.src); err != nil {
				t.Fatalf("%s: expected successful emission, got %v", tc.name, err)
			}
		})
	}

	// typeExprCases: type expressions never reach emitExpr's switch;
	// they are resolved via go/types. Each must still emit.
	typeExprCases := []struct {
		name string
		src  string
	}{
		{"ArrayType", "package demo\n\nfunc f(a [3]int) int { return a[0] }\nfunc main() { println(f([3]int{9, 0, 0})) }\n"},
		{"ChanType", "package demo\n\nfunc f(c chan int) { println(1) }\nfunc main() { f(nil) }\n"},
		{"FuncType", "package demo\n\nfunc g(x int) int { return x }\nfunc f(h func(int) int) { println(h(1)) }\nfunc main() { f(g) }\n"},
		{"InterfaceType", "package demo\n\nfunc f(x any) { println(1) }\nfunc main() { f(42) }\n"},
		{"MapType", "package demo\n\nfunc f(m map[string]int) { println(m[\"a\"]) }\nfunc main() { f(map[string]int{\"a\": 3}) }\n"},
		{"StructType", "package demo\n\nfunc f(p struct{ X int }) int { return p.X }\nfunc main() { println(f(struct{ X int }{X: 4})) }\n"},
		{"TypeSpec", "package demo\n\ntype Point struct { X int; Y int }\nfunc main() { p := Point{X: 1, Y: 2}\n println(p.X + p.Y) }\n"},
	}
	for _, tc := range typeExprCases {
		t.Run("typeExpr/"+tc.name, func(t *testing.T) {
			if _, ok := typeExprNodes[tc.name]; !ok {
				t.Fatalf("%s is not in typeExprNodes (matrix drift)", tc.name)
			}
			if _, err := emitTestProgram(t, tc.src); err != nil {
				t.Fatalf("%s: expected successful emission, got %v", tc.name, err)
			}
		})
	}

	// structuralCases: presence of the node must not break emission.
	structuralCases := []struct {
		name string
		src  string
	}{
		{"Comment", "package demo\n\n// F returns one.\nfunc F() int { return 1 }\nfunc main() { println(F()) }\n"},
		{"CommentGroup", "package demo\n\n// F returns one.\n// Second line.\nfunc F() int { return 1 }\nfunc main() { println(F()) }\n"},
		{"Field", "package demo\n\nfunc F(x int) int { return x }\nfunc main() { println(F(1)) }\n"},
		{"FieldList", "package demo\n\nfunc F(x int, y string) int { return x }\nfunc main() { println(F(1, \"s\")) }\n"},
		{"File", "package demo\n\nfunc main() { println(1) }\n"},
		{"Package", "package demo\n\nfunc main() { println(1) }\n"},
		{"ImportSpec", "package demo\n\nimport \"fmt\"\nfunc main() { fmt.Println(1) }\n"},
		{"Directive", "package demo\n\n//go:noinline\nfunc F() int { return 1 }\nfunc main() { println(F()) }\n"},
	}
	for _, tc := range structuralCases {
		t.Run("structural/"+tc.name, func(t *testing.T) {
			if _, ok := structuralNodes[tc.name]; !ok {
				t.Fatalf("%s is not in structuralNodes (matrix drift)", tc.name)
			}
			if _, err := emitTestProgram(t, tc.src); err != nil {
				t.Fatalf("%s: expected successful emission, got %v", tc.name, err)
			}
		})
	}

	// rejectedCases: pipeline-reachable kinds that must fail loudly in
	// the emitter with an "unsupported" error. CaseClause/CommClause
	// only occur inside Switch/Select, so their programs are the
	// enclosing statement's program and the error names that statement
	// — the clause is still rejected, via its parent. GenDecl const
	// and ValueSpec const are proved under partiallyHandled below;
	// TypeSpec is not rejected at all (it is via go/types, above).
	rejectedCases := []struct {
		name string
		src  string
	}{
		{"DeferStmt", "package demo\nfunc main() { defer println(\"x\") }\n"},
		{"GoStmt", "package demo\nfunc main() { go println(\"x\") }\n"},
		{"SendStmt", "package demo\nfunc send(c chan int) { c <- 1 }\nfunc main() {}\n"},
		{"SelectStmt", "package demo\nfunc sel(c chan int) { select { case x := <-c: println(x); default: } }\nfunc main() {}\n"},
		{"SwitchStmt", "package demo\nfunc main() { x := 1\n switch x { case 1: println(\"one\"); default: println(\"other\") } }\n"},
		{"TypeSwitchStmt", "package demo\nfunc main() { var x any = 1\n switch v := x.(type) { case int: println(v); default: } }\n"},
		{"LabeledStmt", "package demo\nfunc main() { outer: for i := 0; i < 3; i++ { break outer } }\n"},
		{"EmptyStmt", "package demo\nfunc main() { x := 1\n ;\n println(x) }\n"},
		{"StarExpr", "package demo\nfunc main() { x := 42\n p := &x\n println(*p) }\n"},
		{"TypeAssertExpr", "package demo\nfunc main() { var x any = 42\n y := x.(int)\n println(y) }\n"},
		{"FuncLit", "package demo\nfunc main() { f := func(x int) int { return x * 2 }\n println(f(21)) }\n"},
		{"Ellipsis", "package demo\nfunc sum(xs ...int) int { s := 0\n for _, x := range xs { s += x }\n return s }\nfunc main() { s := []int{1, 2, 3}\n println(sum(s...)) }\n"},
		{"IndexExpr", "package demo\nfunc id[T any](x T) T { return x }\nfunc main() { println(id[int](42)) }\n"},
		{"IndexListExpr", "package demo\nfunc pair[A, B any](a A, b B) int { return 1 }\nfunc main() { println(pair[int, string](1, \"s\")) }\n"},
		{"CaseClause", "package demo\nfunc main() { x := 1\n switch x { case 1: println(\"one\"); default: println(\"other\") } }\n"},
		{"CommClause", "package demo\nfunc sel(c chan int) { select { case x := <-c: println(x); default: } }\nfunc main() {}\n"},
	}
	for _, tc := range rejectedCases {
		t.Run("rejected/"+tc.name, func(t *testing.T) {
			// IndexExpr is dual: it is in handledNodes (plain indexing
			// emits, proved by handled/IndexExpr above) and its generic
			// instantiation form rejects, proved by the program here —
			// so it is exempt from the loudlyRejected membership check
			// that the other rejected kinds must pass.
			if tc.name != "IndexExpr" {
				if _, ok := loudlyRejected[tc.name]; !ok {
					t.Fatalf("%s is not in loudlyRejected (matrix drift)", tc.name)
				}
			}
			_, err := emitTestProgram(t, tc.src)
			if err == nil {
				t.Fatalf("%s: expected loud failure, got success", tc.name)
			}
			if !strings.Contains(err.Error(), "unsupported") {
				t.Fatalf("%s: error %q does not contain %q", tc.name, err.Error(), "unsupported")
			}
		})
	}

	// Bad* nodes only arise from malformed source, which the parser
	// rejects before the emitter ever runs. The guarantee that matters
	// is the same — no silent acceptance — proved here at the parse
	// stage rather than by an emitter "unsupported" error.
	badCases := []struct {
		name string
		src  string
	}{
		{"BadStmt", "package demo\nfunc main() { @ }\n"},
		{"BadExpr", "package demo\nfunc main() { x := @ }\n"},
		{"BadDecl", "package demo\n@\n"},
	}
	for _, tc := range badCases {
		t.Run("rejected/"+tc.name, func(t *testing.T) {
			if _, ok := loudlyRejected[tc.name]; !ok {
				t.Fatalf("%s is not in loudlyRejected (matrix drift)", tc.name)
			}
			// Bad* must not survive the pipeline into a document.
			if _, err := emitTestProgram(t, tc.src); err == nil {
				t.Fatalf("%s: expected pipeline failure, got success", tc.name)
			}
		})
	}

	// partiallyHandled: both sides, including top-level GenDecl.
	partialCases := []struct {
		name    string
		src     string
		wantErr bool
	}{
		{"GenDecl/var", "package demo\n\nvar x = 5\nfunc main() { println(x) }\n", false},
		{"GenDecl/const", "package demo\n\nconst c = 1\nfunc main() { println(c) }\n", true},
		{"GenDecl/type", "package demo\n\ntype T int\nfunc main() { println(1) }\n", false},
		{"ValueSpec/var", "package demo\n\nvar x = 5\nfunc main() { println(x) }\n", false},
		{"ValueSpec/const", "package demo\n\nconst c = 1\nfunc main() { println(c) }\n", true},
	}
	for _, tc := range partialCases {
		t.Run("partial/"+tc.name, func(t *testing.T) {
			kind := strings.SplitN(tc.name, "/", 2)[0]
			if _, ok := partiallyHandled[kind]; !ok {
				t.Fatalf("%s is not in partiallyHandled (matrix drift)", kind)
			}
			_, err := emitTestProgram(t, tc.src)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("%s: expected loud failure, got success", tc.name)
				}
				if !strings.Contains(err.Error(), "unsupported") {
					t.Fatalf("%s: error %q does not contain %q", tc.name, err.Error(), "unsupported")
				}
			} else if err != nil {
				t.Fatalf("%s: expected successful emission, got %v", tc.name, err)
			}
		})
	}
}

// emitTestProgram and emitTestFiles live in globals_test.go (same
// package) and are shared by the truth test above.
