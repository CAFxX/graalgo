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
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return nil, fmt.Errorf("emit: %s type is not a signature", fd.Name.Name)
	}
	sigID := e.typeID(fn.Type())

	// Parameters and results come from the signature tuples, matched to
	// AST fields by position. The tuple is authoritative: it contains
	// unnamed parameters and results too, which have no ident for a
	// Defs lookup — walking AST names alone silently dropped them.
	params, err := e.emitFieldVars(fd.Type.Params, sig.Params(), "param")
	if err != nil {
		return nil, err
	}
	results, err := e.emitFieldVars(fd.Type.Results, sig.Results(), "result")
	if err != nil {
		return nil, err
	}

	// Methods carry their receiver; plain functions carry recv nil, so a
	// consumer can tell a method from a same-named function. The
	// receiver object is always declared, named or not: an unnamed
	// receiver still HAS a *types.Var object (its name is just ""), it
	// merely has no ident — exactly like an unnamed parameter, which
	// emitFieldVars also declares. Declaring it keeps recv.obj uniform
	// (an object id, never nil-for-unnamed) so a consumer resolves the
	// receiver the same way for both forms. The node's position is the
	// receiver name when named, else the receiver field.
	var recv any
	if fd.Recv != nil {
		rv := sig.Recv()
		if rv == nil {
			return nil, fmt.Errorf("emit: method %s has no receiver in signature", fd.Name.Name)
		}
		if len(fd.Recv.List) == 0 {
			return nil, fmt.Errorf("emit: method %s has an empty receiver list", fd.Name.Name)
		}
		rid, err := e.declareParam(rv)
		if err != nil {
			return nil, err
		}
		recvNode := ast.Node(fd.Recv.List[0])
		if len(fd.Recv.List[0].Names) > 0 {
			recvNode = fd.Recv.List[0].Names[0]
		}
		recv = e.node("recv", recvNode, map[string]any{
			"name": rv.Name(),
			"obj":  rid,
			"type": e.typeID(rv.Type()),
		})
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
		"sig":     sigID,
		"recv":    recv,
		"params":  params,
		"results": results,
		"body":    body,
		"pkgpath": pkgpath,
	}
	return e.node("funcdecl", fd, fields), nil
}

// emitFieldVars emits one parameter or result list. AST fields and the
// signature tuple are walked in lockstep: a field with N names consumes
// N tuple entries; a field with no names (unnamed parameter/result)
// consumes one tuple entry whose object has no ident. A mismatch between
// the AST list and the tuple is a checker/emitter inconsistency and fails
// loudly instead of emitting a short list.
func (e *Emitter) emitFieldVars(fl *ast.FieldList, tuple *types.Tuple, kind string) ([]any, error) {
	out := []any{}
	if fl == nil {
		return out, nil
	}
	i := 0
	for _, field := range fl.List {
		if len(field.Names) == 0 {
			if i >= tuple.Len() {
				return nil, fmt.Errorf("emit: %s %d has no signature entry", kind, i)
			}
			v := tuple.At(i)
			vid, err := e.declareParam(v)
			if err != nil {
				return nil, err
			}
			out = append(out, e.node(kind, field.Type, map[string]any{
				"name": v.Name(),
				"obj":  vid,
				"type": e.typeID(v.Type()),
			}))
			i++
			continue
		}
		for _, name := range field.Names {
			if i >= tuple.Len() {
				return nil, fmt.Errorf("emit: %s %q has no signature entry", kind, name.Name)
			}
			v := tuple.At(i)
			vid, err := e.declareParam(v)
			if err != nil {
				return nil, err
			}
			out = append(out, e.node(kind, name, map[string]any{
				"name": name.Name,
				"obj":  vid,
				"type": e.typeID(v.Type()),
			}))
			i++
		}
	}
	return out, nil
}

// declareTopGenDecl declares every named object of a top-level GenDecl:
// each var/const name and each type name. Imports declare nothing: the
// imported package names are the checker's business, not emitted objects.
func (e *Emitter) declareTopGenDecl(gd *ast.GenDecl) error {
	switch gd.Tok {
	case token.IMPORT:
		return nil
	case token.VAR, token.CONST:
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				return fmt.Errorf("emit: top-level %s holds %T, want *ast.ValueSpec", gd.Tok, spec)
			}
			for _, name := range vs.Names {
				if obj := e.info.Info.Defs[name]; obj != nil {
					if _, err := e.declareObject(obj); err != nil {
						return err
					}
				}
			}
		}
		return nil
	case token.TYPE:
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				return fmt.Errorf("emit: top-level type holds %T, want *ast.TypeSpec", spec)
			}
			if obj := e.info.Info.Defs[ts.Name]; obj != nil {
				if _, err := e.declareObject(obj); err != nil {
					return err
				}
			}
		}
		return nil
	default:
		return fmt.Errorf("emit: unsupported top-level decl %s", gd.Tok)
	}
}

// emitTopGenDecl emits one top-level GenDecl as Document.Globals entries,
// one node per spec, in source order. Imports emit nothing (see
// declareTopGenDecl); any other token is an error, never a silent skip.
func (e *Emitter) emitTopGenDecl(gd *ast.GenDecl) ([]any, error) {
	switch gd.Tok {
	case token.IMPORT:
		return nil, nil
	case token.VAR, token.CONST:
		kind := "globalvar"
		isConst := gd.Tok == token.CONST
		if isConst {
			kind = "globalconst"
		}
		var out []any
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				return nil, fmt.Errorf("emit: top-level %s holds %T, want *ast.ValueSpec", gd.Tok, spec)
			}
			n, err := e.emitGlobalValueSpec(vs, kind, isConst)
			if err != nil {
				return nil, err
			}
			out = append(out, n)
		}
		return out, nil
	case token.TYPE:
		var out []any
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				return nil, fmt.Errorf("emit: top-level type holds %T, want *ast.TypeSpec", spec)
			}
			var oid any
			obj := e.info.Info.Defs[ts.Name]
			if obj != nil {
				id, err := e.declareObject(obj)
				if err != nil {
					return nil, err
				}
				oid = id
			} else {
				oid = -1
			}
			var typ any
			if obj != nil && obj.Type() != nil {
				typ = e.typeID(obj.Type())
			}
			out = append(out, e.node("globaltype", ts, map[string]any{
				"name":  ts.Name.Name,
				"obj":   oid,
				"type":  typ,
				"alias": ts.Assign.IsValid(),
			}))
		}
		return out, nil
	default:
		return nil, fmt.Errorf("emit: unsupported top-level decl %s", gd.Tok)
	}
}

// emitGlobalValueSpec emits one top-level var or const spec. The shape
// mirrors the local vardecl node (names, objs, type, values) so a
// consumer handles globals and locals uniformly. For consts, "constvals"
// additionally records the evaluated constant for every name: repeated
// const specs (`b;` after `a = iota`) carry no Values in the AST, and
// their values live only in go/types — without constvals a consumer
// would have to re-derive iota itself.
func (e *Emitter) emitGlobalValueSpec(vs *ast.ValueSpec, kind string, isConst bool) (map[string]any, error) {
	names := []string{}
	objs := []any{}
	for _, name := range vs.Names {
		names = append(names, name.Name)
		if obj := e.info.Info.Defs[name]; obj != nil {
			oid, err := e.declareObject(obj)
			if err != nil {
				return nil, err
			}
			objs = append(objs, oid)
		} else {
			objs = append(objs, -1)
		}
	}
	var typ any
	if vs.Type != nil {
		if tv, ok := e.info.Info.Types[vs.Type]; ok && tv.Type != nil {
			typ = e.typeID(tv.Type)
		}
	}
	if typ == nil && len(vs.Names) > 0 {
		if obj := e.info.Info.Defs[vs.Names[0]]; obj != nil && obj.Type() != nil {
			typ = e.typeID(obj.Type())
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
	fields := map[string]any{
		"names":  names,
		"objs":   objs,
		"type":   typ,
		"values": values,
	}
	if isConst {
		constvals := []any{}
		for _, name := range vs.Names {
			if c, ok := e.info.Info.Defs[name].(*types.Const); ok && c.Val() != nil {
				constvals = append(constvals, c.Val().ExactString())
			} else {
				constvals = append(constvals, nil)
			}
		}
		fields["constvals"] = constvals
	}
	return e.node(kind, vs, fields), nil
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
		// The label is the whole point of a labelled branch: break L,
		// continue L, and goto L must be distinguishable from their
		// unlabelled forms, which carry label nil.
		var label any
		if st.Label != nil {
			label = st.Label.Name
		}
		return e.node("branch", s, map[string]any{
			"tok":   st.Tok.String(),
			"label": label,
		}), nil
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
		value, err := e.emitExpr(st.Value)
		if err != nil {
			return nil, err
		}
		return e.node("send", s, map[string]any{"chan": ch, "value": value}), nil
	case *ast.SwitchStmt:
		return e.emitSwitch(st)
	case *ast.TypeSwitchStmt:
		return e.emitTypeSwitch(st)
	case *ast.SelectStmt:
		return e.emitSelect(st)
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

// emitSwitch emits a switch statement: optional init, optional tag (nil
// for `switch {`), and one "case" node per clause. A clause with an empty
// list is the default clause, flagged explicitly so a consumer need not
// infer it from list emptiness (which would also match `case nil:` —
// that clause has a one-element list holding the nil ident).
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
	body, err := e.emitCaseBody(st.Body)
	if err != nil {
		return nil, err
	}
	return e.node("switch", st, map[string]any{
		"init": init,
		"tag":  tag,
		"body": body,
	}), nil
}

// emitTypeSwitch emits a type switch. The guard gets its own node kind,
// not an ordinary assign: go/types deliberately records no single object
// for a type-switch guard variable (Defs maps it to nil — the variable
// has a different type in each clause), so emitting it as an assign
// would either fail on the guard ident or invent an object that does
// not exist. Per-case uses of the guard variable resolve through
// go/types Uses to their per-clause objects like any other ident.
func (e *Emitter) emitTypeSwitch(st *ast.TypeSwitchStmt) (map[string]any, error) {
	var init any
	if st.Init != nil {
		in, err := e.emitStmt(st.Init)
		if err != nil {
			return nil, err
		}
		init = in
	}
	guard, err := e.emitTypeSwitchGuard(st.Assign)
	if err != nil {
		return nil, err
	}
	body, err := e.emitCaseBody(st.Body)
	if err != nil {
		return nil, err
	}
	return e.node("typeswitch", st, map[string]any{
		"init":  init,
		"guard": guard,
		"body":  body,
	}), nil
}

// emitTypeSwitchGuard emits a type-switch guard: `v := x.(type)` or
// `x.(type)`. The guard variable is recorded by name only — it has no
// single object (see emitTypeSwitch) — and the switched expression is
// emitted as "x". Anything else in guard position is malformed and fails.
func (e *Emitter) emitTypeSwitchGuard(assign ast.Stmt) (map[string]any, error) {
	switch a := assign.(type) {
	case *ast.AssignStmt:
		if len(a.Lhs) != 1 || len(a.Rhs) != 1 {
			return nil, fmt.Errorf("emit: malformed type switch guard: want one LHS and one RHS")
		}
		ta, ok := a.Rhs[0].(*ast.TypeAssertExpr)
		if !ok || ta.Type != nil {
			return nil, fmt.Errorf("emit: type switch guard is not x.(type)")
		}
		id, ok := a.Lhs[0].(*ast.Ident)
		if !ok {
			return nil, fmt.Errorf("emit: type switch guard LHS is not an ident")
		}
		x, err := e.emitExpr(ta.X)
		if err != nil {
			return nil, err
		}
		return e.node("typeswitchguard", a, map[string]any{
			"var": id.Name,
			"x":   x,
		}), nil
	case *ast.ExprStmt:
		ta, ok := a.X.(*ast.TypeAssertExpr)
		if !ok || ta.Type != nil {
			return nil, fmt.Errorf("emit: type switch guard is not x.(type)")
		}
		x, err := e.emitExpr(ta.X)
		if err != nil {
			return nil, err
		}
		return e.node("typeswitchguard", a, map[string]any{
			"var": nil,
			"x":   x,
		}), nil
	default:
		return nil, fmt.Errorf("emit: unsupported type switch guard %T", assign)
	}
}

// emitCaseBody emits the clause list of a switch/select body. Every
// element must be the clause kind for that statement; anything else is
// a malformed AST and fails loudly.
func (e *Emitter) emitCaseBody(body *ast.BlockStmt) ([]any, error) {
	clauses := []any{}
	for _, stmt := range body.List {
		cc, ok := stmt.(*ast.CaseClause)
		if !ok {
			return nil, fmt.Errorf("emit: switch body holds %T, want *ast.CaseClause", stmt)
		}
		list := []any{}
		for _, expr := range cc.List {
			// Type-switch case lists hold type expressions (`case int:`,
			// `case []byte:`), not value expressions. go/types marks
			// them IsType(); emitting them through emitExpr would fail
			// on type syntax, so they become typeexpr nodes carrying
			// the resolved type id. (In a checked program a plain
			// switch case is never a type, so this branch cannot
			// misfire there; `case nil:` is a value and takes the
			// emitExpr path.)
			if tv, ok := e.info.Info.Types[expr]; ok && tv.IsType() {
				list = append(list, e.node("typeexpr", expr, map[string]any{
					"type": e.typeID(tv.Type),
				}))
				continue
			}
			en, err := e.emitExpr(expr)
			if err != nil {
				return nil, err
			}
			list = append(list, en)
		}
		stmts := []any{}
		for _, s := range cc.Body {
			sn, err := e.emitStmt(s)
			if err != nil {
				return nil, err
			}
			stmts = append(stmts, sn)
		}
		clauses = append(clauses, e.node("case", cc, map[string]any{
			"list":    list,
			"default": len(cc.List) == 0,
			"body":    stmts,
		}))
	}
	return clauses, nil
}

// emitSelect emits a select statement: one "comm" node per clause. The
// comm is the communication statement (send, receive-assign, or bare
// receive); a default clause has comm nil.
func (e *Emitter) emitSelect(st *ast.SelectStmt) (map[string]any, error) {
	clauses := []any{}
	for _, stmt := range st.Body.List {
		cc, ok := stmt.(*ast.CommClause)
		if !ok {
			return nil, fmt.Errorf("emit: select body holds %T, want *ast.CommClause", stmt)
		}
		var comm any
		if cc.Comm != nil {
			cn, err := e.emitStmt(cc.Comm)
			if err != nil {
				return nil, err
			}
			comm = cn
		}
		body := []any{}
		for _, s := range cc.Body {
			sn, err := e.emitStmt(s)
			if err != nil {
				return nil, err
			}
			body = append(body, sn)
		}
		clauses = append(clauses, e.node("comm", cc, map[string]any{
			"comm":    comm,
			"default": cc.Comm == nil,
			"body":    body,
		}))
	}
	return e.node("select", st, map[string]any{"body": clauses}), nil
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
					if _, err := e.declareObject(obj); err != nil {
						return nil, err
					}
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
	names := []string{}
	objs := []any{}
	var typ any
	values := []any{}
	for _, spec := range gd.Specs {
		vs := spec.(*ast.ValueSpec)
		if vs.Type != nil {
			if tv, ok := e.info.Info.Types[vs.Type]; ok {
				typ = e.typeID(tv.Type)
			}
		}
		for _, name := range vs.Names {
			names = append(names, name.Name)
			if obj := e.info.Info.Defs[name]; obj != nil {
				oid, err := e.declareObject(obj)
				if err != nil {
					return nil, err
				}
				objs = append(objs, oid)
			} else {
				objs = append(objs, -1)
			}
		}
		for _, v := range vs.Values {
			vn, err := e.emitExpr(v)
			if err != nil {
				return nil, err
			}
			values = append(values, vn)
		}
	}
	return e.node("vardecl", st, map[string]any{
		"names":  names,
		"objs":   objs,
		"define": false,
		"type":   typ,
		"values": values,
	}), nil
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
	var key, value any
	if st.Key != nil {
		if id, ok := st.Key.(*ast.Ident); ok && id.Name != "_" {
			if obj := e.info.Info.Defs[id]; obj != nil {
				kid, err := e.declareObject(obj)
				if err != nil {
					return nil, err
				}
				key = kid
			}
		}
	}
	if st.Value != nil {
		if id, ok := st.Value.(*ast.Ident); ok && id.Name != "_" {
			if obj := e.info.Info.Defs[id]; obj != nil {
				vid, err := e.declareObject(obj)
				if err != nil {
					return nil, err
				}
				value = vid
			}
		}
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
		"key":    key,
		"value":  value,
		"define": st.Tok == token.DEFINE,
		"x":      x,
		"body":   body,
	}), nil
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
	case *ast.SliceExpr:
		return e.emitSlice(ex)
	case *ast.TypeAssertExpr:
		return e.emitTypeAssert(ex)
	default:
		return nil, fmt.Errorf("emit: unsupported expr %T", x)
	}
}

// emitTypeAssert emits a type assertion. For `x.(type)` — legal only as a
// type-switch guard — the asserted type is nil by definition: each case
// supplies it, so the node carries type nil rather than inventing one.
// For `x.(T)`, the type is the resolved type id; a missing type entry for
// a non-nil asserted type is a checker/emitter inconsistency and fails.
func (e *Emitter) emitTypeAssert(t *ast.TypeAssertExpr) (map[string]any, error) {
	x, err := e.emitExpr(t.X)
	if err != nil {
		return nil, err
	}
	var typ any
	if t.Type != nil {
		tv, ok := e.info.Info.Types[t.Type]
		if !ok || tv.Type == nil {
			return nil, fmt.Errorf("emit: no type for asserted type %v", t.Type)
		}
		typ = e.typeID(tv.Type)
	}
	return e.node("typeassert", t, map[string]any{
		"x":    x,
		"type": typ,
	}), nil
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
	// Distinguish generic instantiation (id[int]) from index (s[0]).
	// If X is a function (has a signature type), this is generics.
	if tv, ok := e.info.Info.Types[ix.X]; ok && tv.Type != nil {
		if _, isSig := tv.Type.(*types.Signature); isSig {
			return nil, fmt.Errorf("emit: unsupported expr *ast.IndexExpr (generic instantiation)")
		}
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
		oid, err := e.declareObject(obj)
		if err != nil {
			return nil, err
		}
		return e.node("ident", id, map[string]any{
			"name": id.Name,
			"obj":  oid,
			"type": e.typeID(obj.Type()),
		}), nil
	}
	if obj := e.info.Info.Defs[id]; obj != nil {
		oid, err := e.declareObject(obj)
		if err != nil {
			return nil, err
		}
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
					tid, err := e.declareObject(target)
					if err != nil {
						return nil, err
					}
					oid = tid
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
