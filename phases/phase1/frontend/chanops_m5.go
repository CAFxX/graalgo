package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
)

// P3-M5 (WS E, ADR-0005 Appendix C): Go channel operations lowering.
//
// P2-M2 implemented channels/select at the Java level only; the Phase-1
// exporter rejected SendStmt/SelectStmt outright ("not supported in
// frontend/v1 scope"). This file lowers Go channel syntax to additive
// wire nodes; phase-3 registers builders via the public
// registerExprKind/registerStmtKind hooks (no frozen NodeFactory
// change). The Phase-2 Java primitives (ChanOps, SelectOp, GoChan) are
// consumed as-is.
//
// Wire nodes (all additive, v1):
//   - "makechan": {elem: type-id, size?: expr, type: chan-type-id} —
//     expression, from make(chan T) / make(chan T, n). Emitted INSTEAD OF
//     the generic make call node: the frozen builtin make path rejects
//     chan types, so we never send it one.
//   - "send": {chan, value} — statement, from ch <- v (SendStmt).
//   - "recv": {x, type: elem-type-id, commaOk: bool} — expression, from
//     <-ch. commaOk is set for the v, ok := <-ch shape (the
//     index-commaOk precedent); "type" is always the element type.
//   - "chanclose": {x} — statement, from close(ch). The builtin registry
//     (freeze §4) is untouched: close is intercepted by ident in the
//     exporter (a user-declared `close` shadows the builtin and is not
//     intercepted).
//   - "select": {cases} — statement, from select. Cases are "selectcase"
//     nodes: {kind: "recv", chan, targets?: [obj ids], body},
//     {kind: "send", chan, value, body}, {kind: "default", body}.
//     Channel/value expressions are case sub-nodes (the Java builder
//     evaluates them once up front, gc semantics); recv values are
//     assigned to the target slots via selectRecvValue/selectRecvOk.
//     v := <-ch introduces case-scoped variables.
//
// `for v := range ch` desugars to a for+select loop (exportRangeChan):
// the v1 "range" node has no channel support.

// isCloseCall reports whether x is a call to the builtin `close`.
func isCloseCall(info *types.Info, x ast.Expr) (*ast.CallExpr, bool) {
	c, ok := x.(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	id, ok := c.Fun.(*ast.Ident)
	if !ok {
		return nil, false
	}
	b, ok := info.Uses[id].(*types.Builtin)
	if !ok || b.Name() != "close" {
		return nil, false
	}
	return c, true
}

// exportSend lowers `ch <- v` (SendStmt) to the "send" statement node.
func (e *exporter) exportSend(s *ast.SendStmt) (*Node, error) {
	ch, err := e.exportExpr(s.Chan)
	if err != nil {
		return nil, err
	}
	v, err := e.exportExpr(s.Value)
	if err != nil {
		return nil, err
	}
	return e.node("send", s, map[string]any{"chan": ch, "value": v}), nil
}

// exportChanCloseCall lowers close(ch) to the "chanclose" statement node.
func (e *exporter) exportChanCloseCall(c *ast.CallExpr) (*Node, error) {
	if len(c.Args) != 1 {
		return nil, scopeErrorf("close takes 1 argument")
	}
	x, err := e.exportExpr(c.Args[0])
	if err != nil {
		return nil, err
	}
	return e.node("chanclose", c, map[string]any{"x": x}), nil
}

// exportCloseDeferGo desugars `defer close(ch)` / `go close(ch)` through
// a synthesized funclit: the channel is evaluated at defer/go time (Go
// semantics) and the "chanclose" statement node runs in the funclit body
// at return/spawn time. (The defer/gostmt wire nodes carry call nodes;
// a bare "chanclose" statement node is not a call.)
func (e *exporter) exportCloseDeferGo(s ast.Stmt, c *ast.CallExpr, isDefer bool) (*Node, error) {
	if len(c.Args) != 1 {
		return nil, scopeErrorf("close takes 1 argument")
	}
	var cht types.Type
	if tv, ok := e.info.Types[c.Args[0]]; ok && tv.Type != nil {
		cht = tv.Type
	}
	if cht == nil {
		return nil, fmt.Errorf("gofrontend: no type info for close argument")
	}
	e.chanSeq++
	pname := fmt.Sprintf("__close_arg_%d", e.chanSeq)
	pv := types.NewParam(token.NoPos, nil, pname, cht)
	pid, err := e.defVar(pv, true)
	if err != nil {
		return nil, err
	}
	ptid, err := e.typeID(cht)
	if err != nil {
		return nil, err
	}
	param := e.node("param", c, map[string]any{"name": pname, "obj": pid, "type": ptid})
	argIdent := e.node("ident", c, map[string]any{"name": pname, "obj": pid, "type": ptid})
	closeNode := e.node("chanclose", c, map[string]any{"x": argIdent})
	body := e.node("block", c, map[string]any{"stmts": []*Node{closeNode}})
	funclit := e.node("funclit", c, map[string]any{
		"params": []*Node{param}, "results": []*Node{}, "body": body,
	})
	if isDefer {
		// Deferred call args are evaluated at defer time (Go semantics).
		argx, err := e.exportExpr(c.Args[0])
		if err != nil {
			return nil, err
		}
		call := e.node("call", c, map[string]any{"fun": funclit, "args": []*Node{argx}, "type": nil})
		return e.node("defer", s, map[string]any{"call": call}), nil
	}
	// `go close(ch)`: a plain "gostmt" call does not evaluate args in the
	// spawning goroutine (cf. exportSyncGoStmt), so the channel is hoisted
	// into a temp first — Go evaluates the function value and parameters
	// in the calling goroutine.
	e.chanSeq++
	tname := fmt.Sprintf("__close_go_%d", e.chanSeq)
	tv := types.NewVar(token.NoPos, nil, tname, cht)
	tid, err := e.defVar(tv, false)
	if err != nil {
		return nil, err
	}
	argx, err := e.exportExpr(c.Args[0])
	if err != nil {
		return nil, err
	}
	hoist := e.node("vardecl", s, map[string]any{
		"names": []string{tname}, "objs": []int{tid},
		"define": true, "type": nil, "values": []*Node{argx},
	})
	tident := e.node("ident", c, map[string]any{"name": tname, "obj": tid, "type": ptid})
	call := e.node("call", c, map[string]any{"fun": funclit, "args": []*Node{tident}, "type": nil})
	spawn := e.node("gostmt", s, map[string]any{"call": call})
	return e.node("block", s, map[string]any{"stmts": []*Node{hoist, spawn}}), nil
}

// exportMakeChan lowers make(chan T) / make(chan T, n) to the "makechan"
// expression node (ADR-0005 Appendix C).
func (e *exporter) exportMakeChan(c *ast.CallExpr, ct *types.Chan) (*Node, error) {
	if len(c.Args) < 1 || len(c.Args) > 2 {
		return nil, scopeErrorf("make(chan) takes 1 or 2 arguments")
	}
	elemTid, err := e.typeID(ct.Elem())
	if err != nil {
		return nil, err
	}
	typ, err := e.callType(c)
	if err != nil {
		return nil, err
	}
	fields := map[string]any{"elem": elemTid, "type": typ}
	if len(c.Args) == 2 {
		size, err := e.exportExpr(c.Args[1])
		if err != nil {
			return nil, err
		}
		fields["size"] = size
	}
	return e.node("makechan", c, fields), nil
}

// exportRecv lowers <-ch to the "recv" expression node (ADR-0005
// Appendix C). commaOk is set for the v, ok := <-ch shape (via
// exportCommaOk); "type" is always the element type (the index-commaOk
// precedent — go/types records a *types.Tuple for the two-valued form,
// so the element type comes from the channel).
func (e *exporter) exportRecv(x *ast.UnaryExpr, commaOk bool) (*Node, error) {
	o, err := e.exportExpr(x.X)
	if err != nil {
		return nil, err
	}
	var t int
	if commaOk {
		el, err := chanElemType(e.info, x.X)
		if err != nil {
			return nil, err
		}
		t, err = e.typeID(el)
		if err != nil {
			return nil, err
		}
	} else {
		t, err = e.exprType(x)
		if err != nil {
			return nil, err
		}
	}
	return e.node("recv", x, map[string]any{"x": o, "type": t, "commaOk": commaOk}), nil
}

// chanElemType returns the element type of the channel denoted by the
// type-checked expression x.
func chanElemType(info *types.Info, x ast.Expr) (types.Type, error) {
	tv, ok := info.Types[x]
	if !ok || tv.Type == nil {
		return nil, fmt.Errorf("gofrontend: no type info for channel expression")
	}
	ct, ok := tv.Type.Underlying().(*types.Chan)
	if !ok {
		return nil, fmt.Errorf("gofrontend: recv on non-channel type %v", tv.Type)
	}
	return ct.Elem(), nil
}

// exportSelect lowers a select statement to the "select" statement node
// with "selectcase" cases (ADR-0005 Appendix C).
func (e *exporter) exportSelect(s *ast.SelectStmt) (*Node, error) {
	cases := make([]*Node, 0, len(s.Body.List))
	for _, stmt := range s.Body.List {
		cc, ok := stmt.(*ast.CommClause)
		if !ok {
			return nil, fmt.Errorf("gofrontend: select body holds %T", stmt)
		}
		var fields map[string]any
		if cc.Comm == nil {
			fields = map[string]any{"kind": "default"}
		} else {
			switch comm := cc.Comm.(type) {
			case *ast.SendStmt:
				ch, err := e.exportExpr(comm.Chan)
				if err != nil {
					return nil, err
				}
				v, err := e.exportExpr(comm.Value)
				if err != nil {
					return nil, err
				}
				fields = map[string]any{"kind": "send", "chan": ch, "value": v}
			case *ast.ExprStmt:
				// case <-ch:
				ch, err := e.selectRecvChan(comm.X)
				if err != nil {
					return nil, err
				}
				fields = map[string]any{"kind": "recv", "chan": ch}
			case *ast.AssignStmt:
				// case v := <-ch: / case v, ok := <-ch: /
				// case v = <-ch: (targets defined before the body so
				// the body resolves them).
				ch, targets, err := e.selectRecvTargets(comm)
				if err != nil {
					return nil, err
				}
				fields = map[string]any{"kind": "recv", "chan": ch}
				if len(targets) > 0 {
					fields["targets"] = targets
				}
			default:
				return nil, fmt.Errorf("gofrontend: select case comm is %T", cc.Comm)
			}
		}
		body, err := e.exportStmtList(cc.Body, cc)
		if err != nil {
			return nil, err
		}
		fields["body"] = body
		cases = append(cases, e.node("selectcase", cc, fields))
	}
	return e.node("select", s, map[string]any{"cases": cases}), nil
}

// selectRecvChan exports the channel operand of a `case <-ch:` clause.
func (e *exporter) selectRecvChan(x ast.Expr) (*Node, error) {
	ux, ok := unparen(x).(*ast.UnaryExpr)
	if !ok || ux.Op != token.ARROW {
		return nil, scopeErrorf("select recv case is %T, want <-ch", x)
	}
	return e.exportExpr(ux.X)
}

// selectRecvTargets exports the channel operand of a `case v := <-ch:`
// (or `case v, ok := <-ch:` / `case v = <-ch:`) clause and defines the
// case-scoped target variables, returning the channel node and the
// target object ids (parallel to the lhs).
func (e *exporter) selectRecvTargets(comm *ast.AssignStmt) (*Node, []int, error) {
	if len(comm.Rhs) != 1 {
		return nil, nil, scopeErrorf("select recv case with %d rhs is not supported in frontend/v1 scope (WS D)", len(comm.Rhs))
	}
	ux, ok := unparen(comm.Rhs[0]).(*ast.UnaryExpr)
	if !ok || ux.Op != token.ARROW {
		return nil, nil, scopeErrorf("select recv case rhs is %T, want <-ch", comm.Rhs[0])
	}
	ch, err := e.exportExpr(ux.X)
	if err != nil {
		return nil, nil, err
	}
	// Element type (for blank temps).
	elem, err := chanElemType(e.info, ux.X)
	if err != nil {
		return nil, nil, err
	}
	targets := make([]int, 0, len(comm.Lhs))
	for _, lhs := range comm.Lhs {
		id, ok := lhs.(*ast.Ident)
		if !ok {
			return nil, nil, scopeErrorf("select recv case with non-identifier target %T is not supported in frontend/v1 scope (WS D)", lhs)
		}
		if id.Name == "_" {
			// Blank: assign to a synthetic temp so target positions
			// stay stable for the Java builder.
			e.chanSeq++
			v := types.NewVar(token.NoPos, nil, fmt.Sprintf("__select_blank_%d", e.chanSeq), elem)
			oid, err := e.defVar(v, false)
			if err != nil {
				return nil, nil, err
			}
			targets = append(targets, oid)
			continue
		}
		v, ok := e.info.ObjectOf(id).(*types.Var)
		if !ok {
			return nil, nil, fmt.Errorf("gofrontend: select target %q does not denote a variable", id.Name)
		}
		var oid int
		if comm.Tok == token.DEFINE {
			oid, err = e.defVar(v, false)
		} else {
			var ok bool
			oid, ok = e.objIDs[v]
			if !ok {
				return nil, nil, fmt.Errorf("gofrontend: select target %q used before declaration", id.Name)
			}
		}
		if err != nil {
			return nil, nil, err
		}
		targets = append(targets, oid)
	}
	return ch, targets, nil
}

// exportRangeChan desugars `for v := range ch` (ADR-0005 Appendix C) to
// a for+select loop over existing wire nodes plus the new "select":
//
//	__rng_ch_N := <chanexpr>
//	__rng_N: for {
//	    select {
//	    case __rng_v_N, __rng_ok_N := <-__rng_ch_N:
//	        if !__rng_ok_N { break __rng_N }
//	        v := __rng_v_N      // := form (v = __rng_v_N for =; dropped if absent)
//	        <original body>
//	    }
//	}
//
// The v1 "range" node has no channel support. gc semantics: the channel
// expression is evaluated once (hoisted); receives block until a value
// is ready or the channel is closed and drained (ok=false terminates the
// loop). The v := __rng_v_N vardecl is an ordinary loop-body vardecl, so
// the factory's loopcap pre-scan gives it per-iteration box semantics
// when captured by a closure (like any loop-body vardecl).
func (e *exporter) exportRangeChan(s *ast.RangeStmt) (*Node, error) {
	tv, ok := e.info.Types[s.X]
	if !ok || tv.Type == nil {
		return nil, fmt.Errorf("gofrontend: no type info for range channel expression")
	}
	ct, ok := tv.Type.Underlying().(*types.Chan)
	if !ok {
		return nil, fmt.Errorf("gofrontend: range over non-channel type %v", tv.Type)
	}
	elem := ct.Elem()
	// go/ast stores a single range variable in Key (Value is nil);
	// `for k, v := range ch` is rejected by go/types.
	if s.Value != nil {
		return nil, scopeErrorf("range over channel with two iteration variables is not supported in frontend/v1 scope")
	}
	keyVar := s.Key
	e.chanSeq++
	seq := e.chanSeq
	label := fmt.Sprintf("__rng_%d", seq)

	newTemp := func(tag string, typ types.Type) (string, int, int, error) {
		name := fmt.Sprintf("__rng_%s_%d", tag, seq)
		v := types.NewVar(token.NoPos, nil, name, typ)
		oid, err := e.defVar(v, false)
		if err != nil {
			return "", 0, 0, err
		}
		tid, err := e.typeID(typ)
		if err != nil {
			return "", 0, 0, err
		}
		return name, oid, tid, nil
	}
	chName, chID, chTid, err := newTemp("ch", tv.Type)
	if err != nil {
		return nil, err
	}
	vName, vID, vTid, err := newTemp("v", elem)
	if err != nil {
		return nil, err
	}
	okName, okID, okTid, err := newTemp("ok", types.Typ[types.Bool])
	if err != nil {
		return nil, err
	}
	ident := func(name string, oid, tid int) *Node {
		return e.node("ident", s, map[string]any{"name": name, "obj": oid, "type": tid})
	}

	// __rng_ch_N := <chanexpr> (evaluated once, gc semantics).
	chanX, err := e.exportExpr(s.X)
	if err != nil {
		return nil, err
	}
	hoist := e.node("vardecl", s, map[string]any{
		"names": []string{chName}, "objs": []int{chID},
		"define": true, "type": nil, "values": []*Node{chanX},
	})

	// if !__rng_ok_N { break __rng_N }
	notOk := e.node("unary", s, map[string]any{
		"op": "!", "x": ident(okName, okID, okTid), "type": okTid,
	})
	brk := e.node("branch", s, map[string]any{"op": "break", "label": label})
	ifNode := e.node("if", s, map[string]any{
		"init": nil, "cond": notOk,
		"then": e.node("block", s, map[string]any{"stmts": []*Node{brk}}),
		"else": nil,
	})

	stmts := []*Node{ifNode}
	if keyVar != nil {
		vid, ok := keyVar.(*ast.Ident)
		if !ok {
			return nil, scopeErrorf("range over channel with non-identifier variable %T is not supported in frontend/v1 scope", keyVar)
		}
		if vid.Name != "_" {
			vVal := ident(vName, vID, vTid)
			if s.Tok == token.DEFINE {
				vv, ok := e.info.ObjectOf(vid).(*types.Var)
				if !ok {
					return nil, fmt.Errorf("gofrontend: range variable %q does not denote a variable", vid.Name)
				}
				oid, err := e.defVar(vv, false)
				if err != nil {
					return nil, err
				}
				stmts = append(stmts, e.node("vardecl", s, map[string]any{
					"names": []string{vid.Name}, "objs": []int{oid},
					"define": true, "type": nil, "values": []*Node{vVal},
				}))
			} else {
				lhs, err := e.exportAssignable(keyVar)
				if err != nil {
					return nil, err
				}
				stmts = append(stmts, e.node("assign", s, map[string]any{
					"tok": "=", "lhs": []*Node{lhs}, "rhs": []*Node{vVal},
				}))
			}
		}
	}
	for _, bst := range s.Body.List {
		st, err := e.exportStmt(bst)
		if err != nil {
			return nil, err
		}
		if st == nil {
			continue // EmptyStmt: normalized away
		}
		stmts = append(stmts, st)
	}
	recvCase := e.node("selectcase", s, map[string]any{
		"kind":    "recv",
		"chan":    ident(chName, chID, chTid),
		"targets": []int{vID, okID},
		"body":    e.node("block", s, map[string]any{"stmts": stmts}),
	})
	sel := e.node("select", s, map[string]any{"cases": []*Node{recvCase}})
	loop := e.node("for", s, map[string]any{
		"init": nil, "cond": nil, "post": nil,
		"body": e.node("block", s, map[string]any{"stmts": []*Node{sel}}),
	})
	labeled := e.node("labeled", s, map[string]any{"label": label, "stmt": loop})
	return e.node("block", s, map[string]any{"stmts": []*Node{hoist, labeled}}), nil
}
