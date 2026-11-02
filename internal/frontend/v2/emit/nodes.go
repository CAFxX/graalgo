// Node emitters: one function per AST node kind.
//
// Each emitter takes the typed AST node and returns its in-memory
// representation: { "kind", "pos", "fields" }. Emitters never synthesize
// code, never special-case packages, and never desugar stdlib calls.
package emit

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
)

// node is the in-memory representation of one AST node.
func (e *Emitter) node(kind string, n ast.Node, fields map[string]any) map[string]any {
	return map[string]any{
		"kind":   kind,
		"pos":    e.pos(n),
		"fields": fields,
	}
}

func (e *Emitter) emitFuncDecl(fd *ast.FuncDecl) (map[string]any, error) {
	obj := e.info.Info.Defs[fd.Name]
	if obj == nil {
		return nil, fmt.Errorf("emit: no object for func %s", fd.Name.Name)
	}
	fn, ok := obj.(*types.Func)
	if !ok {
		return nil, fmt.Errorf("emit: %s is not a func", fd.Name.Name)
	}
	sig := e.typeID(fn.Type())
	sigType, _ := fn.Type().(*types.Signature)

	// Receiver first (source order): a method without a recv field is a
	// plain function, and its body references to the receiver would fail
	// as "not declared in scope". The receiver is declared in the
	// function scope exactly like a parameter, before the body is emitted.
	var recv any
	if fd.Recv != nil && len(fd.Recv.List) > 0 {
		field := fd.Recv.List[0]
		var recvObj *types.Var
		if sigType != nil {
			recvObj = sigType.Recv()
		}
		var name string
		var pid any = -1
		var typ any
		if len(field.Names) > 0 {
			name = field.Names[0].Name
			if pobj := e.info.Info.Defs[field.Names[0]]; pobj != nil {
				if v, ok := pobj.(*types.Var); ok {
					recvObj = v
				}
			}
		}
		if recvObj != nil {
			name = recvObj.Name()
			pid = e.declareParam(recvObj)
			typ = e.typeID(recvObj.Type())
		} else if tv, ok := e.info.Info.Types[field.Type]; ok && tv.Type != nil {
			typ = e.typeID(tv.Type)
		}
		recv = map[string]any{"name": name, "obj": pid, "type": typ}
	}

	var params, results []any
	if fd.Type.Params != nil {
		var tuple *types.Tuple
		if sigType != nil {
			tuple = sigType.Params()
		}
		params = e.emitParamList(fd.Type.Params, tuple)
	} else {
		params = []any{}
	}
	if fd.Type.Results != nil {
		var tuple *types.Tuple
		if sigType != nil {
			tuple = sigType.Results()
		}
		results = e.emitParamList(fd.Type.Results, tuple)
	} else {
		results = []any{}
	}

	var body any
	if fd.Body != nil {
		b, err := e.emitBlock(fd.Body)
		if err != nil {
			return nil, err
		}
		body = b
	}

	pkgpath := ""
	if pkg := fn.Pkg(); pkg != nil {
		pkgpath = pkg.Path()
	}
	fields := map[string]any{
		"name":    fn.Name(),
		"sig":     sig,
		"recv":    recv,
		"params":  params,
		"results": results,
		"body":    body,
		"pkgpath": pkgpath,
	}
	return e.node("funcdecl", fd, fields), nil
}

// emitParamList emits one parameter or result list. Every position in
// the list produces exactly one "param" node, in order — including
// unnamed parameters (func f(int, string)), which the old loop dropped
// by iterating only field.Names while the signature still counted them.
// For unnamed positions the object comes from the go/types tuple (the
// signature's Var, whose name is ""), so the position still gets a slot;
// when no tuple entry exists the node carries obj -1 rather than being
// silently omitted. Named positions resolve through Defs, falling back
// to the tuple entry at the same position.
func (e *Emitter) emitParamList(fl *ast.FieldList, tuple *types.Tuple) []any {
	params := []any{}
	idx := 0
	for _, field := range fl.List {
		if len(field.Names) == 0 {
			var pid any = -1
			var typ any
			if tuple != nil && idx < tuple.Len() {
				v := tuple.At(idx)
				pid = e.declareParam(v)
				typ = e.typeID(v.Type())
			} else if tv, ok := e.info.Info.Types[field.Type]; ok && tv.Type != nil {
				typ = e.typeID(tv.Type)
			}
			params = append(params, e.node("param", field, map[string]any{
				"name": "",
				"obj":  pid,
				"type": typ,
			}))
			idx++
			continue
		}
		for _, name := range field.Names {
			var pid any = -1
			var typ any
			if pobj := e.info.Info.Defs[name]; pobj != nil {
				pid = e.declareParam(pobj)
				typ = e.typeID(pobj.Type())
			} else if tuple != nil && idx < tuple.Len() {
				v := tuple.At(idx)
				pid = e.declareParam(v)
				typ = e.typeID(v.Type())
			} else if tv, ok := e.info.Info.Types[field.Type]; ok && tv.Type != nil {
				typ = e.typeID(tv.Type)
			}
			params = append(params, e.node("param", name, map[string]any{
				"name": name.Name,
				"obj":  pid,
				"type": typ,
			}))
			idx++
		}
	}
	return params
}

func (e *Emitter) emitBlock(b *ast.BlockStmt) (map[string]any, error) {
	stmts := []any{}
	for _, s := range b.List {
		sn, err := e.emitStmt(s)
		if err != nil {
			return nil, err
		}
		stmts = append(stmts, sn)
	}
	return e.node("block", b, map[string]any{"stmts": stmts}), nil
}

func (e *Emitter) emitStmt(s ast.Stmt) (map[string]any, error) {
	switch st := s.(type) {
	case *ast.ExprStmt:
		x, err := e.emitExpr(st.X)
		if err != nil {
			return nil, err
		}
		return e.node("exprstmt", s, map[string]any{"x": x}), nil
	case *ast.ReturnStmt:
		results := []any{}
		for _, r := range st.Results {
			rn, err := e.emitExpr(r)
			if err != nil {
				return nil, err
			}
			results = append(results, rn)
		}
		return e.node("return", s, map[string]any{"results": results}), nil
	case *ast.AssignStmt:
		return e.emitAssign(st)
	case *ast.DeclStmt:
		return e.emitDeclStmt(st)
	case *ast.IfStmt:
		return e.emitIf(st)
	case *ast.ForStmt:
		return e.emitFor(st)
	case *ast.RangeStmt:
		return e.emitRange(st)
	case *ast.IncDecStmt:
		x, err := e.emitExpr(st.X)
		if err != nil {
			return nil, err
		}
		return e.node("incdec", s, map[string]any{
			"tok": st.Tok.String(),
			"x":   x,
		}), nil
	case *ast.BranchStmt:
		// The label is load-bearing: dropping it silently degrades
		// `break outer` / `goto` to an unlabeled branch the moment
		// LabeledStmt is emitted, so it is always carried, nil when
		// the branch is unlabeled.
		var label any
		if st.Label != nil {
			label = st.Label.Name
		}
		return e.node("branch", s, map[string]any{
			"tok":   st.Tok.String(),
			"label": label,
		}), nil
	case *ast.SwitchStmt:
		return e.emitSwitch(st)
	case *ast.TypeSwitchStmt:
		return e.emitTypeSwitch(st)
	case *ast.SelectStmt:
		return e.emitSelect(st)
	case *ast.GoStmt:
		call, err := e.emitCall(st.Call)
		if err != nil {
			return nil, err
		}
		return e.node("go", s, map[string]any{"call": call}), nil
	case *ast.DeferStmt:
		call, err := e.emitCall(st.Call)
		if err != nil {
			return nil, err
		}
		return e.node("defer", s, map[string]any{"call": call}), nil
	case *ast.SendStmt:
		ch, err := e.emitExpr(st.Chan)
		if err != nil {
			return nil, err
		}
		val, err := e.emitExpr(st.Value)
		if err != nil {
			return nil, err
		}
		return e.node("send", s, map[string]any{"chan": ch, "value": val}), nil
	case *ast.LabeledStmt:
		inner, err := e.emitStmt(st.Stmt)
		if err != nil {
			return nil, err
		}
		return e.node("labeled", s, map[string]any{
			"label": st.Label.Name,
			"stmt":  inner,
		}), nil
	default:
		return nil, fmt.Errorf("emit: unsupported stmt %T", s)
	}
}

// emitCaseList emits a switch body's CaseClauses. Each case node carries
// its expressions (empty list = default) and its body statements in
// order; the body is a statement list, not a BlockStmt, so it is emitted
// statement by statement rather than via emitBlock.
func (e *Emitter) emitCaseList(body *ast.BlockStmt, typeCase bool) ([]any, error) {
	cases := []any{}
	for _, stmt := range body.List {
		cc, ok := stmt.(*ast.CaseClause)
		if !ok {
			return nil, fmt.Errorf("emit: unsupported switch body stmt %T", stmt)
		}
		exprs := []any{}
		for _, ex := range cc.List {
			var en map[string]any
			var err error
			if typeCase {
				// Type-switch case expressions are types, not values:
				// emit them as type nodes (a nil list entry is the
				// default case and produces no node here — the empty
				// exprs list marks it).
				en, err = e.emitTypeExpr(ex)
			} else {
				en, err = e.emitExpr(ex)
			}
			if err != nil {
				return nil, err
			}
			exprs = append(exprs, en)
		}
		stmts := []any{}
		for _, s := range cc.Body {
			sn, err := e.emitStmt(s)
			if err != nil {
				return nil, err
			}
			stmts = append(stmts, sn)
		}
		cases = append(cases, e.node("case", cc, map[string]any{
			"exprs": exprs,
			"stmts": stmts,
		}))
	}
	return cases, nil
}

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
	cases, err := e.emitCaseList(st.Body, false)
	if err != nil {
		return nil, err
	}
	return e.node("switch", st, map[string]any{
		"init":  init,
		"tag":   tag,
		"cases": cases,
	}), nil
}

func (e *Emitter) emitTypeSwitch(st *ast.TypeSwitchStmt) (map[string]any, error) {
	var init any
	if st.Init != nil {
		in, err := e.emitStmt(st.Init)
		if err != nil {
			return nil, err
		}
		init = in
	}
	var assign any
	if st.Assign != nil {
		an, err := e.emitTypeSwitchGuard(st.Assign)
		if err != nil {
			return nil, err
		}
		assign = an
	}
	cases, err := e.emitCaseList(st.Body, true)
	if err != nil {
		return nil, err
	}
	return e.node("typeswitch", st, map[string]any{
		"init":   init,
		"assign": assign,
		"cases":  cases,
	}), nil
}

// emitTypeSwitchGuard emits a type-switch guard (v := x.(type) or
// x.(type)) as a "typeswitchguard" node carrying the switched value x.
// The guard variable itself is symbolic and must NOT be emitted as a
// normal assignment: go/types records no Defs/Uses object for it (each
// case clause has its own implicit variable of the case type, visible in
// that clause's scope). Emitting it through emitAssign fails with
// "unresolved ident v" — and even if it did not fail, a single object
// id for v would be wrong, since v has a different type in each case.
// Case bodies reference their clause's implicit variable through Uses,
// which emitIdent resolves to distinct per-case object ids.
func (e *Emitter) emitTypeSwitchGuard(assign ast.Stmt) (map[string]any, error) {
	var assert *ast.TypeAssertExpr
	switch a := assign.(type) {
	case *ast.ExprStmt:
		ta, ok := a.X.(*ast.TypeAssertExpr)
		if !ok {
			return nil, fmt.Errorf("emit: type switch guard is not a type assertion: %T", a.X)
		}
		assert = ta
	case *ast.AssignStmt:
		if len(a.Rhs) != 1 {
			return nil, fmt.Errorf("emit: type switch guard has %d RHS values, want 1", len(a.Rhs))
		}
		ta, ok := a.Rhs[0].(*ast.TypeAssertExpr)
		if !ok {
			return nil, fmt.Errorf("emit: type switch guard RHS is not a type assertion: %T", a.Rhs[0])
		}
		assert = ta
	default:
		return nil, fmt.Errorf("emit: unsupported type switch guard %T", assign)
	}
	xn, err := e.emitExpr(assert.X)
	if err != nil {
		return nil, err
	}
	return e.node("typeswitchguard", assign, map[string]any{"x": xn}), nil
}

func (e *Emitter) emitSelect(st *ast.SelectStmt) (map[string]any, error) {
	cases := []any{}
	for _, stmt := range st.Body.List {
		cc, ok := stmt.(*ast.CommClause)
		if !ok {
			return nil, fmt.Errorf("emit: unsupported select body stmt %T", stmt)
		}
		var comm any
		if cc.Comm != nil {
			cn, err := e.emitStmt(cc.Comm)
			if err != nil {
				return nil, err
			}
			comm = cn
		}
		stmts := []any{}
		for _, s := range cc.Body {
			sn, err := e.emitStmt(s)
			if err != nil {
				return nil, err
			}
			stmts = append(stmts, sn)
		}
		cases = append(cases, e.node("commcase", cc, map[string]any{
			"comm":  comm,
			"stmts": stmts,
		}))
	}
	return e.node("select", st, map[string]any{"cases": cases}), nil
}

func (e *Emitter) emitAssign(st *ast.AssignStmt) (map[string]any, error) {
	lhs := []any{}
	for _, l := range st.Lhs {
		ln, err := e.emitExpr(l)
		if err != nil {
			return nil, err
		}
		lhs = append(lhs, ln)
	}
	rhs := []any{}
	for _, r := range st.Rhs {
		rn, err := e.emitExpr(r)
		if err != nil {
			return nil, err
		}
		rhs = append(rhs, rn)
	}
	// For :=, declare the defined objects.
	if st.Tok == token.DEFINE {
		for _, l := range st.Lhs {
			if id, ok := l.(*ast.Ident); ok && id.Name != "_" {
				if obj := e.info.Info.Defs[id]; obj != nil {
					e.declareObject(obj)
				}
			}
		}
	}
	return e.node("assign", st, map[string]any{
		"tok": st.Tok.String(),
		"lhs": lhs,
		"rhs": rhs,
	}), nil
}

func (e *Emitter) emitDeclStmt(st *ast.DeclStmt) (map[string]any, error) {
	gd, ok := st.Decl.(*ast.GenDecl)
	if !ok || gd.Tok != token.VAR {
		return nil, fmt.Errorf("emit: unsupported decl stmt %T", st.Decl)
	}
	specs := []any{}
	for _, spec := range gd.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			return nil, fmt.Errorf("emit: unsupported decl spec %T", spec)
		}
		fields, err := e.emitValueSpec(vs)
		if err != nil {
			return nil, err
		}
		specs = append(specs, fields)
	}
	return e.node("vardecl", st, map[string]any{
		"specs":  specs,
		"define": false,
	}), nil
}

// emitValueSpec emits one var spec as a self-contained group: its names,
// their object ids, its own type (explicit type, or nil when inferred),
// and its own initializer expressions. Specs are never flattened across
// a block: flattening let a single (last-spec-wins) type cover all names
// and let the backend zip values[i] onto names[i] across specs, so
// `var (a int; b = 2)` assigned 2 to a and left b zero.
//
// The "expand" flag marks a single multi-value right-hand side
// (a call returning as many values as there are names): the one value
// node must be distributed over all names, not assigned to the first
// name only. It is computed from go/types, not from counting alone:
// the RHS type is a tuple of the right length, or the called function's
// signature returns that many results.
func (e *Emitter) emitValueSpec(vs *ast.ValueSpec) (map[string]any, error) {
	names := []string{}
	objs := []any{}
	var typ any
	if vs.Type != nil {
		if tv, ok := e.info.Info.Types[vs.Type]; ok && tv.Type != nil {
			typ = e.typeID(tv.Type)
		}
	}
	for _, name := range vs.Names {
		names = append(names, name.Name)
		if obj := e.info.Info.Defs[name]; obj != nil {
			objs = append(objs, e.declareObject(obj))
		} else {
			objs = append(objs, -1)
		}
	}
	values := []any{}
	for _, v := range vs.Values {
		vn, err := e.emitExpr(v)
		if err != nil {
			return nil, err
		}
		values = append(values, vn)
	}
	expand := false
	if len(vs.Values) == 1 && len(vs.Names) > 1 {
		rhs := vs.Values[0]
		if tv, ok := e.info.Info.Types[rhs]; ok && tv.Type != nil {
			if tup, ok := tv.Type.(*types.Tuple); ok && tup.Len() == len(vs.Names) {
				expand = true
			}
		}
		if !expand {
			if call, ok := rhs.(*ast.CallExpr); ok {
				if sig := e.callSignature(call); sig != nil && sig.Results().Len() == len(vs.Names) {
					expand = true
				}
			}
		}
	}
	return map[string]any{
		"names":  names,
		"objs":   objs,
		"type":   typ,
		"values": values,
		"expand": expand,
	}, nil
}

// callSignature returns the called function's signature from go/types.
// The callee expression itself (including a generic instantiation) carries
// the (instantiated) signature; the unwrapped callee is the fallback.
func (e *Emitter) callSignature(call *ast.CallExpr) *types.Signature {
	if tv, ok := e.info.Info.Types[call.Fun]; ok && tv.Type != nil {
		if sig, ok := tv.Type.(*types.Signature); ok {
			return sig
		}
	}
	fun := call.Fun
	for {
		switch f := fun.(type) {
		case *ast.ParenExpr:
			fun = f.X
		case *ast.IndexExpr:
			fun = f.X
		case *ast.IndexListExpr:
			fun = f.X
		default:
			goto done
		}
	}
done:
	if tv, ok := e.info.Info.Types[fun]; ok && tv.Type != nil {
		if sig, ok := tv.Type.(*types.Signature); ok {
			return sig
		}
	}
	return nil
}

func (e *Emitter) emitIf(st *ast.IfStmt) (map[string]any, error) {
	var init any
	if st.Init != nil {
		in, err := e.emitStmt(st.Init)
		if err != nil {
			return nil, err
		}
		init = in
	}
	cond, err := e.emitExpr(st.Cond)
	if err != nil {
		return nil, err
	}
	then, err := e.emitBlock(st.Body)
	if err != nil {
		return nil, err
	}
	var els any
	if st.Else != nil {
		switch elseStmt := st.Else.(type) {
		case *ast.BlockStmt:
			eb, err := e.emitBlock(elseStmt)
			if err != nil {
				return nil, err
			}
			els = eb
		case *ast.IfStmt:
			ei, err := e.emitIf(elseStmt)
			if err != nil {
				return nil, err
			}
			els = ei
		}
	}
	return e.node("if", st, map[string]any{
		"init": init,
		"cond": cond,
		"then": then,
		"else": els,
	}), nil
}

func (e *Emitter) emitFor(st *ast.ForStmt) (map[string]any, error) {
	var init, cond, post any
	if st.Init != nil {
		in, err := e.emitStmt(st.Init)
		if err != nil {
			return nil, err
		}
		init = in
	}
	if st.Cond != nil {
		cn, err := e.emitExpr(st.Cond)
		if err != nil {
			return nil, err
		}
		cond = cn
	}
	if st.Post != nil {
		pn, err := e.emitStmt(st.Post)
		if err != nil {
			return nil, err
		}
		post = pn
	}
	body, err := e.emitBlock(st.Body)
	if err != nil {
		return nil, err
	}
	return e.node("for", st, map[string]any{
		"init": init,
		"cond": cond,
		"post": post,
		"body": body,
	}), nil
}

func (e *Emitter) emitRange(st *ast.RangeStmt) (map[string]any, error) {
	// Targets are resolved before the body so := targets are declared
	// for it, exactly as for AssignStmt.
	key, keyExpr, err := e.emitRangeTarget(st.Key)
	if err != nil {
		return nil, err
	}
	value, valueExpr, err := e.emitRangeTarget(st.Value)
	if err != nil {
		return nil, err
	}
	x, err := e.emitExpr(st.X)
	if err != nil {
		return nil, err
	}
	body, err := e.emitBlock(st.Body)
	if err != nil {
		return nil, err
	}
	return e.node("range", st, map[string]any{
		"key":       key,
		"value":     value,
		"keyExpr":   keyExpr,
		"valueExpr": valueExpr,
		"define":    st.Tok == token.DEFINE,
		"x":         x,
		"body":      body,
	}), nil
}

// emitRangeTarget emits one range target and returns (objId, exprNode).
// The objId keeps the existing wire shape for ident targets (an int, or
// nil for absent/blank), which the backend already consumes as a slot
// id. It is resolved through Defs AND Uses: for `:=` the target is a
// definition, for `=` it is a use — resolving only through Defs left
// `for i = range x` with key nil, so the backend iterated with
// keySlot = -1 and never wrote the loop variable. Non-ident assignable
// targets (m[k], s.f) have no object id; they are carried by the expr
// node, which the backend must use as the write target (a follow-up on
// the backend side — the emitter no longer silently drops them).
func (e *Emitter) emitRangeTarget(target ast.Expr) (any, any, error) {
	if target == nil {
		return nil, nil, nil
	}
	if id, ok := target.(*ast.Ident); ok && id.Name == "_" {
		return nil, nil, nil
	}
	var obj any
	if id, ok := target.(*ast.Ident); ok {
		var o types.Object
		if obj2 := e.info.Info.Defs[id]; obj2 != nil {
			o = obj2
		} else if obj2 := e.info.Info.Uses[id]; obj2 != nil {
			o = obj2
		}
		if o != nil {
			obj = e.declareObject(o)
		}
	}
	expr, err := e.emitExpr(target)
	if err != nil {
		return nil, nil, err
	}
	return obj, expr, nil
}

func (e *Emitter) emitExpr(x ast.Expr) (map[string]any, error) {
	switch ex := x.(type) {
	case *ast.Ident:
		return e.emitIdent(ex)
	case *ast.BasicLit:
		return e.emitBasicLit(ex)
	case *ast.BinaryExpr:
		return e.emitBinary(ex)
	case *ast.UnaryExpr:
		xn, err := e.emitExpr(ex.X)
		if err != nil {
			return nil, err
		}
		return e.node("unary", x, map[string]any{
			"op":   ex.Op.String(),
			"x":    xn,
			"type": e.exprType(x),
		}), nil
	case *ast.ParenExpr:
		xn, err := e.emitExpr(ex.X)
		if err != nil {
			return nil, err
		}
		return e.node("paren", x, map[string]any{
			"x":    xn,
			"type": e.exprType(x),
		}), nil
	case *ast.CallExpr:
		return e.emitCall(ex)
	case *ast.SelectorExpr:
		return e.emitSelector(ex)
	case *ast.CompositeLit:
		return e.emitCompositeLit(ex)
	case *ast.IndexExpr:
		return e.emitIndex(ex)
	case *ast.IndexListExpr:
		return e.emitIndexList(ex)
	case *ast.SliceExpr:
		return e.emitSlice(ex)
	case *ast.StarExpr:
		// Pointer dereference in value position (*p). A StarExpr used
		// as a type (*T) never reaches here: type positions go through
		// emitTypeExpr / the conversion path in emitCall, which check
		// IsType before calling emitExpr.
		xn, err := e.emitExpr(ex.X)
		if err != nil {
			return nil, err
		}
		return e.node("star", x, map[string]any{
			"x":    xn,
			"type": e.exprType(x),
		}), nil
	case *ast.FuncLit:
		return e.emitFuncLit(ex)
	case *ast.TypeAssertExpr:
		return e.emitTypeAssert(ex)
	case *ast.ArrayType, *ast.ChanType, *ast.FuncType, *ast.InterfaceType, *ast.MapType, *ast.StructType:
		// A bare type expression in an operand position (the callee of a
		// conversion is handled in emitCall; other positions go through
		// emitTypeExpr). It denotes a type, so it emits as a type node
		// carrying the type-table id — never as an unsupported error.
		return e.emitTypeExpr(x)
	default:
		return nil, fmt.Errorf("emit: unsupported expr %T", x)
	}
}

// emitTypeAssert emits x.(T). When Type is nil this is the guard of a
// type switch (x.(type)); the type field is then nil and the enclosing
// typeswitch node gives it meaning — it is not an error, because the
// checker only permits x.(type) in that position.
func (e *Emitter) emitTypeAssert(ex *ast.TypeAssertExpr) (map[string]any, error) {
	xn, err := e.emitExpr(ex.X)
	if err != nil {
		return nil, err
	}
	var typ any
	if ex.Type != nil {
		if tv, ok := e.info.Info.Types[ex.Type]; ok && tv.Type != nil {
			typ = e.typeID(tv.Type)
		}
	}
	return e.node("typeassert", ex, map[string]any{
		"x":    xn,
		"type": typ,
	}), nil
}

// emitFuncLit emits a function literal with the same param/result shape
// as a FuncDecl (including unnamed params/results via the signature
// tuple) plus its body.
func (e *Emitter) emitFuncLit(lit *ast.FuncLit) (map[string]any, error) {
	var sigType *types.Signature
	if tv, ok := e.info.Info.Types[lit.Type]; ok && tv.Type != nil {
		sigType, _ = tv.Type.(*types.Signature)
	}
	var params, results []any = []any{}, []any{}
	if lit.Type.Params != nil {
		var tuple *types.Tuple
		if sigType != nil {
			tuple = sigType.Params()
		}
		params = e.emitParamList(lit.Type.Params, tuple)
	}
	if lit.Type.Results != nil {
		var tuple *types.Tuple
		if sigType != nil {
			tuple = sigType.Results()
		}
		results = e.emitParamList(lit.Type.Results, tuple)
	}
	var body any
	if lit.Body != nil {
		b, err := e.emitBlock(lit.Body)
		if err != nil {
			return nil, err
		}
		body = b
	}
	return e.node("funclit", lit, map[string]any{
		"type":    e.exprType(lit.Type),
		"params":  params,
		"results": results,
		"body":    body,
	}), nil
}

// emitIndexList emits a multi-argument generic instantiation f[T, U].
// The indices are type arguments, so each is emitted as a type node;
// a non-type index falls back to a value expression.
func (e *Emitter) emitIndexList(ix *ast.IndexListExpr) (map[string]any, error) {
	return e.emitGenericInst(ix, ix.X, ix.Indices)
}

// emitGenericInst emits a generic instantiation (IndexExpr with a
// signature/type callee, or IndexListExpr) as one "indexlist" node, so
// the backend has a single shape for instantiated callees regardless of
// argument count.
func (e *Emitter) emitGenericInst(n ast.Expr, x ast.Expr, indices []ast.Expr) (map[string]any, error) {
	xn, err := e.emitExpr(x)
	if err != nil {
		return nil, err
	}
	idxs := []any{}
	for _, idx := range indices {
		in, err := e.emitTypeExpr(idx)
		if err != nil {
			return nil, err
		}
		idxs = append(idxs, in)
	}
	return e.node("indexlist", n, map[string]any{
		"x":       xn,
		"indices": idxs,
		"type":    e.exprType(n),
	}), nil
}

// typeExprOf returns the denoted type for an expression that denotes a
// type, unwrapping parentheses. It recognizes three go/types records of
// "denotes a type": the Types entry flagged IsType, an Ident whose Use
// (or Def) is a TypeName, and a SelectorExpr whose Sel use is a
// TypeName (a package-qualified type). This is how conversions are
// distinguished from calls: MyInt(x) and []byte(s) are conversions even
// though they look syntactically like calls.
func (e *Emitter) typeExprOf(x ast.Expr) (types.Type, bool) {
	for {
		if p, ok := x.(*ast.ParenExpr); ok {
			x = p.X
			continue
		}
		break
	}
	if tv, ok := e.info.Info.Types[x]; ok && tv.Type != nil && tv.IsType() {
		return tv.Type, true
	}
	if id, ok := x.(*ast.Ident); ok {
		if obj := e.info.Info.Uses[id]; obj != nil {
			if tn, ok := obj.(*types.TypeName); ok {
				return tn.Type(), true
			}
		}
		if obj := e.info.Info.Defs[id]; obj != nil {
			if tn, ok := obj.(*types.TypeName); ok {
				return tn.Type(), true
			}
		}
	}
	if sel, ok := x.(*ast.SelectorExpr); ok {
		if obj := e.info.Info.Uses[sel.Sel]; obj != nil {
			if tn, ok := obj.(*types.TypeName); ok {
				return tn.Type(), true
			}
		}
	}
	return nil, false
}

// emitTypeExpr emits an expression known to denote a type as a "typeexpr"
// node carrying its type-table id. Non-type expressions fall back to
// emitExpr, so callers can use this wherever a type-or-value position
// (generic type arguments, type-switch cases) needs one shape.
func (e *Emitter) emitTypeExpr(x ast.Expr) (map[string]any, error) {
	if t, ok := e.typeExprOf(x); ok {
		return e.node("typeexpr", x, map[string]any{
			"type": e.typeID(t),
		}), nil
	}
	// A syntactic type expression with no go/types record still denotes
	// a type (it must not recurse back into emitExpr's type case, which
	// calls here). Emit it with whatever type id is available, nil if
	// none — the checker has already validated the program.
	switch x.(type) {
	case *ast.ArrayType, *ast.ChanType, *ast.FuncType, *ast.InterfaceType, *ast.MapType, *ast.StructType, *ast.StarExpr:
		return e.node("typeexpr", x, map[string]any{
			"type": e.exprType(x),
		}), nil
	}
	return e.emitExpr(x)
}

func (e *Emitter) emitCompositeLit(c *ast.CompositeLit) (map[string]any, error) {
	var typ any
	if c.Type != nil {
		if tv, ok := e.info.Info.Types[c.Type]; ok {
			typ = e.typeID(tv.Type)
		}
	}
	elems := []any{}
	for _, elt := range c.Elts {
		// KeyValueExpr for keyed literals (e.g. Point{X:1}, map[string]int{"a":1}).
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			kn, err := e.emitExpr(kv.Key)
			if err != nil {
				return nil, err
			}
			vn, err := e.emitExpr(kv.Value)
			if err != nil {
				return nil, err
			}
			elems = append(elems, map[string]any{
				"kind": "keyvalue",
				"fields": map[string]any{
					"key":   kn,
					"value": vn,
					"type":  e.exprType(kv),
				},
			})
			continue
		}
		en, err := e.emitExpr(elt)
		if err != nil {
			return nil, err
		}
		elems = append(elems, en)
	}
	return e.node("compositelit", c, map[string]any{
		"type":  typ,
		"elems": elems,
	}), nil
}

func (e *Emitter) emitIndex(ix *ast.IndexExpr) (map[string]any, error) {
	// Distinguish generic instantiation (id[int], T[int]) from index
	// (s[0]). A signature callee is a generic function instantiation;
	// a callee denoting a type is a generic type instantiation. Both
	// emit as "indexlist" (single index), the same shape as
	// IndexListExpr, so the backend has one instantiation shape.
	if tv, ok := e.info.Info.Types[ix.X]; ok && tv.Type != nil {
		if _, isSig := tv.Type.(*types.Signature); isSig {
			return e.emitGenericInst(ix, ix.X, []ast.Expr{ix.Index})
		}
	}
	if _, ok := e.typeExprOf(ix.X); ok {
		return e.emitGenericInst(ix, ix.X, []ast.Expr{ix.Index})
	}
	x, err := e.emitExpr(ix.X)
	if err != nil {
		return nil, err
	}
	index, err := e.emitExpr(ix.Index)
	if err != nil {
		return nil, err
	}
	return e.node("index", ix, map[string]any{
		"x":     x,
		"index": index,
		"type":  e.exprType(ix),
	}), nil
}

func (e *Emitter) emitSlice(s *ast.SliceExpr) (map[string]any, error) {
	x, err := e.emitExpr(s.X)
	if err != nil {
		return nil, err
	}
	var low, high, max any
	if s.Low != nil {
		ln, err := e.emitExpr(s.Low)
		if err != nil {
			return nil, err
		}
		low = ln
	}
	if s.High != nil {
		hn, err := e.emitExpr(s.High)
		if err != nil {
			return nil, err
		}
		high = hn
	}
	if s.Max != nil {
		mn, err := e.emitExpr(s.Max)
		if err != nil {
			return nil, err
		}
		max = mn
	}
	return e.node("slice", s, map[string]any{
		"x":    x,
		"low":  low,
		"high": high,
		"max":  max,
		"type": e.exprType(s),
	}), nil
}

func (e *Emitter) emitIdent(id *ast.Ident) (map[string]any, error) {
	// Blank identifier.
	if id.Name == "_" {
		return e.node("ident", id, map[string]any{
			"name": "_",
		}), nil
	}
	// Builtin?
	if obj := e.info.Info.Uses[id]; obj != nil {
		if _, ok := obj.(*types.Builtin); ok {
			return e.node("ident", id, map[string]any{
				"name":    id.Name,
				"builtin": id.Name,
			}), nil
		}
		oid := e.declareObject(obj)
		return e.node("ident", id, map[string]any{
			"name": id.Name,
			"obj":  oid,
			"type": e.typeID(obj.Type()),
		}), nil
	}
	if obj := e.info.Info.Defs[id]; obj != nil {
		oid := e.declareObject(obj)
		return e.node("ident", id, map[string]any{
			"name": id.Name,
			"obj":  oid,
			"type": e.typeID(obj.Type()),
		}), nil
	}
	return nil, fmt.Errorf("emit: unresolved ident %s", id.Name)
}

func (e *Emitter) emitBasicLit(l *ast.BasicLit) (map[string]any, error) {
	tv, ok := e.info.Info.Types[l]
	if !ok {
		return nil, fmt.Errorf("emit: no type for basiclit %s", l.Value)
	}
	fields := map[string]any{
		"value": l.Value,
		"type":  e.typeID(tv.Type),
	}
	if tv.Value != nil {
		fields["const"] = tv.Value.ExactString()
	}
	_ = constant.MakeInt64(0)
	return e.node("basiclit", l, fields), nil
}

func (e *Emitter) emitBinary(b *ast.BinaryExpr) (map[string]any, error) {
	x, err := e.emitExpr(b.X)
	if err != nil {
		return nil, err
	}
	y, err := e.emitExpr(b.Y)
	if err != nil {
		return nil, err
	}
	return e.node("binary", b, map[string]any{
		"op":   b.Op.String(),
		"x":    x,
		"y":    y,
		"type": e.exprType(b),
	}), nil
}

func (e *Emitter) emitCall(c *ast.CallExpr) (map[string]any, error) {
	// Variadic spread (s...) is not yet supported.
	if c.Ellipsis.IsValid() {
		return nil, fmt.Errorf("emit: unsupported expr *ast.Ellipsis (variadic spread)")
	}
	// Conversion, not a call: the callee denotes a type (int(x),
	// MyInt(x), []byte(s), pkg.T(x)). Emitting it as a call to a type
	// object would make the backend look up a function that does not
	// exist; a "convert" node carrying the target type id says what the
	// source means. The checker guarantees exactly one argument.
	if t, ok := e.typeExprOf(c.Fun); ok {
		if len(c.Args) != 1 {
			return nil, fmt.Errorf("emit: conversion requires exactly one argument, got %d", len(c.Args))
		}
		xn, err := e.emitExpr(c.Args[0])
		if err != nil {
			return nil, err
		}
		return e.node("convert", c, map[string]any{
			"type": e.typeID(t),
			"x":    xn,
		}), nil
	}
	fun, err := e.emitExpr(c.Fun)
	if err != nil {
		return nil, err
	}
	args := []any{}
	for _, a := range c.Args {
		an, err := e.emitExpr(a)
		if err != nil {
			return nil, err
		}
		args = append(args, an)
	}
	// Builtin marker: if the fun is a builtin ident, record it.
	var builtin any
	if id, ok := c.Fun.(*ast.Ident); ok {
		if obj := e.info.Info.Uses[id]; obj != nil {
			if _, ok := obj.(*types.Builtin); ok {
				builtin = id.Name
			}
		}
	}
	return e.node("call", c, map[string]any{
		"fun":      fun,
		"args":     args,
		"builtin":  builtin,
		"type":     e.exprType(c),
		"ellipsis": c.Ellipsis.IsValid(),
	}), nil
}

func (e *Emitter) emitSelector(s *ast.SelectorExpr) (map[string]any, error) {
	// Method call or field access — go/types Selection tells us.
	if sel, ok := e.info.Info.Selections[s]; ok {
		// Method or field selection.
		x, err := e.emitExpr(s.X)
		if err != nil {
			return nil, err
		}
		_ = sel
		return e.node("selector", s, map[string]any{
			"x":    x,
			"sel":  s.Sel.Name,
			"type": e.exprType(s),
		}), nil
	}
	// Package-qualified identifier (e.g. fmt.Println).
	if id, ok := s.X.(*ast.Ident); ok {
		if obj := e.info.Info.Uses[id]; obj != nil {
			if pkgname, ok := obj.(*types.PkgName); ok {
				target := e.info.Info.Uses[s.Sel]
				if target == nil {
					target = e.info.Info.Defs[s.Sel]
				}
				var oid any
				if target != nil {
					oid = e.declareObject(target)
				}
				_ = pkgname
				return e.node("pkgref", s, map[string]any{
					"pkg":  id.Name,
					"name": s.Sel.Name,
					"obj":  oid,
					"type": e.exprType(s),
				}), nil
			}
		}
	}
	return nil, fmt.Errorf("emit: unresolved selector %v", s)
}

// exprType returns the type-table id for an expression's type.
func (e *Emitter) exprType(x ast.Expr) any {
	if tv, ok := e.info.Info.Types[x]; ok && tv.Type != nil {
		return e.typeID(tv.Type)
	}
	return nil
}
