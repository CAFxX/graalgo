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
//   - "recv": {x, type: elem-type-id} — expression, from <-ch. "type"
//     is always the element type. The v, ok := <-ch shape desugars to a
//     single-case, no-default select (ADR-0005 App C, reconciled
//     2026-10-06) — the frozen factory's isCommaOkValue only knows
//     index/typeassert.
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
	// spawning goroutine, so the channel is hoisted
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
	// Zero-arg funclit capturing the hoisted temp (Appendix N pattern).
	// The temp is evaluated in the spawning goroutine (Go semantics).
	tident := e.node("ident", c, map[string]any{"name": tname, "obj": tid, "type": ptid})
	closeNode2 := e.node("chanclose", c, map[string]any{"x": tident})
	body2 := e.node("block", c, map[string]any{"stmts": []*Node{closeNode2}})
	funclit2 := e.node("funclit", c, map[string]any{
		"params": []*Node{}, "results": []*Node{}, "body": body2,
	})
	call := e.node("call", c, map[string]any{"fun": funclit2, "args": []*Node{}, "type": nil})
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
// Appendix C). "type" is always the element type (go/types records a
// *types.Tuple for the two-valued form, so the element type comes from
// the channel). The v, ok := <-ch shape is desugared to a select by
// exportRecvCommaOkDesugar (the frozen factory only knows
// index/typeassert comma-ok).
func (e *exporter) exportRecv(x *ast.UnaryExpr) (*Node, error) {
	o, err := e.exportExpr(x.X)
	if err != nil {
		return nil, err
	}
	t, err := e.exprType(x)
	if err != nil {
		return nil, err
	}
	return e.node("recv", x, map[string]any{"x": o, "type": t}), nil
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
	// P3-M5 (WS E, ADR-0005 App C): the p3chan SelectNode builder
	// requires every select target obj to have a frame slot allocated
	// by a vardecl DOMINATING the select, and rejects BOXED targets
	// outright ("the exporter must desugar through a plain
	// temporary"). A user target is boxed whenever its address is
	// taken anywhere in the function or a loop-captured funclit
	// references it, so a user variable must NEVER be a select
	// target: selectRecvTargets receives into a fresh plain temp per
	// target (declared by the dominating vardecl — a synthetic temp
	// is never address-taken or funclit-captured, hence never
	// boxed) and returns a copy-back (v := temp / v = temp) that is
	// prepended to the case body, mirroring exportRangeChan's
	// v := __rng_v_N pattern (R2313-P1-1).
	var declNames []string
	var declObjs []any
	seenDecl := map[int]bool{}
	declare := func(name string, oid int) {
		if !seenDecl[oid] {
			seenDecl[oid] = true
			declNames = append(declNames, name)
			declObjs = append(declObjs, oid)
		}
	}
	for _, stmt := range s.Body.List {
		cc, ok := stmt.(*ast.CommClause)
		if !ok {
			return nil, fmt.Errorf("gofrontend: select body holds %T", stmt)
		}
		var fields map[string]any
		var copyBack []*Node // R2313-P1-1: temp -> target copy-back, prepended to the case body
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
				ch, targets, cb, err := e.selectRecvTargets(comm, declare)
				if err != nil {
					return nil, err
				}
				copyBack = cb
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
		if len(copyBack) > 0 {
			// R2313-P1-1: the copy-back runs first in the case body,
			// before any user statement reads the target.
			if stmts, ok := body.Fields["stmts"].([]*Node); ok {
				body.Fields["stmts"] = append(append([]*Node{}, copyBack...), stmts...)
			}
		}
		fields["body"] = body
		cases = append(cases, e.node("selectcase", cc, fields))
	}
	sel := e.node("select", s, map[string]any{"cases": cases})
	if len(declObjs) == 0 {
		return sel, nil
	}
	// Dominating zero-value declaration for the := targets.
	vd := e.node("vardecl", s, map[string]any{
		"names": declNames, "objs": declObjs, "define": true,
		"values": []any{},
	})
	return e.node("block", s, map[string]any{"stmts": []*Node{vd, sel}}), nil
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
// case-scoped target variables, returning the channel node, the target
// object ids (parallel to the lhs), and the copy-back statements that
// move each received value from its temp into the real target at the
// top of the case body.
//
// R2313-P1-1: the wire "targets" are NEVER the user variables — they
// are fresh plain temps (`__select_tmp_N`, declared via declare so
// the caller emits the dominating vardecl the p3chan builder
// requires). A user target is boxed whenever its address is taken
// anywhere in the function or a loop-captured funclit references it,
// and SelectNode rejects boxed targets at build time; a synthetic
// temp is never named by user code, so it is never boxed. The
// copy-back is `v := temp` for := targets (mirroring exportRangeChan's
// `v := __rng_v_N`) and `v = temp` for = targets. Blank targets need
// no copy-back: the temp itself discards the value. Non-identifier
// targets are still rejected here (R2313-P1-2, sibling lane); that
// lane can append a `target = temp` assign to the same copyBack list.
func (e *exporter) selectRecvTargets(comm *ast.AssignStmt, declare func(string, int)) (*Node, []int, []*Node, error) {
	if len(comm.Rhs) != 1 {
		return nil, nil, nil, scopeErrorf("select recv case with %d rhs is not supported in frontend/v1 scope (WS D)", len(comm.Rhs))
	}
	ux, ok := unparen(comm.Rhs[0]).(*ast.UnaryExpr)
	if !ok || ux.Op != token.ARROW {
		return nil, nil, nil, scopeErrorf("select recv case rhs is %T, want <-ch", comm.Rhs[0])
	}
	ch, err := e.exportExpr(ux.X)
	if err != nil {
		return nil, nil, nil, err
	}
	// Element type (for blank temps).
	elem, err := chanElemType(e.info, ux.X)
	if err != nil {
		return nil, nil, nil, err
	}
	targets := make([]int, 0, len(comm.Lhs))
	var copyBack []*Node
	for i, lhs := range comm.Lhs {
		id, ok := lhs.(*ast.Ident)
		if !ok {
			return nil, nil, nil, scopeErrorf("select recv case with non-identifier target %T is not supported in frontend/v1 scope (WS D)", lhs)
		}
		if id.Name == "_" {
			// Blank: assign to a synthetic temp so target positions
			// stay stable for the Java builder.
			e.chanSeq++
			name := fmt.Sprintf("__select_blank_%d", e.chanSeq)
			v := types.NewVar(token.NoPos, nil, name, recvTargetType(i, elem))
			oid, err := e.defVar(v, false)
			if err != nil {
				return nil, nil, nil, err
			}
			declare(name, oid)
			targets = append(targets, oid)
			continue
		}
		v, ok := e.info.ObjectOf(id).(*types.Var)
		if !ok {
			return nil, nil, nil, fmt.Errorf("gofrontend: select target %q does not denote a variable", id.Name)
		}
		// Receive into a fresh plain temp, never into the user
		// variable (see the function comment).
		e.chanSeq++
		tmpName := fmt.Sprintf("__select_tmp_%d", e.chanSeq)
		tv := types.NewVar(token.NoPos, nil, tmpName, v.Type())
		tmpOid, err := e.defVar(tv, false)
		if err != nil {
			return nil, nil, nil, err
		}
		declare(tmpName, tmpOid)
		targets = append(targets, tmpOid)
		tmpTid, err := e.typeID(v.Type())
		if err != nil {
			return nil, nil, nil, err
		}
		tmpIdent := e.node("ident", comm, map[string]any{
			"name": tmpName, "obj": tmpOid, "type": tmpTid,
		})
		if comm.Tok == token.DEFINE {
			oid, err := e.defVar(v, false)
			if err != nil {
				return nil, nil, nil, err
			}
			copyBack = append(copyBack, e.node("vardecl", comm, map[string]any{
				"names": []string{id.Name}, "objs": []int{oid},
				"define": true, "type": nil, "values": []*Node{tmpIdent},
			}))
		} else {
			if _, ok := e.objIDs[v]; !ok {
				return nil, nil, nil, fmt.Errorf("gofrontend: select target %q used before declaration", id.Name)
			}
			lhsNode, err := e.exportAssignable(id)
			if err != nil {
				return nil, nil, nil, err
			}
			copyBack = append(copyBack, e.node("assign", comm, map[string]any{
				"tok": "=", "lhs": []*Node{lhsNode}, "rhs": []*Node{tmpIdent},
			}))
		}
	}
	return ch, targets, copyBack, nil
}

// recvTargetType returns the wire type of a select / comma-ok receive
// target at position i: the channel element type for the value
// (i == 0), bool for the ok target (i == 1). A blank ok temp must be
// bool, not elem — the builder rejects a non-bool ok target.
func recvTargetType(i int, elem types.Type) types.Type {
	if i == 1 {
		return types.Typ[types.Bool]
	}
	return elem
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
	// P3-M5 (WS E, ADR-0005 App C): the p3chan SelectNode builder
	// requires the select targets to be declared by a vardecl
	// dominating the select. The temps are zeroed per iteration;
	// the select assigns them on case selection.
	targetDecl := e.node("vardecl", s, map[string]any{
		"names": []string{vName, okName}, "objs": []int{vID, okID},
		"define": true, "values": []any{},
	})
	loop := e.node("for", s, map[string]any{
		"init": nil, "cond": nil, "post": nil,
		"body": e.node("block", s, map[string]any{"stmts": []*Node{targetDecl, sel}}),
	})
	labeled := e.node("labeled", s, map[string]any{"label": label, "stmt": loop})
	return e.node("block", s, map[string]any{"stmts": []*Node{hoist, labeled}}), nil
}

// exportRecvCommaOkDesugar lowers `v, ok := <-ch` (define=true) or
// `v, ok = <-ch` (define=false) to a single-case, no-default select
// (ADR-0005 Appendix C, reconciled 2026-10-06). The frozen factory's
// isCommaOkValue only recognizes index/typeassert, so recv{commaOk:true}
// is not emittable; a blocking single-case select with targets [v, ok]
// is semantically identical to <-ch plus the closed-channel bit.
//
// R2313-P1-1: the select targets are fresh plain temps, never the
// user variables (a user target is boxed whenever its address is
// taken or a loop-captured funclit references it, and SelectNode
// rejects boxed targets); the copy-back (v := temp for :=,
// v = temp for =) runs after the select, mirroring the select
// desugar in selectRecvTargets.
func (e *exporter) exportRecvCommaOkDesugar(s *ast.AssignStmt, define bool) (*Node, error) {
	ux := unparen(s.Rhs[0]).(*ast.UnaryExpr)
	ch, err := e.exportExpr(ux.X)
	if err != nil {
		return nil, err
	}
	// Element type for blank temps.
	elem, err := chanElemType(e.info, ux.X)
	if err != nil {
		return nil, err
	}
	// Resolve the two targets. Both forms desugar through fresh
	// plain temps (ADR-0005 App C): user variables must never be
	// select targets — a boxed target is rejected by the builder
	// (R2313-P1-1) — and for `=` a global target is additionally not
	// in the frame maps. The copy-back (v := temp for :=, v = temp
	// for =) runs after the select.
	var targetNames []string
	var targetObjs []int
	needDecl := true // temps need a dominating vardecl
	var copyBack []*Node
	if !define {
		// = : receive into temps, then copy back with a single
		// multi-target assign (v, ok = tmp0, tmp1).
		e.chanSeq++
		seq := e.chanSeq
		lhs := make([]*Node, 0, 2)
		rhs := make([]*Node, 0, 2)
		for i, l := range s.Lhs {
			id, ok := l.(*ast.Ident)
			if !ok {
				return nil, scopeErrorf("comma-ok recv with non-identifier target %T is not supported in frontend/v1 scope (WS D)", l)
			}
			var typ types.Type
			var userName string
			if id.Name == "_" {
				typ = recvTargetType(i, elem)
				userName = "_"
			} else {
				v := e.info.ObjectOf(id).(*types.Var)
				typ = v.Type()
				userName = id.Name
			}
			name := fmt.Sprintf("__rcv_tmp_%d_%d", seq, i)
			tv := types.NewVar(token.NoPos, nil, name, typ)
			oid, err := e.defVar(tv, false)
			if err != nil {
				return nil, err
			}
			targetNames = append(targetNames, name)
			targetObjs = append(targetObjs, oid)
			if userName != "_" {
				ln, err := e.exportAssignable(l)
				if err != nil {
					return nil, err
				}
				lhs = append(lhs, ln)
				tid, err := e.typeID(typ)
				if err != nil {
					return nil, err
				}
				rhs = append(rhs, e.node("ident", s, map[string]any{
					"name": name, "obj": oid, "type": tid,
				}))
			}
		}
		if len(lhs) > 0 {
			copyBack = append(copyBack, e.node("assign", s, map[string]any{
				"tok": "=", "lhs": lhs, "rhs": rhs,
			}))
		}
	} else {
		// := : receive into temps, then declare the targets from
		// them (v := tmp0, ok := tmp1) after the select.
		e.chanSeq++
		seq := e.chanSeq
		for i, lhs := range s.Lhs {
			id, ok := lhs.(*ast.Ident)
			if !ok {
				return nil, scopeErrorf("comma-ok recv with non-identifier target %T is not supported in frontend/v1 scope (WS D)", lhs)
			}
			if id.Name == "_" {
				name := fmt.Sprintf("__rcv_tmp_%d_%d", seq, i)
				tv := types.NewVar(token.NoPos, nil, name, recvTargetType(i, elem))
				oid, err := e.defVar(tv, false)
				if err != nil {
					return nil, err
				}
				targetNames = append(targetNames, name)
				targetObjs = append(targetObjs, oid)
				continue
			}
			v, ok := e.info.ObjectOf(id).(*types.Var)
			if !ok {
				return nil, fmt.Errorf("gofrontend: comma-ok recv target %q does not denote a variable", id.Name)
			}
			name := fmt.Sprintf("__rcv_tmp_%d_%d", seq, i)
			tv := types.NewVar(token.NoPos, nil, name, v.Type())
			tmpOid, err := e.defVar(tv, false)
			if err != nil {
				return nil, err
			}
			targetNames = append(targetNames, name)
			targetObjs = append(targetObjs, tmpOid)
			tmpTid, err := e.typeID(v.Type())
			if err != nil {
				return nil, err
			}
			oid, err := e.defVar(v, false)
			if err != nil {
				return nil, err
			}
			copyBack = append(copyBack, e.node("vardecl", s, map[string]any{
				"names": []string{id.Name}, "objs": []int{oid},
				"define": true, "type": nil, "values": []*Node{e.node("ident", s, map[string]any{
					"name": name, "obj": tmpOid, "type": tmpTid,
				})},
			}))
		}
	}

	// Build the select.
	declObjs := make([]any, len(targetObjs))
	for i, oid := range targetObjs {
		declObjs[i] = oid
	}
	targetAny := make([]any, len(targetObjs))
	for i, oid := range targetObjs {
		targetAny[i] = oid
	}
	recvCase := e.node("selectcase", s, map[string]any{
		"kind":    "recv",
		"chan":    ch,
		"targets": targetAny,
		"body":    e.node("block", s, map[string]any{"stmts": []*Node{}}),
	})
	sel := e.node("select", s, map[string]any{"cases": []*Node{recvCase}})
	stmts := []*Node{}
	if needDecl {
		vd := e.node("vardecl", s, map[string]any{
			"names": targetNames, "objs": declObjs, "define": true,
			"values": []any{},
		})
		stmts = append(stmts, vd)
	}
	stmts = append(stmts, sel)
	stmts = append(stmts, copyBack...)
	return e.node("block", s, map[string]any{"stmts": stmts}), nil
}

// isCommaOkRecv reports whether s is a 2-target assignment with a
// single RHS that is a channel receive (<-ch).
func isCommaOkRecv(s *ast.AssignStmt) bool {
	if len(s.Lhs) != 2 || len(s.Rhs) != 1 {
		return false
	}
	ux, ok := unparen(s.Rhs[0]).(*ast.UnaryExpr)
	return ok && ux.Op == token.ARROW
}
