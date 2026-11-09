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
	case *ast.LabeledStmt:
		// A labeled statement terminates iff the statement it labels
		// does, e.g. `Outer: for {}` with no break.
		return terminates(s.Stmt)
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

// hasBreak reports whether the loop body contains a break that exits
// this loop: an unlabeled break directly in the loop body, or a labeled
// break whose label is not defined on any statement inside the body
// (so it must name the loop itself or an enclosing construct, and
// breaking to it leaves the loop).
//
// A labeled break to a label defined inside the body exits only the
// nested construct carrying that label, not this loop, so it does not
// count. Unlabeled breaks inside nested loops, switches, and selects
// target those constructs, so they do not count either — but a labeled
// break inside them can still exit this loop, so nested bodies are
// walked for labeled breaks.
func hasBreak(body *ast.BlockStmt) bool {
	if body == nil {
		return false
	}
	// Labels defined inside the body. A break to one of these stays
	// inside the loop. Labels are function-scoped, so closures get
	// their own scope: do not collect labels from inside a func
	// literal, where the same name may denote a different label.
	inner := map[string]bool{}
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
	var walk func(list []ast.Stmt, depth int)
	walkCases := func(body *ast.BlockStmt, depth int, comm bool) {
		if body == nil {
			return
		}
		for _, c := range body.List {
			if cc, ok := c.(*ast.CaseClause); ok && !comm {
				walk(cc.Body, depth)
			}
			if cc, ok := c.(*ast.CommClause); ok && comm {
				walk(cc.Body, depth)
			}
		}
	}
	walk = func(list []ast.Stmt, depth int) {
		for _, s := range list {
			if found {
				return
			}
			switch s := s.(type) {
			case *ast.BranchStmt:
				if s.Tok == token.BREAK {
					if s.Label == nil {
						if depth == 0 {
							found = true
						}
					} else if !inner[s.Label.Name] {
						// Breaks to a label outside the body:
						// exits the loop (and anything enclosing
						// it up to the labeled construct).
						found = true
					}
				}
			case *ast.LabeledStmt:
				walk([]ast.Stmt{s.Stmt}, depth)
			case *ast.ForStmt:
				if s.Body != nil {
					walk(s.Body.List, depth+1)
				}
			case *ast.RangeStmt:
				if s.Body != nil {
					walk(s.Body.List, depth+1)
				}
			case *ast.SwitchStmt:
				walkCases(s.Body, depth+1, false)
			case *ast.TypeSwitchStmt:
				walkCases(s.Body, depth+1, false)
			case *ast.SelectStmt:
				walkCases(s.Body, depth+1, true)
			case *ast.BlockStmt:
				walk(s.List, depth)
			case *ast.IfStmt:
				if s.Body != nil {
					walk(s.Body.List, depth)
				}
				if s.Else != nil {
					switch e := s.Else.(type) {
					case *ast.BlockStmt:
						walk(e.List, depth)
					case *ast.IfStmt:
						walk([]ast.Stmt{e}, depth)
					}
				}
			}
		}
	}
	walk(body.List, 0)
	return found
}
