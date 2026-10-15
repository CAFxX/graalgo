package vet

import (
	"go/ast"
)

func init() {
	register(&Analyzer{
		Name: "unreachable",
		Doc:  "check for unreachable code",
		Run:  runUnreachable,
	})
}

func runUnreachable(pass *Pass) []Diagnostic {
	var out []Diagnostic
	for _, f := range pass.Files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			out = append(out, checkBlockUnreachable(pass, fn.Body.List)...)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.FuncLit)
			if ok && lit.Body != nil {
				out = append(out, checkBlockUnreachable(pass, lit.Body.List)...)
			}
			return true
		})
	}
	return out
}

// checkBlockUnreachable reports statements that follow a terminating
// statement in the same block.
func checkBlockUnreachable(pass *Pass, list []ast.Stmt) []Diagnostic {
	var out []Diagnostic
	for i, s := range list {
		// Recurse into nested blocks first.
		out = append(out, checkStmtUnreachable(pass, s)...)
		if i+1 < len(list) && terminates(s) {
			out = append(out, Diagnostic{
				pass.position(list[i+1]),
				"unreachable code",
			})
			// Only report the first unreachable statement per block
			// to avoid cascading.
			break
		}
	}
	return out
}

func checkStmtUnreachable(pass *Pass, s ast.Stmt) []Diagnostic {
	var out []Diagnostic
	switch s := s.(type) {
	case *ast.IfStmt:
		if s.Body != nil {
			out = append(out, checkBlockUnreachable(pass, s.Body.List)...)
		}
		if s.Else != nil {
			switch e := s.Else.(type) {
			case *ast.BlockStmt:
				out = append(out, checkBlockUnreachable(pass, e.List)...)
			case *ast.IfStmt:
				out = append(out, checkStmtUnreachable(pass, e)...)
			}
		}
	case *ast.ForStmt:
		if s.Body != nil {
			out = append(out, checkBlockUnreachable(pass, s.Body.List)...)
		}
	case *ast.RangeStmt:
		if s.Body != nil {
			out = append(out, checkBlockUnreachable(pass, s.Body.List)...)
		}
	case *ast.SwitchStmt:
		if s.Body != nil {
			for _, c := range s.Body.List {
				if cc, ok := c.(*ast.CaseClause); ok {
					out = append(out, checkBlockUnreachable(pass, cc.Body)...)
				}
			}
		}
	case *ast.TypeSwitchStmt:
		if s.Body != nil {
			for _, c := range s.Body.List {
				if cc, ok := c.(*ast.CaseClause); ok {
					out = append(out, checkBlockUnreachable(pass, cc.Body)...)
				}
			}
		}
	case *ast.SelectStmt:
		if s.Body != nil {
			for _, c := range s.Body.List {
				if cc, ok := c.(*ast.CommClause); ok {
					out = append(out, checkBlockUnreachable(pass, cc.Body)...)
				}
			}
		}
	case *ast.BlockStmt:
		out = append(out, checkBlockUnreachable(pass, s.List)...)
	case *ast.LabeledStmt:
		out = append(out, checkStmtUnreachable(pass, s.Stmt)...)
	}
	return out
}

// terminates reports whether s always exits the current block
// (return, panic, break/continue/goto/fallthrough, or an infinite
// for loop without break).
func terminates(s ast.Stmt) bool {
	switch s := s.(type) {
	case *ast.ReturnStmt, *ast.BranchStmt:
		// return, break, continue, goto, fallthrough
		return true
	case *ast.ExprStmt:
		// panic(...) terminates.
		if call, ok := s.X.(*ast.CallExpr); ok {
			if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "panic" {
				return true
			}
		}
	case *ast.ForStmt:
		// for {} with no break terminates (infinite loop).
		if s.Cond == nil && s.Init == nil && s.Post == nil {
			if !hasBreak(s.Body) {
				return true
			}
		}
	}
	return false
}

// hasBreak reports whether the loop body contains a break that
// targets this loop (conservatively: any break not inside a nested
// for/switch/select).
func hasBreak(body *ast.BlockStmt) bool {
	if body == nil {
		return false
	}
	found := false
	var walk func(list []ast.Stmt, depth int)
	walk = func(list []ast.Stmt, depth int) {
		for _, s := range list {
			if found {
				return
			}
			switch s := s.(type) {
			case *ast.BranchStmt:
				if s.Tok.String() == "break" && s.Label == nil && depth == 0 {
					found = true
				}
			case *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt,
				*ast.TypeSwitchStmt, *ast.SelectStmt:
				// Nested loop/switch: breaks inside target it.
				continue
			case *ast.BlockStmt:
				walk(s.List, depth)
			case *ast.IfStmt:
				if s.Body != nil {
					walk(s.Body.List, depth)
				}
			}
		}
	}
	walk(body.List, 0)
	return found
}
