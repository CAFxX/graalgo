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
// for loop without exit).
func terminates(s ast.Stmt) bool {
	// A labeled statement terminates iff its inner statement does.
	// Collect every label on it: each names the same statement, so
	// a break naming any of them exits the loop below.
	var labels []string
	for {
		l, ok := s.(*ast.LabeledStmt)
		if !ok {
			break
		}
		labels = append(labels, l.Label.Name)
		s = l.Stmt
	}
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
			if !loopExits(s.Body, labels) {
				return true
			}
		}
	}
	return false
}

// loopExits reports whether the body of an infinite for loop can exit
// the loop, i.e. whether control can reach the statement after the loop.
// That is the case for a break targeting the loop — an unlabeled break
// at this level, or a break labeled with one of loopLabels (the labels
// on the for statement itself), which may sit inside a nested
// for/switch/select — or for a goto whose target label is not defined
// inside the loop body.
//
// A break naming an inner label (e.g. a labeled block inside the body),
// or an outer label other than the loop's own, does not fall through to
// the statement after the loop, so it does not count; neither does a
// goto to a label defined inside the body. The walk mirrors the
// label-aware rule of go/types hasBreak (implicit breaks only count at
// this level; labeled breaks count at any depth when they name the
// loop), extended with the goto rule.
func loopExits(body *ast.BlockStmt, loopLabels []string) bool {
	if body == nil {
		return false
	}
	// Labels defined anywhere inside the body (any depth, except inside
	// a nested func literal, whose labels are scoped to it): a goto to
	// one of them stays inside the loop.
	inBody := make(map[string]bool)
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.LabeledStmt:
			inBody[n.Label.Name] = true
		}
		return true
	})
	found := false
	var walkList func(list []ast.Stmt, nested bool)
	var walkStmt func(s ast.Stmt, nested bool)
	// walkClauses descends into the case/comm clauses of a switch,
	// type switch, or select body.
	walkClauses := func(body *ast.BlockStmt) {
		if body == nil {
			return
		}
		for _, c := range body.List {
			if found {
				return
			}
			walkStmt(c, true)
		}
	}
	walkList = func(list []ast.Stmt, nested bool) {
		for _, s := range list {
			if found {
				return
			}
			walkStmt(s, nested)
		}
	}
	walkStmt = func(s ast.Stmt, nested bool) {
		if found {
			return
		}
		switch s := s.(type) {
		case *ast.BranchStmt:
			switch s.Tok {
			case token.BREAK:
				if s.Label == nil {
					// Unlabeled break targets the innermost
					// enclosing breakable statement; at this
					// level that is the loop itself.
					if !nested {
						found = true
					}
				} else if hasLabel(loopLabels, s.Label.Name) {
					// break L names the loop itself (possibly
					// from inside a nested loop/switch/select).
					found = true
				}
			case token.GOTO:
				if s.Label != nil && !inBody[s.Label.Name] {
					// goto to a label outside the loop body.
					found = true
				}
			}
		case *ast.BlockStmt:
			walkList(s.List, nested)
		case *ast.IfStmt:
			if s.Body != nil {
				walkList(s.Body.List, nested)
			}
			if s.Else != nil {
				walkStmt(s.Else, nested)
			}
		case *ast.LabeledStmt:
			walkStmt(s.Stmt, nested)
		case *ast.ForStmt:
			if s.Body != nil {
				walkList(s.Body.List, true)
			}
		case *ast.RangeStmt:
			if s.Body != nil {
				walkList(s.Body.List, true)
			}
		case *ast.SwitchStmt:
			walkClauses(s.Body)
		case *ast.TypeSwitchStmt:
			walkClauses(s.Body)
		case *ast.SelectStmt:
			walkClauses(s.Body)
		case *ast.CaseClause:
			walkList(s.Body, true)
		case *ast.CommClause:
			walkList(s.Body, true)
		}
		// Anything else (declarations, plain expressions, func
		// literals) cannot hold a break/goto exiting this loop at
		// this level; in particular a branch inside a func literal
		// belongs to that literal, so it is never descended into.
	}
	walkList(body.List, false)
	return found
}

func hasLabel(labels []string, name string) bool {
	for _, l := range labels {
		if l == name {
			return true
		}
	}
	return false
}
