// Node emitters: one function per AST node kind.
//
// Each emitter takes the typed AST node and returns its in-memory
// representation: { "kind", "pos", "fields" }. Emitters never synthesize
// code, never special-case packages, and never desugar stdlib calls.
package emit

import (
	"fmt"
	"go/ast"
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

	// The method receiver is part of the declaration's identity: two
	// same-named methods on different types must not emit identically,
	// and the backend must be able to tell a method from a function.
	// fn.Type() is the signature; its Recv is nil for plain functions.
	var recv any
	sigType, _ := fn.Type().(*types.Signature)
	if sigType != nil && sigType.Recv() != nil {
		rv := sigType.Recv()
		recv = map[string]any{
			"name": rv.Name(),
			"obj":  e.declareObject(rv),
			"type": e.typeID(rv.Type()),
		}
	}

	params := []any{}
	if fd.Type.Params != nil {
		// Positional index into the signature's parameter list. A field
		// with no names is one unnamed parameter (func f(int, string)),
		// not zero parameters: the old loop over field.Names dropped
		// those entirely while the signature still carried them.
		// (Go forbids mixing named and unnamed parameters, so the
		// positional index stays aligned across fields.)
		idx := 0
		for _, field := range fd.Type.Params.List {
			if len(field.Names) == 0 {
				if sigType == nil || idx >= sigType.Params().Len() {
					return nil, fmt.Errorf("emit: no signature parameter at index %d for func %s", idx, fd.Name.Name)
				}
				pobj := sigType.Params().At(idx)
				idx++
				pid := e.declareParam(pobj)
				params = append(params, e.node("param", field, map[string]any{
					"name": "",
					"obj":  pid,
					"type": e.typeID(pobj.Type()),
				}))
				continue
			}
			for _, name := range field.Names {
				idx++
				pobj := e.info.Info.Defs[name]
				if pobj == nil {
					continue
				}
				pid := e.declareParam(pobj)
				params = append(params, e.node("param", name, map[string]any{
					"name": name.Name,
					"obj":  pid,
					"type": e.typeID(pobj.Type()),
				}))
			}
		}
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
		"body":    body,
		"pkgpath": pkgpath,
	}
	return e.node("funcdecl", fd, fields), nil
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
		return e.node("branch", s, map[string]any{"tok": st.Tok.String()}), nil
	default:
		return nil, fmt.Errorf("emit: unsupported stmt %T", s)
	}
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
				objs = append(objs, e.declareObject(obj))
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
	var err error
	if st.Key != nil {
		key, err = e.emitRangeVar(st.Key)
		if err != nil {
			return nil, err
		}
	}
	if st.Value != nil {
		value, err = e.emitRangeVar(st.Value)
		if err != nil {
			return nil, err
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

// emitRangeVar emits one side of a range clause. Identifier targets keep
// the historical shape (the declared/used object id, nil for blank).
// Non-identifier targets (field selectors, index expressions, ...) are
// emitted as general expressions: the old code left them nil while still
// emitting the body, silently dropping the assignment target. An
// identifier with no object in either Defs or Uses is an error, not a
// silent nil.
func (e *Emitter) emitRangeVar(x ast.Expr) (any, error) {
	if id, ok := x.(*ast.Ident); ok {
		if id.Name == "_" {
			return nil, nil
		}
		obj := e.info.Info.Defs[id]
		if obj == nil {
			obj = e.info.Info.Uses[id]
		}
		if obj == nil {
			return nil, fmt.Errorf("emit: unresolved range var %s", id.Name)
		}
		return e.declareObject(obj), nil
	}
	return e.emitExpr(x)
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
	default:
		return nil, fmt.Errorf("emit: unsupported expr %T", x)
	}
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
	// No "ellipsis" field: a valid Ellipsis returns an error above, so
	// the field would always be false on every successfully emitted call.
	return e.node("call", c, map[string]any{
		"fun":     fun,
		"args":    args,
		"builtin": builtin,
		"type":    e.exprType(c),
	}), nil
}

func (e *Emitter) emitSelector(s *ast.SelectorExpr) (map[string]any, error) {
	// Method call or field access — go/types Selection tells us.
	if sel, ok := e.info.Info.Selections[s]; ok {
		// Emit the Selection itself, not just the name: the index is
		// the promotion path (len > 1 means a promoted field/method),
		// the kind distinguishes a method from a field, and the object
		// identifies which declaration was selected. Dropping all three
		// (the old `_ = sel`) made promoted selections indistinguishable
		// from direct ones.
		x, err := e.emitExpr(s.X)
		if err != nil {
			return nil, err
		}
		index := []any{}
		for _, i := range sel.Index() {
			index = append(index, i)
		}
		var recv any
		if rt := sel.Recv(); rt != nil {
			recv = e.typeID(rt)
		}
		return e.node("selector", s, map[string]any{
			"x":            x,
			"sel":          s.Sel.Name,
			"sel_index":    index,
			"sel_kind":     selectionKindName(sel.Kind()),
			"sel_obj":      e.declareObject(sel.Obj()),
			"sel_recv":     recv,
			"sel_indirect": sel.Indirect(),
			"type":         e.exprType(s),
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

// selectionKindName renders a go/types SelectionKind. go/types defines
// no String method for it, so the mapping lives here, in one place.
func selectionKindName(k types.SelectionKind) string {
	switch k {
	case types.FieldVal:
		return "field"
	case types.MethodVal:
		return "method"
	case types.MethodExpr:
		return "method_expr"
	default:
		return "unknown"
	}
}

// exprType returns the type-table id for an expression's type.
func (e *Emitter) exprType(x ast.Expr) any {
	if tv, ok := e.info.Info.Types[x]; ok && tv.Type != nil {
		return e.typeID(tv.Type)
	}
	return nil
}
