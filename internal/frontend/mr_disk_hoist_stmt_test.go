package main

// Regression tests for the nested-MR hoist (hoistNestedMRInStmt) in
// statement positions (REVIEW-2026-11-02-15 F1 and P2-F1).
//
// F1: the SwitchStmt case hoisted nested MR temps into a throwaway
// BlockStmt wrapper and then "copied back" from a fresh wrapper around
// the unchanged clause body — a no-op. The rewritten case-body
// expressions still referenced __mr_arg_N while the temp assignments
// were dropped, exporting a reference to an undeclared temp.
//
// P2-F1: the same pass handled only Return/Assign/Expr/If/For/Range/
// Switch/Defer/Go/Block, leaving SelectStmt, TypeSwitchStmt,
// LabeledStmt, SendStmt, and DeclStmt unhoisted.
//
// Every test below asserts the same invariant at the rewritten AST:
// each __mr_arg_N reference appears only after a := definition of
// that temp in the same statement list, in order. Before the F1 fix
// the switch test fails (the case body holds only the rewritten
// assignment, no temp definition); before the P2-F1 fix the other
// tests fail the same way (no temp definition at all, or the temp
// defined after its use).

import (
	"go/ast"
	"go/token"
	"strings"
	"testing"
)

// assertHoistTempsDefined checks, over one statement list in order,
// that every __mr_arg_N reference follows a := definition of that temp
// in the same list. A statement's own := definitions are not uses.
func assertHoistTempsDefined(t *testing.T, stmts []ast.Stmt) {
	t.Helper()
	defined := map[string]bool{}
	for _, st := range stmts {
		ast.Inspect(st, func(n ast.Node) bool {
			if as, ok := n.(*ast.AssignStmt); ok && as.Tok == token.DEFINE {
				return false // definitions are not uses
			}
			if id, ok := n.(*ast.Ident); ok && strings.HasPrefix(id.Name, "__mr_arg_") {
				if !defined[id.Name] {
					t.Errorf("reference to %s before its temp assignment (statement: %#v)", id.Name, st)
				}
			}
			return true
		})
		ast.Inspect(st, func(n ast.Node) bool {
			if as, ok := n.(*ast.AssignStmt); ok && as.Tok == token.DEFINE {
				for _, lhs := range as.Lhs {
					if id, ok := lhs.(*ast.Ident); ok {
						defined[id.Name] = true
					}
				}
			}
			return true
		})
	}
	if len(defined) == 0 {
		t.Fatalf("no __mr_arg_ temp defined in statement list %#v", stmts)
	}
}

// assertFirstStmtIsTempDefine requires stmts[0] to be
// __mr_arg_N := __Inner(), returning the temp name.
func assertFirstStmtIsTempDefine(t *testing.T, stmts []ast.Stmt) string {
	t.Helper()
	if len(stmts) == 0 {
		t.Fatalf("empty statement list, want temp definition first")
	}
	as, ok := stmts[0].(*ast.AssignStmt)
	if !ok || as.Tok != token.DEFINE || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
		t.Fatalf("first statement is %#v, want __mr_arg_N := __Inner()", stmts[0])
	}
	id, ok := as.Lhs[0].(*ast.Ident)
	if !ok || !strings.HasPrefix(id.Name, "__mr_arg_") {
		t.Fatalf("first statement LHS is %#v, want a __mr_arg_ temp", as.Lhs[0])
	}
	call, ok := as.Rhs[0].(*ast.CallExpr)
	if !ok {
		t.Fatalf("temp RHS is %#v, want a call to __Inner", as.Rhs[0])
	}
	fn, ok := call.Fun.(*ast.Ident)
	if !ok || fn.Name != "__Inner" {
		t.Fatalf("temp RHS callee is %#v, want __Inner", call.Fun)
	}
	return id.Name
}

func findHoistFuncDecl(t *testing.T, f *ast.File, name string) *ast.FuncDecl {
	t.Helper()
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == name {
			return fd
		}
	}
	t.Fatalf("no func %s in rewritten file", name)
	return nil
}

// firstCaseBody returns the body of the first non-default case clause
// of the first switch (or type-switch) statement in fd.
func firstCaseBody(t *testing.T, fd *ast.FuncDecl) []ast.Stmt {
	t.Helper()
	var body []ast.Stmt
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if body != nil {
			return false
		}
		switch s := n.(type) {
		case *ast.SwitchStmt:
			for _, cc := range s.Body.List {
				if c, ok := cc.(*ast.CaseClause); ok && c.List != nil {
					body = c.Body
					return false
				}
			}
		case *ast.TypeSwitchStmt:
			for _, cc := range s.Body.List {
				if c, ok := cc.(*ast.CaseClause); ok && c.List != nil {
					body = c.Body
					return false
				}
			}
		}
		return true
	})
	if body == nil {
		t.Fatalf("no non-default case clause found in %s", fd.Name.Name)
	}
	return body
}

const hoistStmtPrelude = `
func Inner() (int, error) { return 1, nil }

func Sink(n int, err error) int { return n }
`

func TestDiskMRHoistSwitchCaseKeepsTemps(t *testing.T) {
	// F1: nested MR call in argument position inside a switch case body.
	f := transformParse(t, `package p
`+hoistStmtPrelude+`
func Use(k int) int {
	var x int
	switch k {
	case 1:
		x = Sink(Inner())
	}
	return x
}
`)
	body := firstCaseBody(t, findHoistFuncDecl(t, f, "Use"))
	tmp := assertFirstStmtIsTempDefine(t, body)
	assertHoistTempsDefined(t, body)
	if len(body) < 2 {
		t.Fatalf("case body has %d statements, want temp + assignment", len(body))
	}
	as, ok := body[1].(*ast.AssignStmt)
	if !ok || len(as.Lhs) != 1 {
		t.Fatalf("second case statement is %#v, want x = Sink(%s...)", body[1], tmp)
	}
}

func TestDiskMRHoistTypeSwitchCaseKeepsTemps(t *testing.T) {
	// P2-F1: nested MR call inside a type-switch case body.
	f := transformParse(t, `package p
`+hoistStmtPrelude+`
func Use(v any) int {
	var x int
	switch t := v.(type) {
	case int:
		x = Sink(Inner())
	default:
		_ = t
	}
	return x
}
`)
	body := firstCaseBody(t, findHoistFuncDecl(t, f, "Use"))
	assertFirstStmtIsTempDefine(t, body)
	assertHoistTempsDefined(t, body)
}

func TestDiskMRHoistSelectCommKeepsTemps(t *testing.T) {
	// P2-F1: nested MR call in a select send clause. The temp must be
	// defined before the select statement (comm expressions are
	// evaluated on entering the select, in source order).
	f := transformParse(t, `package p
`+hoistStmtPrelude+`
func Use(ch chan int) int {
	var x int
	select {
	case ch <- Sink(Inner()):
		x = 1
	default:
		x = 2
	}
	return x
}
`)
	fd := findHoistFuncDecl(t, f, "Use")
	assertHoistTempsDefined(t, fd.Body.List)
	// The temp definition must precede the select itself.
	seenTemp := false
	for _, st := range fd.Body.List {
		if _, ok := st.(*ast.SelectStmt); ok {
			if !seenTemp {
				t.Fatalf("select statement precedes its hoisted temp definition")
			}
			break
		}
		if as, ok := st.(*ast.AssignStmt); ok && as.Tok == token.DEFINE {
			seenTemp = true
		}
	}
}

func TestDiskMRHoistLocalVarDeclKeepsTemps(t *testing.T) {
	// P2-F1: nested MR call in a local var declaration.
	f := transformParse(t, `package p
`+hoistStmtPrelude+`
func Use() int {
	var x = Sink(Inner())
	return x
}
`)
	fd := findHoistFuncDecl(t, f, "Use")
	assertFirstStmtIsTempDefine(t, fd.Body.List)
	assertHoistTempsDefined(t, fd.Body.List)
	if len(fd.Body.List) < 2 {
		t.Fatalf("function body has %d statements, want temp + var decl", len(fd.Body.List))
	}
	if _, ok := fd.Body.List[1].(*ast.DeclStmt); !ok {
		t.Fatalf("second statement is %#v, want the var declaration", fd.Body.List[1])
	}
}

func TestDiskMRHoistLabeledForKeepsTemps(t *testing.T) {
	// P2-F1: nested MR call inside a labeled for loop body.
	f := transformParse(t, `package p
`+hoistStmtPrelude+`
func Use(k int) int {
	var x int
loop:
	for k > 0 {
		x = Sink(Inner())
		k--
	}
	return x
}
`)
	fd := findHoistFuncDecl(t, f, "Use")
	var forBody []ast.Stmt
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if fs, ok := n.(*ast.ForStmt); ok {
			forBody = fs.Body.List
			return false
		}
		return true
	})
	if forBody == nil {
		t.Fatalf("no for statement found in %s", fd.Name.Name)
	}
	assertFirstStmtIsTempDefine(t, forBody)
	assertHoistTempsDefined(t, forBody)
}

func TestDiskMRHoistSendStmtKeepsTemps(t *testing.T) {
	// P2-F1: nested MR call as a send value.
	f := transformParse(t, `package p
`+hoistStmtPrelude+`
func Use(ch chan int) {
	ch <- Sink(Inner())
}
`)
	fd := findHoistFuncDecl(t, f, "Use")
	assertFirstStmtIsTempDefine(t, fd.Body.List)
	assertHoistTempsDefined(t, fd.Body.List)
	if len(fd.Body.List) < 2 {
		t.Fatalf("function body has %d statements, want temp + send", len(fd.Body.List))
	}
	if _, ok := fd.Body.List[1].(*ast.SendStmt); !ok {
		t.Fatalf("second statement is %#v, want the send", fd.Body.List[1])
	}
}
