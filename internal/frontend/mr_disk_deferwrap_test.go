package main

// FE-1 / FE-58: structural tests for the defer-aware multi-result disk
// transform.
//
// The old transform rewrote every `return` to `return __F_pair{...}`,
// constructing the pair at the return statement — before deferred
// functions run. A deferred closure modifying a named result was silently
// discarded (gc: caller gets 42; old output: caller gets 1). The tests
// below assert the EMITTED SHAPE, not just "the rewritten AST
// type-checks": a type-check-only test is exactly what let FE-1 survive.
//
// Wrapper shape (defer present + named results): the helper body is
//   var __mr_result0 T; ...; func() { ... }(); return __F_pair{...}
// Defers registered by the body run when the closure returns, strictly
// before the single pair construction. Every return inside the closure
// is bare and preceded by assignments to the result locals.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// transformForShape parses src and runs transformDiskMultiResult,
// returning the transformed file for structural assertions.
func transformForShape(t *testing.T, src string) *ast.File {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	transformDiskMultiResult("p", []*ast.File{f})
	return f
}

// findHelper returns the FuncDecl named helperName, failing the test if
// absent.
func findHelper(t *testing.T, f *ast.File, helperName string) *ast.FuncDecl {
	t.Helper()
	for _, decl := range f.Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok && fd.Name.Name == helperName {
			return fd
		}
	}
	t.Fatalf("helper %s not found in transformed file", helperName)
	return nil
}

// topLevelReturns collects ReturnStmts in fd's body without descending
// into function literals (their returns belong to the literal).
func topLevelReturns(body *ast.BlockStmt) []*ast.ReturnStmt {
	var out []*ast.ReturnStmt
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			out = append(out, n)
		}
		return true
	})
	return out
}

// isPairReturn reports whether rs returns a single pair composite literal.
func isPairReturn(rs *ast.ReturnStmt) bool {
	if len(rs.Results) != 1 {
		return false
	}
	_, ok := rs.Results[0].(*ast.CompositeLit)
	return ok
}

// assertDeferWrapShape asserts the FE-1 wrapper shape on the helper:
// the single pair construction is the last statement, preceded by the
// immediately-invoked closure; no pair construction anywhere inside the
// closure; every return inside the closure is bare.
func assertDeferWrapShape(t *testing.T, fd *ast.FuncDecl, wantResults int) *ast.FuncLit {
	t.Helper()
	body := fd.Body.List
	if len(body) < 2 {
		t.Fatalf("wrapped helper body has %d statements, want >= 2 (closure + tail return)", len(body))
	}
	// The single pair construction is the last statement ...
	tail, ok := body[len(body)-1].(*ast.ReturnStmt)
	if !ok || !isPairReturn(tail) {
		t.Fatalf("last helper statement is not the single pair return")
	}
	if lit := tail.Results[0].(*ast.CompositeLit); len(lit.Elts) != wantResults {
		t.Fatalf("pair literal has %d elements, want %d", len(lit.Elts), wantResults)
	}
	// ... and it is the ONLY pair-constructing return at the top level
	// (returns in nested blocks are bare and live inside the closure).
	pairReturns := 0
	for _, rs := range topLevelReturns(fd.Body) {
		if isPairReturn(rs) {
			pairReturns++
		}
	}
	if pairReturns != 1 {
		t.Fatalf("helper has %d pair-constructing top-level returns, want exactly 1 (the trailing pair)", pairReturns)
	}
	// Immediately before it: the IIFE statement.
	exprStmt, ok := body[len(body)-2].(*ast.ExprStmt)
	if !ok {
		t.Fatalf("statement before tail return is not an expression statement")
	}
	call, ok := exprStmt.X.(*ast.CallExpr)
	if !ok {
		t.Fatalf("statement before tail return is not a call")
	}
	iife, ok := call.Fun.(*ast.FuncLit)
	if !ok {
		t.Fatalf("statement before tail return is not an immediately-invoked func literal")
	}
	// Inside the closure: no pair construction, every return bare.
	ast.Inspect(iife.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false // nested literals have their own returns
		case *ast.ReturnStmt:
			if len(n.Results) != 0 {
				t.Fatalf("non-bare return inside the defer-wrapper closure")
			}
		}
		return true
	})
	ast.Inspect(iife.Body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok && n != iife {
			return false
		}
		if rs, ok := n.(*ast.ReturnStmt); ok && isPairReturn(rs) {
			t.Fatalf("pair constructed inside the defer-wrapper closure (before defers run)")
		}
		return true
	})
	return iife
}

// findAssignToResult finds `__mr_result<i>, ... = rhs` (Tok == ASSIGN, not
// DEFINE) inside the closure body.
func findAssignToResult(t *testing.T, iife *ast.FuncLit) *ast.AssignStmt {
	t.Helper()
	var found *ast.AssignStmt
	ast.Inspect(iife.Body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if as, ok := n.(*ast.AssignStmt); ok && as.Tok == token.ASSIGN {
			for _, lhs := range as.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && strings.HasPrefix(id.Name, "__mr_result") {
					found = as
					return false
				}
			}
		}
		return true
	})
	if found == nil {
		t.Fatalf("no assignment to __mr_result locals inside the defer-wrapper closure")
	}
	return found
}

// TestMRDeferWrapExplicitReturn: defer modifies named results; explicit
// return values. The wrapper must defer pair construction until after the
// deferred closure ran.
func TestMRDeferWrapExplicitReturn(t *testing.T) {
	src := `package p

func f() (x int, err error) {
	defer func() { x = 42 }()
	return 1, nil
}
`
	if err := checkMRTransform(t, src); err != nil {
		t.Fatalf("type-check after transform: %v", err)
	}
	fd := findHelper(t, transformForShape(t, src), "__f")
	iife := assertDeferWrapShape(t, fd, 2)
	as := findAssignToResult(t, iife)
	if len(as.Lhs) != 2 || len(as.Rhs) != 2 {
		t.Fatalf("result assignment has %d lhs / %d rhs, want 2/2", len(as.Lhs), len(as.Rhs))
	}
	// The deferred closure's write must target the hygienic local, not
	// the (renamed-away) original name: no ident "x" may remain in the
	// helper body.
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == "x" {
			t.Fatalf("original result name x still referenced in helper body")
		}
		return true
	})
}

// TestMRDeferWrapBareReturn: bare return + defer. The bare return stays
// bare inside the closure; the pair is built from the locals afterwards.
func TestMRDeferWrapBareReturn(t *testing.T) {
	src := `package p

func g(cond bool) (a string, b int) {
	defer func() { a = "deferred" }()
	if cond {
		a = "x"
		b = 1
		return
	}
	return "y", 2
}
`
	if err := checkMRTransform(t, src); err != nil {
		t.Fatalf("type-check after transform: %v", err)
	}
	fd := findHelper(t, transformForShape(t, src), "__g")
	iife := assertDeferWrapShape(t, fd, 2)
	findAssignToResult(t, iife) // the explicit return's assignment
	// A bare return exists inside the closure (the if-branch one).
	bare := 0
	ast.Inspect(iife.Body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if rs, ok := n.(*ast.ReturnStmt); ok && len(rs.Results) == 0 {
			bare++
		}
		return true
	})
	if bare == 0 {
		t.Fatalf("no bare return inside the defer-wrapper closure")
	}
}

// TestMRDeferWrapForwardingReturn: `return h()` forwarding + defer. The
// helper call stays inside the closure; its pair fields are assigned to
// the result locals.
func TestMRDeferWrapForwardingReturn(t *testing.T) {
	src := `package p

func h() (int, error) { return 1, nil }

func f() (x int, err error) {
	defer func() { x = 9 }()
	return h()
}
`
	if err := checkMRTransform(t, src); err != nil {
		t.Fatalf("type-check after transform: %v", err)
	}
	fd := findHelper(t, transformForShape(t, src), "__f")
	iife := assertDeferWrapShape(t, fd, 2)
	// __mr_tmp := __h() must occur inside the closure, before the tail.
	foundHelperCall := false
	ast.Inspect(iife.Body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if as, ok := n.(*ast.AssignStmt); ok && as.Tok == token.DEFINE {
			if id, ok := as.Lhs[0].(*ast.Ident); ok && id.Name == "__mr_tmp" {
				foundHelperCall = true
			}
		}
		return true
	})
	if !foundHelperCall {
		t.Fatalf("__mr_tmp := __h() not found inside the defer-wrapper closure")
	}
	findAssignToResult(t, iife)
}

// TestMRNoDeferKeepsDirectShape: without a defer the emitted shape is
// unchanged — pair-returns directly in the helper body, no wrapper.
func TestMRNoDeferKeepsDirectShape(t *testing.T) {
	src := `package p

func f() (x int, err error) {
	return 1, nil
}
`
	if err := checkMRTransform(t, src); err != nil {
		t.Fatalf("type-check after transform: %v", err)
	}
	fd := findHelper(t, transformForShape(t, src), "__f")
	direct := 0
	for _, stmt := range fd.Body.List {
		if rs, ok := stmt.(*ast.ReturnStmt); ok && isPairReturn(rs) {
			direct++
		}
		if _, ok := stmt.(*ast.ExprStmt); ok {
			t.Fatalf("unexpected expression statement (IIFE?) in no-defer helper")
		}
	}
	if direct == 0 {
		t.Fatalf("no-defer helper lost its direct pair return")
	}
	if rs := topLevelReturns(fd.Body); len(rs) != 1 {
		t.Fatalf("no-defer helper has %d top-level returns, want 1", len(rs))
	}
}

// TestMRDeferInNestedLiteralNotWrapped: a defer inside a nested function
// literal belongs to the literal, not to the transformed function — it
// must not trigger the wrapper.
func TestMRDeferInNestedLiteralNotWrapped(t *testing.T) {
	src := `package p

func f() (x int, y int) {
	g := func() {
		defer func() {}()
	}
	g()
	return 1, 2
}
`
	if err := checkMRTransform(t, src); err != nil {
		t.Fatalf("type-check after transform: %v", err)
	}
	fd := findHelper(t, transformForShape(t, src), "__f")
	for _, stmt := range fd.Body.List {
		if es, ok := stmt.(*ast.ExprStmt); ok {
			if call, ok := es.X.(*ast.CallExpr); ok {
				if _, ok := call.Fun.(*ast.FuncLit); ok {
					t.Fatalf("wrapper applied for a defer nested inside a function literal")
				}
			}
		}
	}
}

// TestMRDeferUnnamedResultsNotWrapped: with unnamed results no deferred
// closure can reference them, so the direct shape is already correct.
func TestMRDeferUnnamedResultsNotWrapped(t *testing.T) {
	src := `package p

func f() (int, error) {
	defer func() {}()
	return 1, nil
}
`
	if err := checkMRTransform(t, src); err != nil {
		t.Fatalf("type-check after transform: %v", err)
	}
	fd := findHelper(t, transformForShape(t, src), "__f")
	if rs := topLevelReturns(fd.Body); len(rs) != 1 || !isPairReturn(rs[0]) {
		t.Fatalf("unnamed-results helper lost its direct pair return")
	}
}

// TestMRDeferAllBlankResultsNotWrapped: blank results are not
// referenceable either.
func TestMRDeferAllBlankResultsNotWrapped(t *testing.T) {
	src := `package p

func f() (_ int, _ error) {
	defer func() {}()
	return 1, nil
}
`
	if err := checkMRTransform(t, src); err != nil {
		t.Fatalf("type-check after transform: %v", err)
	}
	fd := findHelper(t, transformForShape(t, src), "__f")
	if rs := topLevelReturns(fd.Body); len(rs) != 1 || !isPairReturn(rs[0]) {
		t.Fatalf("all-blank-results helper lost its direct pair return")
	}
}

// TestMRShadowedResultBareReturn: an inner := shadowing a result name
// must not capture the result references. gc reads the RESULT at a bare
// return, so the pair must be built from the hygienic locals.
func TestMRShadowedResultBareReturn(t *testing.T) {
	src := `package p

func f() (x int, y int) {
	x = 1
	y = 2
	{
		x := 100
		_ = x
		return
	}
}
`
	if err := checkMRTransform(t, src); err != nil {
		t.Fatalf("type-check after transform: %v", err)
	}
	fd := findHelper(t, transformForShape(t, src), "__f")
	rs := topLevelReturns(fd.Body)
	if len(rs) != 1 || !isPairReturn(rs[0]) {
		t.Fatalf("expected a single direct pair return")
	}
	lit := rs[0].Results[0].(*ast.CompositeLit)
	if len(lit.Elts) != 2 {
		t.Fatalf("pair literal has %d elements, want 2", len(lit.Elts))
	}
	for i, e := range lit.Elts {
		id, ok := e.(*ast.Ident)
		if !ok || !strings.HasPrefix(id.Name, "__mr_result") {
			t.Fatalf("pair element %d is not a hygienic result local", i)
		}
	}
	// The shadowing declaration itself is untouched (still declares x).
	shadowed := false
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if as, ok := n.(*ast.AssignStmt); ok && as.Tok == token.DEFINE {
			if id, ok := as.Lhs[0].(*ast.Ident); ok && id.Name == "x" {
				shadowed = true
			}
		}
		return true
	})
	if !shadowed {
		t.Fatalf("shadowing x := declaration lost in rewrite")
	}
}

// TestMRShadowedResultDeferWrap: shadowing + defer combined — the
// deferred write and the return assignment must both target the hygienic
// local, never the shadowing declaration.
func TestMRShadowedResultDeferWrap(t *testing.T) {
	src := `package p

func f() (x int) {
	defer func() { x = 7 }()
	{
		x := 100
		_ = x
	}
	return 1
}
`
	if err := checkMRTransform(t, src); err != nil {
		t.Fatalf("type-check after transform: %v", err)
	}
	fd := findHelper(t, transformForShape(t, src), "__f")
	iife := assertDeferWrapShape(t, fd, 1)
	findAssignToResult(t, iife)
	// Every assignment to a __mr_result local must exist; no remaining
	// ident may be the original result name x outside its shadowing
	// declaration.
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, lhs := range as.Lhs {
			if id, ok := lhs.(*ast.Ident); ok && id.Name == "x" && as.Tok != token.DEFINE {
				t.Fatalf("non-declaration assignment to shadowed name x in helper body")
			}
		}
		return true
	})
}
