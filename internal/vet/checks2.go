package vet

import (
	"go/ast"
	"go/token"
	"go/types"
)

func init() {
	register(&Analyzer{
		Name: "ifaceassert",
		Doc:  "detect impossible interface-to-interface type assertions",
		Run:  runIfaceAssert,
	})
	register(&Analyzer{
		Name: "waitgroup",
		Doc:  "check for misuses of sync.WaitGroup",
		Run:  runWaitGroup,
	})
	register(&Analyzer{
		Name: "httpresponse",
		Doc:  "check for mistakes using HTTP responses",
		Run:  runHTTPResponse,
	})
	register(&Analyzer{
		Name: "defers",
		Doc:  "report common mistakes in defer statements",
		Run:  runDefers,
	})
	register(&Analyzer{
		Name: "loopclosure",
		Doc:  "check references to loop variables from within nested functions",
		Run:  runLoopClosure,
	})
}

// ifaceassert detects impossible type assertions: x.(T) where the static
// type of x cannot possibly implement T (interface-to-interface or
// interface-to-concrete).
func runIfaceAssert(pass *Pass) []Diagnostic {
	var out []Diagnostic
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			ta, ok := n.(*ast.TypeAssertExpr)
			if !ok || ta.Type == nil {
				return true
			}
			// Get the type of x.
			xtv, ok := pass.Info.Types[ta.X]
			if !ok || xtv.Type == nil {
				return true
			}
			// Get the asserted type T.
			ttv, ok := pass.Info.Types[ta.Type]
			if !ok || ttv.Type == nil || !ttv.IsType() {
				return true
			}
			xType := xtv.Type
			tType := ttv.Type
			// Only check interface-to-X assertions.
			if _, ok := xType.Underlying().(*types.Interface); !ok {
				return true
			}
			// Check if x's type could implement T.
			if !couldImplement(xType, tType) {
				out = append(out, Diagnostic{
					pass.position(ta),
					"impossible type assertion: " + xType.String() + " does not implement " + tType.String(),
				})
			}
			return true
		})
	}
	return out
}

// couldImplement reports whether a value of type x could possibly have
// dynamic type implementing t.
func couldImplement(x, t types.Type) bool {
	// If t is an interface, check if x's method set could satisfy it.
	// A value of interface type x could have any dynamic type implementing x,
	// so we check if there EXISTS a type implementing both.
	// Conservative: if x is interface and t is interface, check method sets.
	xIface, xIsIface := x.Underlying().(*types.Interface)
	tIface, tIsIface := t.Underlying().(*types.Interface)
	if xIsIface && tIsIface {
		// If every method of t is in x, then any x dynamic type implements t?
		// No — that's backwards. We need: exists a type implementing both.
		// If t's methods are a subset of x's, then an x value might implement t.
		// Otherwise, impossible only if method sets are incompatible.
		return methodsCompatible(xIface, tIface)
	}
	if xIsIface && !tIsIface {
		// x is interface, t is concrete: possible iff t implements x.
		return types.Implements(t, xIface)
	}
	return true
}

func methodsCompatible(x, t *types.Interface) bool {
	// Check that no method in t conflicts with a method in x
	// (same name, different signature).
	for i := 0; i < t.NumMethods(); i++ {
		tm := t.Method(i)
		var xm *types.Func
		for j := 0; j < x.NumMethods(); j++ {
			if x.Method(j).Name() == tm.Name() {
				xm = x.Method(j)
				break
			}
		}
		if xm == nil {
			continue
		}
		// Same name; check signatures are identical.
		tSig, ok1 := tm.Type().(*types.Signature)
		xSig, ok2 := xm.Type().(*types.Signature)
		if !ok1 || !ok2 {
			continue
		}
		if !types.Identical(tSig, xSig) {
			return false
		}
	}
	return true
}

// waitgroup checks for misuses of sync.WaitGroup:
// - wg.Add called from inside a new goroutine (race with wg.Wait)
func runWaitGroup(pass *Pass) []Diagnostic {
	var out []Diagnostic
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			goStmt, ok := n.(*ast.GoStmt)
			if !ok {
				return true
			}
			// Look for wg.Add calls inside the go statement.
			ast.Inspect(goStmt.Call, func(m ast.Node) bool {
				call, ok := m.(*ast.CallExpr)
				if !ok {
					return true
				}
				if isWaitGroupAdd(pass, call) {
					out = append(out, Diagnostic{
						pass.position(call),
						"WaitGroup.Add called from inside new goroutine",
					})
				}
				return true
			})
			return true
		})
	}
	return out
}

func isWaitGroupAdd(pass *Pass, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Add" {
		return false
	}
	tv, ok := pass.Info.Types[sel.X]
	if !ok || tv.Type == nil {
		return false
	}
	t := tv.Type
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return false
	}
	return named.Obj().Pkg().Path() == "sync" && named.Obj().Name() == "WaitGroup"
}

// httpresponse checks for mistakes using http.ResponseWriter:
// - Header().Set/Add/Del called after WriteHeader or Write (headers are ignored)
func runHTTPResponse(pass *Pass) []Diagnostic {
	var out []Diagnostic
	for _, f := range pass.Files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			// Find http.ResponseWriter parameters.
			var rwNames []string
			if fn.Type.Params != nil {
				for _, param := range fn.Type.Params.List {
					if isResponseWriter(pass, param.Type) {
						for _, name := range param.Names {
							rwNames = append(rwNames, name.Name)
						}
					}
				}
			}
			if len(rwNames) == 0 {
				continue
			}
			out = append(out, checkHTTPResponseInFunc(pass, fn.Body, rwNames)...)
		}
	}
	return out
}

func isResponseWriter(pass *Pass, e ast.Expr) bool {
	tv, ok := pass.Info.Types[e]
	if !ok || tv.Type == nil {
		return false
	}
	t := tv.Type
	named, ok := t.(*types.Named)
	if !ok {
		// Check if it's an interface with the right name.
		if iface, ok := t.Underlying().(*types.Interface); ok {
			_ = iface
		}
		return false
	}
	obj := named.Obj()
	return obj != nil && obj.Pkg() != nil &&
		obj.Pkg().Path() == "net/http" && obj.Name() == "ResponseWriter"
}

func checkHTTPResponseInFunc(pass *Pass, body *ast.BlockStmt, rwNames []string) []Diagnostic {
	var out []Diagnostic
	isRW := func(e ast.Expr) bool {
		if id, ok := e.(*ast.Ident); ok {
			for _, n := range rwNames {
				if id.Name == n {
					return true
				}
			}
		}
		return false
	}

	// Track whether we've seen WriteHeader/Write on each writer.
	// Simple flow-insensitive: if WriteHeader/Write appears before a
	// Header().Set in source order, flag it.
	type event struct {
		pos    token.Position
		writer string
		kind   string // "write" or "headerSet"
	}
	var events []event

	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		// w.WriteHeader(...) or w.Write(...)
		if (sel.Sel.Name == "WriteHeader" || sel.Sel.Name == "Write") && isRW(sel.X) {
			if id, ok := sel.X.(*ast.Ident); ok {
				events = append(events, event{pass.position(call), id.Name, "write"})
			}
			return true
		}
		// w.Header().Set/Add/Del(...)
		if sel.Sel.Name == "Set" || sel.Sel.Name == "Add" || sel.Sel.Name == "Del" {
			if inner, ok := sel.X.(*ast.CallExpr); ok {
				if innerSel, ok := inner.Fun.(*ast.SelectorExpr); ok &&
					innerSel.Sel.Name == "Header" && isRW(innerSel.X) {
					if id, ok := innerSel.X.(*ast.Ident); ok {
						events = append(events, event{pass.position(call), id.Name, "headerSet"})
					}
				}
			}
		}
		return true
	})

	// Flag headerSet events that occur after a write event for the same writer.
	wrote := make(map[string]bool)
	for _, e := range events {
		if e.kind == "write" {
			wrote[e.writer] = true
		} else if e.kind == "headerSet" && wrote[e.writer] {
			out = append(out, Diagnostic{
				e.pos,
				"header Set after WriteHeader/Write: headers will be ignored",
			})
		}
	}
	return out
}

// defers reports common mistakes in defer statements:
// - defer inside a loop (defers accumulate until function return)
func runDefers(pass *Pass) []Diagnostic {
	var out []Diagnostic
	for _, f := range pass.Files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				// Find loops.
				var loop ast.Node
				switch n := n.(type) {
				case *ast.ForStmt:
					loop = n
				case *ast.RangeStmt:
					loop = n
				}
				if loop == nil {
					return true
				}
				// Check for defer directly in loop body (not in a func literal).
				var body *ast.BlockStmt
				switch l := loop.(type) {
				case *ast.ForStmt:
					body = l.Body
				case *ast.RangeStmt:
					body = l.Body
				}
				for _, stmt := range body.List {
					if ds, ok := stmt.(*ast.DeferStmt); ok {
						out = append(out, Diagnostic{
							pass.position(ds),
							"defer in a loop: defers accumulate until the function returns",
						})
					}
				}
				return true
			})
		}
	}
	return out
}

// loopclosure checks for references to loop variables from within nested
// functions (closures) that may outlive the iteration.
// Note: Go 1.22+ gives each iteration its own loop variables, so the classic
// bug is fixed; we still flag explicit cases where the closure escapes.
func runLoopClosure(pass *Pass) []Diagnostic {
	var out []Diagnostic
	for _, f := range pass.Files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				var loopVar *ast.Ident
				var body *ast.BlockStmt
				switch l := n.(type) {
				case *ast.RangeStmt:
					if l.Tok == token.DEFINE {
						// Range variables are per-iteration in Go 1.22+.
						return true
					}
					// For = (assignment to outer var), check closures.
					if l.Key != nil {
						if id, ok := l.Key.(*ast.Ident); ok {
							loopVar = id
						}
					}
					body = l.Body
				case *ast.ForStmt:
					// Check init statement for := .
					if assign, ok := l.Init.(*ast.AssignStmt); ok && assign.Tok == token.DEFINE {
						// Per-iteration in Go 1.22+.
						return true
					}
					body = l.Body
				}
				if loopVar == nil || body == nil {
					return true
				}
				// Look for func literals referencing loopVar.
				ast.Inspect(body, func(m ast.Node) bool {
					lit, ok := m.(*ast.FuncLit)
					if !ok {
						return true
					}
					refs := false
					ast.Inspect(lit.Body, func(p ast.Node) bool {
						if id, ok := p.(*ast.Ident); ok && id.Name == loopVar.Name {
							if obj := pass.Info.Uses[id]; obj != nil {
								if obj == pass.Info.Defs[loopVar] || obj == pass.Info.Uses[loopVar] {
									refs = true
								}
							} else {
								refs = true // no type info; syntactic match
							}
						}
						return true
					})
					if refs {
						out = append(out, Diagnostic{
							pass.position(lit),
							"loop variable " + loopVar.Name + " captured by func literal",
						})
					}
					return true
				})
				return true
			})
		}
	}
	return out
}
