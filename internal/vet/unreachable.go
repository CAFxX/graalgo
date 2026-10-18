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
// for loop with no exit).
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
		// for {} with no exit terminates (infinite loop).
		if s.Cond == nil && s.Init == nil && s.Post == nil {
			if !loopMayExit(s.Body) {
				return true
			}
		}
	}
	return false
}

// loopMayExit reports whether the body of an infinite for loop may
// transfer control past the end of the loop, via a break or a goto
// whose target lies outside the loop body. Code following such a loop
// is reachable and must not be reported as unreachable.
//
// A break exits the loop when it is unlabeled and not enclosed in a
// nested for/switch/select, or when it is labeled and the label is not
// defined inside the loop body: label names are unique within a
// function, so such a label must name this loop itself or an enclosing
// statement, and the break abandons the loop either way. A goto exits
// the loop when its target label is not defined inside the loop body.
// Labels inside nested function literals are ignored: a goto inside a
// function literal cannot name a label outside it, and a goto outside
// one cannot jump into it.
func loopMayExit(body *ast.BlockStmt) bool {
	if body == nil {
		return false
	}
	inner := map[string]struct{}{}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.LabeledStmt:
			inner[n.Label.Name] = struct{}{}
		}
		return true
	})
	exits := false
	// breakable counts the for/range/switch/select statements enclosing
	// the current statement relative to the analyzed loop.
	var walk func(list []ast.Stmt, breakable int)
	walk = func(list []ast.Stmt, breakable int) {
		for _, s := range list {
			if exits {
				return
			}
			switch s := s.(type) {
			case *ast.BranchStmt:
				switch s.Tok {
				case token.BREAK:
					if s.Label == nil {
						if breakable == 0 {
							exits = true
						}
					} else if _, ok := inner[s.Label.Name]; !ok {
						exits = true
					}
				case token.GOTO:
					if s.Label != nil {
						if _, ok := inner[s.Label.Name]; !ok {
							exits = true
						}
					}
				}
			case *ast.ForStmt:
				if s.Body != nil {
					walk(s.Body.List, breakable+1)
				}
			case *ast.RangeStmt:
				if s.Body != nil {
					walk(s.Body.List, breakable+1)
				}
			case *ast.SwitchStmt:
				if s.Body != nil {
					for _, c := range s.Body.List {
						if cc, ok := c.(*ast.CaseClause); ok {
							walk(cc.Body, breakable+1)
						}
					}
				}
			case *ast.TypeSwitchStmt:
				if s.Body != nil {
					for _, c := range s.Body.List {
						if cc, ok := c.(*ast.CaseClause); ok {
							walk(cc.Body, breakable+1)
						}
					}
				}
			case *ast.SelectStmt:
				if s.Body != nil {
					for _, c := range s.Body.List {
						if cc, ok := c.(*ast.CommClause); ok {
							walk(cc.Body, breakable+1)
						}
					}
				}
			case *ast.BlockStmt:
				walk(s.List, breakable)
			case *ast.IfStmt:
				if s.Body != nil {
					walk(s.Body.List, breakable)
				}
				if s.Else != nil {
					switch e := s.Else.(type) {
					case *ast.BlockStmt:
						walk(e.List, breakable)
					case *ast.IfStmt:
						walk([]ast.Stmt{e}, breakable)
					}
				}
			case *ast.LabeledStmt:
				walk([]ast.Stmt{s.Stmt}, breakable)
			}
		}
	}
	walk(body.List, 0)
	return exits
}
