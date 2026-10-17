package vet

import (
	"go/ast"
	"go/token"
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
		if i+1 < len(list) && terminates(s, list[i+1:]) {
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

// terminates reports whether s always exits the current block: via
// return, panic, break/continue/fallthrough, a forward goto to a label
// defined later in the same block, or an infinite for loop with no exit.
// rest is the remainder of the block after s: a goto terminates the block
// only when it jumps forward to a label in rest. A backward goto is a
// loop-back edge — it does not by itself make rest unreachable, since
// rest may be reached via other jumps.
func terminates(s ast.Stmt, rest []ast.Stmt) bool {
	switch s := s.(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BranchStmt:
		// break, continue and fallthrough always skip the rest of the
		// block. A goto does so only when jumping forward within the
		// same block.
		if s.Tok == token.GOTO {
			if s.Label == nil {
				return false
			}
			for _, r := range rest {
				if ls, ok := r.(*ast.LabeledStmt); ok && ls.Label.Name == s.Label.Name {
					return true
				}
			}
			return false
		}
		return true
	case *ast.ExprStmt:
		// panic(...) terminates.
		if call, ok := s.X.(*ast.CallExpr); ok {
			if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "panic" {
				return true
			}
		}
	case *ast.ForStmt:
		// for {} with no exit terminates (infinite loop).
		if s.Cond == nil && s.Init == nil && s.Post == nil {
			if !hasLoopExit(s.Body) {
				return true
			}
		}
	}
	return false
}

// hasLoopExit reports whether the loop body contains a statement that can
// leave the loop: a return (outside a nested function literal, whose
// return belongs to the literal), an unlabeled break at the loop's own
// depth, or a break/goto targeting a label not defined inside the loop
// body. It errs toward reporting an exit (a missed infinite loop is a
// false negative; a missed exit is a false "unreachable" positive).
func hasLoopExit(body *ast.BlockStmt) bool {
	if body == nil {
		return false
	}
	// Labels defined inside the loop body: a break/goto targeting one of
	// these stays inside the loop. Labels inside nested function
	// literals are excluded — they belong to the literal.
	inner := make(map[string]bool)
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.LabeledStmt:
			inner[n.Label.Name] = true
		}
		return true
	})
	found := false
	scanLoopStmts(body.List, 0, inner, &found)
	return found
}

// scanLoopStmts looks for loop exits in list. depth counts the enclosing
// for/range/switch/select statements between the loop body and list: an
// unlabeled break at depth > 0 targets a nested construct, not the loop.
func scanLoopStmts(list []ast.Stmt, depth int, inner map[string]bool, found *bool) {
	for _, s := range list {
		if *found {
			return
		}
		switch s := s.(type) {
		case *ast.ReturnStmt:
			*found = true
		case *ast.BranchStmt:
			switch s.Tok {
			case token.BREAK:
				if s.Label == nil {
					if depth == 0 {
						*found = true
					}
				} else if !inner[s.Label.Name] {
					// Labeled break targeting a label outside the
					// loop body leaves the loop.
					*found = true
				}
			case token.GOTO:
				if s.Label != nil && !inner[s.Label.Name] {
					// Goto jumping outside the loop body leaves it.
					*found = true
				}
			}
		case *ast.BlockStmt:
			scanLoopStmts(s.List, depth, inner, found)
		case *ast.IfStmt:
			if s.Body != nil {
				scanLoopStmts(s.Body.List, depth, inner, found)
			}
			if s.Else != nil {
				scanLoopStmts([]ast.Stmt{s.Else}, depth, inner, found)
			}
		case *ast.ForStmt:
			if s.Body != nil {
				scanLoopStmts(s.Body.List, depth+1, inner, found)
			}
		case *ast.RangeStmt:
			if s.Body != nil {
				scanLoopStmts(s.Body.List, depth+1, inner, found)
			}
		case *ast.SwitchStmt:
			if s.Body != nil {
				scanLoopStmts(s.Body.List, depth+1, inner, found)
			}
		case *ast.TypeSwitchStmt:
			if s.Body != nil {
				scanLoopStmts(s.Body.List, depth+1, inner, found)
			}
		case *ast.SelectStmt:
			if s.Body != nil {
				scanLoopStmts(s.Body.List, depth+1, inner, found)
			}
		case *ast.LabeledStmt:
			scanLoopStmts([]ast.Stmt{s.Stmt}, depth, inner, found)
		case *ast.CaseClause:
			scanLoopStmts(s.Body, depth, inner, found)
		case *ast.CommClause:
			scanLoopStmts(s.Body, depth, inner, found)
		}
	}
}
