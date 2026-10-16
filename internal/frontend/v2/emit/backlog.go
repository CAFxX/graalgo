// Backlog emitters (V2-18): the constructs the coverage matrix used to
// list as "loudly rejected" — EmptyStmt, GoStmt, DeferStmt, SendStmt,
// SwitchStmt, TypeSwitchStmt, LabeledStmt, StarExpr (deref), &x (addrof),
// <-c (recv), FuncLit, TypeAssertExpr.
//
// One function per AST node kind, same as nodes.go. Emitters are faithful:
// they record what the source says (including channels, which the factory
// does not yet execute) and never synthesize code or special-case
// packages. A construct the factory cannot yet build fails there with an
// accurate message, not here.
package emit

import (
	"fmt"
	"go/ast"
	"go/types"
)

// emitSigVars emits one node per named variable in fl (a func type's params
// or results), declaring each object in the object table. Unnamed variables
// carry no object and are skipped, matching emitFuncDecl's convention.
// isParam selects "param" (function parameter) vs "result" node kind.
func (e *Emitter) emitSigVars(fl *ast.FieldList, isParam bool) []any {
	out := []any{}
	if fl == nil {
		return out
	}
	kind := "param"
	if !isParam {
		kind = "result"
	}
	for _, field := range fl.List {
		for _, name := range field.Names {
			pobj := e.info.Info.Defs[name]
			if pobj == nil {
				continue
			}
			pid := e.declareObjectParam(pobj, isParam)
			out = append(out, e.node(kind, name, map[string]any{
				"name": name.Name,
				"obj":  pid,
				"type": e.typeID(pobj.Type()),
			}))
		}
	}
	return out
}

// emitGoDefer emits `go f(...)` / `defer f(...)`: the call node is emitted
// by emitCall (so builtins, method calls, and func values are faithful);
// the statement records only the call.
func (e *Emitter) emitGoDefer(kind string, s ast.Stmt, call *ast.CallExpr) (map[string]any, error) {
	cn, err := e.emitCall(call)
	if err != nil {
		return nil, err
	}
	return e.node(kind, s, map[string]any{"call": cn}), nil
}

// emitSend emits `ch <- v`.
func (e *Emitter) emitSend(st *ast.SendStmt) (map[string]any, error) {
	ch, err := e.emitExpr(st.Chan)
	if err != nil {
		return nil, err
	}
	val, err := e.emitExpr(st.Value)
	if err != nil {
		return nil, err
	}
	return e.node("send", st, map[string]any{
		"chan":  ch,
		"value": val,
	}), nil
}

// emitLabeled emits `L: stmt`.
func (e *Emitter) emitLabeled(st *ast.LabeledStmt) (map[string]any, error) {
	inner, err := e.emitStmt(st.Stmt)
	if err != nil {
		return nil, err
	}
	return e.node("labeled", st, map[string]any{
		"label": st.Label.Name,
		"stmt":  inner,
	}), nil
}

// emitSwitch emits `switch [init;] [tag] { case ...: ... }`. The tag may be
// nil (`switch { case x: ... }`). Each clause is a "caseclause" node with
// its case expressions, a default flag, and its body statements.
func (e *Emitter) emitSwitch(st *ast.SwitchStmt) (map[string]any, error) {
	var init any
	if st.Init != nil {
		in, err := e.emitStmt(st.Init)
		if err != nil {
			return nil, err
		}
		init = in
	}
	var tag any
	if st.Tag != nil {
		tn, err := e.emitExpr(st.Tag)
		if err != nil {
			return nil, err
		}
		tag = tn
	}
	clauses := []any{}
	for _, c := range st.Body.List {
		cc, ok := c.(*ast.CaseClause)
		if !ok {
			return nil, fmt.Errorf("emit: switch body contains %T, want *ast.CaseClause", c)
		}
		cases := []any{}
		for _, ce := range cc.List {
			cn, err := e.emitExpr(ce)
			if err != nil {
				return nil, err
			}
			cases = append(cases, cn)
		}
		body, err := e.emitStmtList(cc.Body)
		if err != nil {
			return nil, err
		}
		clauses = append(clauses, map[string]any{
			"kind": "caseclause",
			"pos":  e.pos(cc),
			"fields": map[string]any{
				"cases":   cases,
				"default": len(cc.List) == 0,
				"body":    body,
			},
		})
	}
	return e.node("switch", st, map[string]any{
		"init":    init,
		"tag":     tag,
		"clauses": clauses,
	}), nil
}

// emitStmtList emits a statement list (clause bodies).
func (e *Emitter) emitStmtList(list []ast.Stmt) ([]any, error) {
	out := []any{}
	for _, s := range list {
		sn, err := e.emitStmt(s)
		if err != nil {
			return nil, err
		}
		out = append(out, sn)
	}
	return out, nil
}

// typeSwitchTagExpr returns the tag expression of a type switch: the
// TypeAssertExpr of `switch [v :=] x.(type)`.
func typeSwitchTagExpr(st *ast.TypeSwitchStmt) ast.Expr {
	switch a := st.Assign.(type) {
	case *ast.ExprStmt:
		return a.X
	case *ast.AssignStmt:
		if len(a.Rhs) == 1 {
			return a.Rhs[0]
		}
	}
	return nil
}

// emitTypeSwitch emits `switch [init;] [v :=] x.(type) { case T: ... }`.
// Each clause is a "typecase" node: the clause's type ids, a nilcase flag
// for `case nil`, a default flag, the per-clause guard binding (go/types
// Info.Implicits) if the switch declares one, and the body.
func (e *Emitter) emitTypeSwitch(st *ast.TypeSwitchStmt) (map[string]any, error) {
	var init any
	if st.Init != nil {
		in, err := e.emitStmt(st.Init)
		if err != nil {
			return nil, err
		}
		init = in
	}
	var tag any
	if tagExpr := typeSwitchTagExpr(st); tagExpr != nil {
		tn, err := e.emitExpr(tagExpr)
		if err != nil {
			return nil, err
		}
		tag = tn
	}
	clauses := []any{}
	for _, c := range st.Body.List {
		cc, ok := c.(*ast.CaseClause)
		if !ok {
			return nil, fmt.Errorf("emit: type switch body contains %T, want *ast.CaseClause", c)
		}
		typeIDs := []any{}
		nilCase := false
		for _, te := range cc.List {
			// `case nil`: the nil identifier has no type expression.
			if id, ok := te.(*ast.Ident); ok && id.Name == "nil" {
				nilCase = true
				typeIDs = append(typeIDs, nil)
				continue
			}
			typeIDs = append(typeIDs, e.exprType(te))
		}
		var bindObj, bindType any
		if obj := e.info.Info.Implicits[cc]; obj != nil {
			bindObj = e.declareObject(obj)
			bindType = e.typeID(obj.Type())
		}
		body, err := e.emitStmtList(cc.Body)
		if err != nil {
			return nil, err
		}
		clauses = append(clauses, map[string]any{
			"kind": "typecase",
			"pos":  e.pos(cc),
			"fields": map[string]any{
				"types":    typeIDs,
				"nilcase":  nilCase,
				"default":  len(cc.List) == 0,
				"bindobj":  bindObj,
				"bindtype": bindType,
				"body":     body,
			},
		})
	}
	return e.node("typeswitch", st, map[string]any{
		"init":    init,
		"tag":     tag,
		"clauses": clauses,
	}), nil
}

// emitFuncLit emits a function literal: params and named results (declared
// in the object table like emitFuncDecl's params), the body, and the
// signature type id.
func (e *Emitter) emitFuncLit(l *ast.FuncLit) (map[string]any, error) {
	tv, ok := e.info.Info.Types[l.Type]
	if !ok || tv.Type == nil {
		return nil, fmt.Errorf("emit: no type for funclit")
	}
	if _, ok := tv.Type.(*types.Signature); !ok {
		return nil, fmt.Errorf("emit: funclit type is %T, want *types.Signature", tv.Type)
	}
	params := e.emitSigVars(l.Type.Params, true)
	results := e.emitSigVars(l.Type.Results, false)
	body, err := e.emitBlock(l.Body)
	if err != nil {
		return nil, err
	}
	return e.node("funclit", l, map[string]any{
		"params":  params,
		"results": results,
		"body":    body,
		"type":    e.typeID(tv.Type),
	}), nil
}
