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
	"strconv"
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
		return nil, fmt.Errorf("emit: %s has no signature type", fd.Name.Name)
	}
	sigID := e.typeID(fn.Type())

	// Receiver: methods carry their receiver explicitly (name, object,
	// type). Without it, same-named methods on different types are
	// indistinguishable in the Funcs list. The signature's Recv is the
	// same *types.Var as the Defs entry for a named receiver, and the
	// object for an unnamed one, so this handles both uniformly.
	var recv any
	if sig.Recv() != nil {
		recvVar := sig.Recv()
		rid, err := e.declareParam(recvVar)
		if err != nil {
			return nil, err
		}
		if fd.Recv == nil || len(fd.Recv.List) == 0 {
			// Cannot happen for a checked method, but a receiverless
			// method node would be a silent mis-emission.
			return nil, fmt.Errorf("emit: method %s has a receiver type but no receiver syntax", fd.Name.Name)
		}
		recvField := fd.Recv.List[0]
		recvNode := ast.Node(recvField.Type)
		if len(recvField.Names) > 0 {
			recvNode = recvField.Names[0]
		}
		recv = e.node("param", recvNode, map[string]any{
			"name": recvVar.Name(),
			"obj":  rid,
			"type": e.typeID(recvVar.Type()),
		})
	}

	params := []any{}
	if fd.Type.Params != nil {
		// Walk the signature's params positionally alongside the AST
		// fields: a field with no names is one unnamed parameter, and
		// its object lives only in the signature (Defs has no ident for
		// it). Named fields fall back to the signature too if Defs has
		// no entry (e.g. the blank identifier). Silently dropping a
		// parameter, as the old code did, is not an option.
		pi := 0
		for _, field := range fd.Type.Params.List {
			count := len(field.Names)
			if count == 0 {
				count = 1
			}
			for i := 0; i < count; i++ {
				if pi >= sig.Params().Len() {
					return nil, fmt.Errorf("emit: param %d of %s has no signature entry", pi, fd.Name.Name)
				}
				var nameNode ast.Node = field.Type
				var name string
				var pobj types.Object
				if len(field.Names) > 0 {
					nameNode = field.Names[i]
					name = field.Names[i].Name
					pobj = e.info.Info.Defs[field.Names[i]]
				}
				if pobj == nil {
					pobj = sig.Params().At(pi)
					name = pobj.Name()
				}
				pid, err := e.declareParam(pobj)
				if err != nil {
					return nil, err
				}
				params = append(params, e.node("param", nameNode, map[string]any{
					"name": name,
					"obj":  pid,
					"type": e.typeID(pobj.Type()),
				}))
				pi++
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
		"name":     fn.Name(),
		"sig":      sigID,
		"receiver": recv,
		"params":   params,
		"body":     body,
		"pkgpath":  pkgpath,
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
		// One statement usually yields one node; a multi-spec var
		// DeclStmt yields one vardecl node per spec.
		for _, n := range sn {
			stmts = append(stmts, n)
		}
	}
	return e.node("block", b, map[string]any{"stmts": stmts}), nil
}

// emitStmt emits one statement, returning one node — except a multi-spec
// var DeclStmt, which expands to one vardecl node per spec (see
// emitDeclSpecs). Callers that hold exactly one statement (if/for init
// and post) use emitSingleStmt, which rejects a multi-node result loudly:
// a multi-spec var decl is not valid there in checked Go.
func (e *Emitter) emitStmt(s ast.Stmt) ([]map[string]any, error) {
	one := func(n map[string]any) []map[string]any { return []map[string]any{n} }
	switch st := s.(type) {
	case *ast.ExprStmt:
		x, err := e.emitExpr(st.X)
		if err != nil {
			return nil, err
		}
		return one(e.node("exprstmt", s, map[string]any{"x": x})), nil
	case *ast.ReturnStmt:
		results := []any{}
		for _, r := range st.Results {
			rn, err := e.emitExpr(r)
			if err != nil {
				return nil, err
			}
			results = append(results, rn)
		}
		return one(e.node("return", s, map[string]any{"results": results})), nil
	case *ast.AssignStmt:
		n, err := e.emitAssign(st)
		if err != nil {
			return nil, err
		}
		return one(n), nil
	case *ast.DeclStmt:
		return e.emitDeclSpecs(st)
	case *ast.IfStmt:
		n, err := e.emitIf(st)
		if err != nil {
			return nil, err
		}
		return one(n), nil
	case *ast.ForStmt:
		n, err := e.emitFor(st)
		if err != nil {
			return nil, err
		}
		return one(n), nil
	case *ast.RangeStmt:
		n, err := e.emitRange(st)
		if err != nil {
			return nil, err
		}
		return one(n), nil
	case *ast.IncDecStmt:
		x, err := e.emitExpr(st.X)
		if err != nil {
			return nil, err
		}
		return one(e.node("incdec", s, map[string]any{
			"tok": st.Tok.String(),
			"x":   x,
		})), nil
	case *ast.BranchStmt:
		return one(e.node("branch", s, map[string]any{"tok": st.Tok.String()})), nil
	default:
		return nil, fmt.Errorf("emit: unsupported stmt %T", s)
	}
}

// emitSingleStmt is emitStmt for positions that hold exactly one statement.
func (e *Emitter) emitSingleStmt(s ast.Stmt) (map[string]any, error) {
	nodes, err := e.emitStmt(s)
	if err != nil {
		return nil, err
	}
	if len(nodes) != 1 {
		return nil, fmt.Errorf("emit: %d nodes for single statement %T", len(nodes), s)
	}
	return nodes[0], nil
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

// emitDeclSpecs emits one vardecl node per ValueSpec in a var DeclStmt.
// A multi-spec `var (...)` block must not merge specs into one node: each
// spec has its own type and values, and merging them misattributes the
// last spec's type to every name (v1 fixed the same shape in
// TestVarGenDeclMultiSpecSplit).
func (e *Emitter) emitDeclSpecs(st *ast.DeclStmt) ([]map[string]any, error) {
	gd, ok := st.Decl.(*ast.GenDecl)
	if !ok || gd.Tok != token.VAR {
		return nil, fmt.Errorf("emit: unsupported decl stmt %T", st.Decl)
	}
	nodes := []map[string]any{}
	for _, spec := range gd.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			return nil, fmt.Errorf("emit: unsupported var spec %T", spec)
		}
		n, err := e.emitValueSpec(vs)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	return nodes, nil
}

// emitValueSpec emits one vardecl node for a single ValueSpec: its own
// names, objects, type, and values. Shared by block-level var decls and
// package-level globals (emitGlobals). The node's position is the spec's,
// not the enclosing GenDecl's, so each node points at its own declaration.
func (e *Emitter) emitValueSpec(vs *ast.ValueSpec) (map[string]any, error) {
	names := []string{}
	objs := []any{}
	var typ any
	if vs.Type != nil {
		if tv, ok := e.info.Info.Types[vs.Type]; ok {
			typ = e.typeID(tv.Type)
		}
	}
	for _, name := range vs.Names {
		names = append(names, name.Name)
		if obj := e.info.Info.Defs[name]; obj != nil {
			id, err := e.declareObject(obj)
			if err != nil {
				return nil, err
			}
			objs = append(objs, id)
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
	return e.node("vardecl", vs, map[string]any{
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
		in, err := e.emitSingleStmt(st.Init)
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
		in, err := e.emitSingleStmt(st.Init)
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
		pn, err := e.emitSingleStmt(st.Post)
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
	key, err := e.emitRangeTarget(st.Key)
	if err != nil {
		return nil, err
	}
	value, err := e.emitRangeTarget(st.Value)
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
		"key":    key,
		"value":  value,
		"define": st.Tok == token.DEFINE,
		"x":      x,
		"body":   body,
	}), nil
}

// emitRangeTarget resolves one range variable. An identifier defined by
// := lives in Info.Defs; one assigned by = lives in Info.Uses — a
// Defs-only lookup silently dropped the = form's variables (key/value
// null). The blank identifier is null. A non-identifier target (index,
// selector, ...) is emitted as an expression, like an assignment LHS.
func (e *Emitter) emitRangeTarget(x ast.Expr) (any, error) {
	if x == nil {
		return nil, nil
	}
	if id, ok := x.(*ast.Ident); ok {
		if id.Name == "_" {
			return nil, nil
		}
		if obj := e.info.Info.Defs[id]; obj != nil {
			return e.declareObject(obj)
		}
		if obj := e.info.Info.Uses[id]; obj != nil {
			return e.declareObject(obj)
		}
		return nil, fmt.Errorf("emit: unresolved range target %s", id.Name)
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
	// A struct field name (composite literal key, e.g. Point{X: 1}): the
	// ident denotes a field, not a variable. Declaring it would create a
	// phantom var object for every literal key. Emit the field name with
	// no object instead.
	if obj := e.info.Info.Uses[id]; obj != nil {
		if v, ok := obj.(*types.Var); ok && v.IsField() {
			return e.node("fieldname", id, map[string]any{
				"name": id.Name,
			}), nil
		}
		// Builtin?
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
		if v, ok := obj.(*types.Var); ok && v.IsField() {
			return e.node("fieldname", id, map[string]any{
				"name": id.Name,
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
	return nil, fmt.Errorf("emit: unresolved ident %s", id.Name)
}

func (e *Emitter) emitBasicLit(l *ast.BasicLit) (map[string]any, error) {
	tv, ok := e.info.Info.Types[l]
	if !ok {
		return nil, fmt.Errorf("emit: no type for basiclit %s", l.Value)
	}
	value, err := canonicalBasicLitValue(l, tv)
	if err != nil {
		return nil, err
	}
	fields := map[string]any{
		"value": value,
		"type":  e.typeID(tv.Type),
	}
	if tv.Value != nil {
		fields["const"] = tv.Value.ExactString()
	}
	return e.node("basiclit", l, fields), nil
}

// canonicalBasicLitValue returns the wire value for a basic literal: the
// literal's canonical (decoded) value, derived from the go/types constant
// value — never the raw source spelling in l.Value.
//
// The raw spelling is not a value: "a\nb" is 4 characters of source for a
// 3-character string, '\n' is 2 characters of source for rune 10, and
// legacy octal 017 is source for 15. The Java consumer parses "value"
// as the value itself (it never unescapes), so emitting the spelling
// silently corrupts the program (REVIEW-2026-11-06-10 R-P0-2). The
// canonical forms, by literal kind:
//
//   - INT, CHAR: go/constant ExactString — canonical decimal. Handles
//     legacy octal (017 -> "15"), hex (0x10 -> "16"), binary, and digit
//     separators (1_000 -> "1000"), and gives the rune's code point
//     ('\n' -> "10").
//   - STRING: constant.StringVal — the decoded string, emitted unquoted.
//     The consumer must use it verbatim: stripping quotes would corrupt
//     a decoded string that starts and ends with '"' (e.g. "\"x\"").
//   - FLOAT: ExactString is unusable here — it renders rationals as
//     "a/b" ("1.5" -> "3/2"), which no float parser reads. Format the
//     float64 value instead, at the bit size of the literal's type, so a
//     float32 literal renders at float32 precision ("1.1", not the
//     float64 expansion of the rounded value).
//   - IMAG: ExactString ("(0 + 2i)"). Complex literals have no consumer
//     support yet (NodeFactoryV2 throws on them, as before); the
//     canonical form keeps that failure loud rather than changing it.
func canonicalBasicLitValue(l *ast.BasicLit, tv types.TypeAndValue) (string, error) {
	if tv.Value == nil {
		// A BasicLit is always a constant expression, so go/types always
		// sets Value; this is a defensive fallback only. Unquote the
		// spelling for string/rune literals, keep it raw otherwise.
		if l.Kind == token.STRING || l.Kind == token.CHAR {
			if s, err := strconv.Unquote(l.Value); err == nil {
				return s, nil
			}
		}
		return l.Value, nil
	}
	switch l.Kind {
	case token.INT, token.CHAR:
		return tv.Value.ExactString(), nil
	case token.STRING:
		if tv.Value.Kind() != constant.String {
			return "", fmt.Errorf("emit: string literal %s has non-string constant %v", l.Value, tv.Value)
		}
		return constant.StringVal(tv.Value), nil
	case token.FLOAT:
		f, ok := constant.Float64Val(tv.Value)
		if !ok {
			return "", fmt.Errorf("emit: float literal %s is not representable as float64", l.Value)
		}
		bitSize := 64
		if b, ok := tv.Type.(*types.Basic); ok && b.Kind() == types.Float32 {
			bitSize = 32
		}
		return strconv.FormatFloat(f, 'g', -1, bitSize), nil
	case token.IMAG:
		return tv.Value.ExactString(), nil
	default:
		return "", fmt.Errorf("emit: unsupported basic literal kind %v (%s)", l.Kind, l.Value)
	}
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
		"fun":     fun,
		"args":    args,
		"builtin": builtin,
		"type":    e.exprType(c),
	}), nil
}

func (e *Emitter) emitSelector(s *ast.SelectorExpr) (map[string]any, error) {
	// Method call or field access — go/types Selection tells us.
	if sel, ok := e.info.Info.Selections[s]; ok {
		// Method or field selection. The selection's kind (field vs
		// method value vs method expression) and field/method index are
		// part of the meaning: without them a backend cannot tell
		// p.X (field 0) from p.M (method) or resolve an embedded
		// field's position.
		x, err := e.emitExpr(s.X)
		if err != nil {
			return nil, err
		}
		// types.SelectionKind has no String method; spell the kinds
		// out so the wire form stays stable across Go releases.
		var selKind string
		switch sel.Kind() {
		case types.FieldVal:
			selKind = "field"
		case types.MethodVal:
			selKind = "method"
		case types.MethodExpr:
			selKind = "methodexpr"
		default:
			return nil, fmt.Errorf("emit: unsupported selection kind %v", sel.Kind())
		}
		index := make([]any, len(sel.Index()))
		for i, ix := range sel.Index() {
			index[i] = ix
		}
		return e.node("selector", s, map[string]any{
			"x":       x,
			"sel":     s.Sel.Name,
			"selkind": selKind,
			"index":   index,
			"type":    e.exprType(s),
		}), nil
	}
	// Package-qualified identifier (e.g. fmt.Println).
	if id, ok := s.X.(*ast.Ident); ok {
		if obj := e.info.Info.Uses[id]; obj != nil {
			if _, ok := obj.(*types.PkgName); ok {
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
