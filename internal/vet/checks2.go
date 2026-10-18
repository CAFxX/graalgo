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

// httpresponse checks for a common mistake using http.ResponseWriter:
// deferring resp.Body.Close() before checking the error. If the HTTP call
// fails, resp may be nil, causing a nil dereference when the deferred
// Close runs.
//
//		resp, err := http.Get(url)
//		defer resp.Body.Close() // BAD: before error check
//		if err != nil {
//			return err
//		}
func runHTTPResponse(pass *Pass) []Diagnostic {
	// Fast path: skip if net/http is not imported.
	hasHTTP := false
	for _, f := range pass.Files {
		for _, imp := range f.Imports {
			if importPath(imp) == "net/http" {
				hasHTTP = true
				break
			}
		}
	}
	if !hasHTTP {
		return nil
	}
	var out []Diagnostic
	for _, f := range pass.Files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			out = append(out, checkHTTPResponseInFunc(pass, fn.Body)...)
		}
	}
	return out
}

// isHTTPClientCall reports whether call is http.Get/Post/Head or
// (http.Client).Get/Post/Head etc.
func isHTTPClientCall(pass *Pass, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	name := sel.Sel.Name
	if name != "Get" && name != "Post" && name != "PostForm" && name != "Head" {
		return false
	}
	// Check if it's http.Get or client.Get.
	if id, ok := sel.X.(*ast.Ident); ok && id.Name == "http" {
		return true
	}
	// Method on *http.Client.
	tv, ok := pass.Info.Types[sel.X]
	if !ok || tv.Type == nil {
		return false
	}
	t := tv.Type
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	if named, ok := t.(*types.Named); ok && named.Obj() != nil &&
		named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "net/http" &&
		named.Obj().Name() == "Client" {
		return true
	}
	return false
}

func checkHTTPResponseInFunc(pass *Pass, body *ast.BlockStmt) []Diagnostic {
	var out []Diagnostic
	// Walk statements looking for:
	//   resp, err := http.Get(...)
	//   defer resp.Body.Close()
	// with no error check in between. The defer may be wrapped in a func
	// literal (defer func() { resp.Body.Close() }()) and need not be the
	// immediately following statement, as long as no statement in between
	// references the error variable (which we conservatively treat as a
	// possible error check).
	for i := 0; i < len(body.List); i++ {
		assign, ok := body.List[i].(*ast.AssignStmt)
		if !ok || len(assign.Lhs) < 1 || len(assign.Rhs) != 1 {
			continue
		}
		call, ok := assign.Rhs[0].(*ast.CallExpr)
		if !ok || !isHTTPClientCall(pass, call) {
			continue
		}
		respIdent, ok := assign.Lhs[0].(*ast.Ident)
		if !ok {
			continue
		}
		errName := ""
		if len(assign.Lhs) > 1 {
			if errIdent, ok := assign.Lhs[1].(*ast.Ident); ok {
				errName = errIdent.Name
			}
		}
		// Scan forward for the deferred close before any error check.
		for j := i + 1; j < len(body.List); j++ {
			stmt := body.List[j]
			if def, ok := stmt.(*ast.DeferStmt); ok &&
				isRespBodyCloseDefer(def, respIdent.Name, errName) {
				out = append(out, Diagnostic{
					pass.position(def),
					"using " + respIdent.Name + " before checking for errors",
				})
				break
			}
			if errName != "" && errName != "_" && stmtMentionsIdent(stmt, errName) {
				// The error variable is referenced first: the author may
				// have checked it, so do not report.
				break
			}
		}
	}
	// Recurse into nested blocks.
	for _, stmt := range body.List {
		ast.Inspect(stmt, func(n ast.Node) bool {
			if block, ok := n.(*ast.BlockStmt); ok && block != body {
				out = append(out, checkHTTPResponseInFunc(pass, block)...)
				return false
			}
			return true
		})
	}
	return out
}

// isRespBodyCloseDefer reports whether d defers closing resp.Body, either
// directly (defer resp.Body.Close()) or wrapped in a func literal
// (defer func() { resp.Body.Close() }()). A wrapper that references the
// error variable is not a match: it may guard the close on the error, in
// which case deferring before the explicit check is safe.
func isRespBodyCloseDefer(d *ast.DeferStmt, respName, errName string) bool {
	if isCloseCall(d.Call, respName) {
		return true
	}
	lit, ok := d.Call.Fun.(*ast.FuncLit)
	if !ok {
		return false
	}
	foundClose := false
	refsErr := false
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		if ce, ok := n.(*ast.CallExpr); ok && isCloseCall(ce, respName) {
			foundClose = true
		}
		if errName != "" && errName != "_" {
			if id, ok := n.(*ast.Ident); ok && id.Name == errName {
				refsErr = true
			}
		}
		return true
	})
	return foundClose && !refsErr
}

// isCloseCall reports whether call is R.Body.Close() for the given
// response variable name R.
func isCloseCall(call *ast.CallExpr, respName string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Close" {
		return false
	}
	inner, ok := sel.X.(*ast.SelectorExpr)
	if !ok || inner.Sel.Name != "Body" {
		return false
	}
	id, ok := inner.X.(*ast.Ident)
	return ok && id.Name == respName
}

// stmtMentionsIdent reports whether stmt contains any reference to the
// identifier name.
func stmtMentionsIdent(stmt ast.Stmt, name string) bool {
	found := false
	ast.Inspect(stmt, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == name {
			found = true
			return false
		}
		return true
	})
	return found
}

// defers reports a common mistake in defer statements: calling time.Since
// in the deferred call's arguments. Arguments are evaluated immediately when
// the defer statement executes, not when the deferred function runs, so
// `defer fmt.Println(time.Since(start))` prints ~0. The fix is
// `defer func() { fmt.Println(time.Since(start)) }()`.
func runDefers(pass *Pass) []Diagnostic {
	// Fast path: skip if time is not imported.
	hasTime := false
	for _, f := range pass.Files {
		for _, imp := range f.Imports {
			if importPath(imp) == "time" {
				hasTime = true
				break
			}
		}
	}
	if !hasTime {
		return nil
	}
	var out []Diagnostic
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			ds, ok := n.(*ast.DeferStmt)
			if !ok {
				return true
			}
			// Inspect the deferred call for time.Since, but do not
			// descend into func literals (those are properly deferred).
			ast.Inspect(ds.Call, func(m ast.Node) bool {
				if _, ok := m.(*ast.FuncLit); ok {
					return false // prune
				}
				call, ok := m.(*ast.CallExpr)
				if !ok {
					return true
				}
				if pass.isPkgFunc(call, "time", "Since") {
					out = append(out, Diagnostic{
						pass.position(call),
						"call to time.Since is not deferred",
					})
				}
				return true
			})
			return true
		})
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
