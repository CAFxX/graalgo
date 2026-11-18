package emit

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"reflect"

	"graalgo-toolexec/ir"
)

var unOps = map[token.Token]ir.UnOp{token.ADD: ir.UnOpPos, token.SUB: ir.UnOpNeg, token.NOT: ir.UnOpNot, token.XOR: ir.UnOpBitNot}

var binOps = map[token.Token]ir.BinOp{
	token.ADD: ir.BinOpAdd, token.SUB: ir.BinOpSub, token.MUL: ir.BinOpMul, token.QUO: ir.BinOpQuo,
	token.REM: ir.BinOpRem, token.AND: ir.BinOpAnd, token.OR: ir.BinOpOr, token.XOR: ir.BinOpXor,
	token.SHL: ir.BinOpShl, token.SHR: ir.BinOpShr, token.AND_NOT: ir.BinOpAndNot,
	token.LAND: ir.BinOpLAnd, token.LOR: ir.BinOpLOr, token.EQL: ir.BinOpEql, token.NEQ: ir.BinOpNeq,
	token.LSS: ir.BinOpLss, token.LEQ: ir.BinOpLeq, token.GTR: ir.BinOpGtr, token.GEQ: ir.BinOpGeq,
}

var builtins = map[string]ir.Builtin{
	"append": ir.BuiltinAppend, "cap": ir.BuiltinCap, "clear": ir.BuiltinClear, "close": ir.BuiltinClose,
	"complex": ir.BuiltinComplex, "copy": ir.BuiltinCopy, "delete": ir.BuiltinDelete, "imag": ir.BuiltinImag,
	"len": ir.BuiltinLen, "make": ir.BuiltinMake, "max": ir.BuiltinMax, "min": ir.BuiltinMin,
	"new": ir.BuiltinNew, "panic": ir.BuiltinPanic, "print": ir.BuiltinPrint, "println": ir.BuiltinPrintln,
	"real": ir.BuiltinReal, "recover": ir.BuiltinRecover,
	"Add": ir.BuiltinUnsafeAdd, "Slice": ir.BuiltinUnsafeSlice, "SliceData": ir.BuiltinUnsafeSliceData,
	"String": ir.BuiltinUnsafeString, "StringData": ir.BuiltinUnsafeStringData,
	"Sizeof": ir.BuiltinUnsafeSizeof, "Alignof": ir.BuiltinUnsafeAlignof, "Offsetof": ir.BuiltinUnsafeOffsetof,
}

func unparen(x ast.Expr) ast.Expr { return ast.Unparen(x) }

func (fs *fnState) expr(x ast.Expr) ir.Expr {
	fs.enter()
	defer fs.leave()
	e := fs.e
	info := e.cfg.Info
	tv, hasTV := info.Types[x]
	if hasTV && tv.Value != nil {
		return &ir.ConstExpr{Type: e.typeID(concrete(tv.Type)), Pos: fs.pos(x), Value: *constValueOf(tv.Value)}
	}
	typ := func() ir.TypeID { return e.typeID(concrete(tv.Type)) }
	switch x := x.(type) {
	case *ast.ParenExpr:
		return fs.expr(x.X)
	case *ast.Ident:
		return fs.ident(x)
	case *ast.FuncLit:
		return fs.funcLit(x)
	case *ast.CompositeLit:
		return fs.compositeLit(x, tv.Type)
	case *ast.SelectorExpr:
		return fs.selector(x)
	case *ast.IndexExpr:
		if inst := fs.instance(x.X); inst != nil {
			return inst
		}
		return &ir.IndexExpr{Type: typ(), Pos: fs.opPos(x, x.Lbrack), X: fs.expr(x.X), Index: fs.indexOperand(x.X, x.Index)}
	case *ast.IndexListExpr:
		if inst := fs.instance(x.X); inst != nil {
			return inst
		}
	case *ast.SliceExpr:
		if _, isArr := info.Types[x.X].Type.Underlying().(*types.Array); isArr {
			fs.markAddrTaken(x.X)
		}
		return &ir.SliceExpr{Type: typ(), Pos: fs.opPos(x, x.Lbrack), X: fs.expr(x.X), Lo: fs.optExpr(x.Low), Hi: fs.optExpr(x.High), Max: fs.optExpr(x.Max)}
	case *ast.StarExpr:
		return &ir.DerefExpr{Type: typ(), Pos: fs.pos(x), X: fs.expr(x.X)}
	case *ast.UnaryExpr:
		switch x.Op {
		case token.AND:
			fs.markAddrTaken(x.X)
			if cl, ok := unparen(x.X).(*ast.CompositeLit); ok {
				return &ir.AddrExpr{Type: typ(), Pos: fs.pos(x), X: fs.compositeLit(cl, info.Types[cl].Type)}
			}
			return &ir.AddrExpr{Type: typ(), Pos: fs.pos(x), X: fs.expr(x.X)}
		case token.ARROW:
			return &ir.RecvExpr{Type: typ(), Pos: fs.pos(x), Ch: fs.expr(x.X)}
		}
		op, ok := unOps[x.Op]
		if !ok {
			e.unsupportedf(x.Pos(), "unary operator %s", x.Op)
		}
		return &ir.UnaryExpr{Type: typ(), Pos: fs.pos(x), Op: op, X: fs.expr(x.X)}
	case *ast.BinaryExpr:
		op := binOps[x.Op]
		lt, rt := info.Types[x.X].Type, info.Types[x.Y].Type
		var l, r ir.Expr
		switch {
		case (op == ir.BinOpEql || op == ir.BinOpNeq) && types.IsInterface(lt) && !types.IsInterface(rt) && !info.Types[x.Y].IsNil():
			l, r = fs.expr(x.X), fs.convExpr(x.Y, lt)
		case (op == ir.BinOpEql || op == ir.BinOpNeq) && types.IsInterface(rt) && !types.IsInterface(lt) && !info.Types[x.X].IsNil():
			l, r = fs.convExpr(x.X, rt), fs.expr(x.Y)
		default:
			l, r = fs.expr(x.X), fs.expr(x.Y)
		}
		if op == ir.BinOpShl || op == ir.BinOpShr {
			r = fs.shiftCount(x.Y, r)
		}
		if n, ok := l.(*ir.NilExpr); ok {
			n.Type = e.typeID(rt)
		}
		if n, ok := r.(*ir.NilExpr); ok {
			n.Type = e.typeID(lt)
		}
		return &ir.BinaryExpr{Type: typ(), Pos: fs.opPos(x, x.OpPos), Op: op, X: l, Y: r}
	case *ast.CallExpr:
		return fs.call(x)
	case *ast.TypeAssertExpr:
		return &ir.TypeAssertExpr{Type: typ(), Pos: fs.opPos(x, x.Lparen-1), X: fs.expr(x.X), Target: e.typeID(info.Types[x.Type].Type)}
	case *ast.KeyValueExpr:
		e.unsupportedf(x.Pos(), "key-value expression outside a composite literal")
	}
	e.unsupportedf(x.Pos(), "expression %T", x)
	return nil
}

func isTypeParam(t types.Type) bool {
	_, ok := types.Unalias(t).(*types.TypeParam)
	return ok
}

// isUntyped reports whether t is an untyped basic type.
func isUntyped(t types.Type) bool {
	b, ok := t.(*types.Basic)
	return ok && b.Info()&types.IsUntyped != 0
}

// concrete is the type an expression of type t has in the IR: untyped
// types other than untyped nil become their default type (the type go/types
// leaves recorded for print/println arguments, conditions and untyped
// boolean results of comparisons). Contexts that give an untyped value
// another type (shift counts, conversions to type parameters, nil) are
// handled where they occur.
func concrete(t types.Type) types.Type {
	if isUntyped(t) && t.(*types.Basic).Kind() != types.UntypedNil {
		return types.Default(t)
	}
	return t
}

// shiftCount types an untyped constant shift count as uint (spec: "an
// untyped constant representable by a value of type uint").
func (fs *fnState) shiftCount(y ast.Expr, r ir.Expr) ir.Expr {
	if c, ok := r.(*ir.ConstExpr); ok && isUntyped(fs.e.cfg.Info.Types[y].Type) {
		c.Type = fs.e.typeID(types.Typ[types.Uint])
	}
	return r
}

func (fs *fnState) optExpr(x ast.Expr) ir.Expr {
	if x == nil {
		return nil
	}
	return fs.expr(x)
}

// indexOperand converts a map key to the key type (interface keys).
func (fs *fnState) indexOperand(x, index ast.Expr) ir.Expr {
	if m, ok := coreType(fs.e.cfg.Info.Types[x].Type).(*types.Map); ok {
		return fs.convExpr(index, m.Key())
	}
	return fs.expr(index)
}

func (fs *fnState) ident(x *ast.Ident) ir.Expr {
	e := fs.e
	info := e.cfg.Info
	if x.Name == "_" {
		return &ir.BlankExpr{Type: e.typeID(info.Types[x].Type), Pos: fs.pos(x)}
	}
	obj := info.Uses[x]
	if obj == nil {
		obj = info.Defs[x]
	}
	switch obj := obj.(type) {
	case *types.Nil:
		return &ir.NilExpr{Type: e.typeID(info.Types[x].Type), Pos: fs.pos(x)}
	case *types.Var:
		if obj.Parent() != nil && obj.Parent() == obj.Pkg().Scope() || obj.Pkg() != e.cfg.Pkg {
			g := fs.globalRef(obj, x.Pos())
			setPos(g, fs.pos(x))
			return g
		}
		if _, ok := fs.locals[obj]; !ok && obj.Parent() == nil {
			// package-level variable of this package seen through Defs
			g := fs.globalRef(obj, x.Pos())
			setPos(g, fs.pos(x))
			return g
		}
		return &ir.LocalExpr{Type: e.typeID(obj.Type()), Pos: fs.pos(x), Local: fs.local(obj, x.Pos())}
	case *types.Func:
		g := fs.funcRef(obj, x)
		return g
	}
	e.unsupportedf(x.Pos(), "identifier %s (%T)", x.Name, obj)
	return nil
}

func setPos(x ir.Expr, p ir.PosID) {
	switch y := x.(type) {
	case *ir.GlobalExpr:
		y.Pos = p
	case *ir.ExtExpr:
		y.Pos = p
	}
}

// funcRef emits a reference to a package-level function, wrapped in Inst
// when the use instantiates a generic function.
func (fs *fnState) funcRef(f *types.Func, id *ast.Ident) ir.Expr {
	e := fs.e
	e.registerFunc(f)
	var ref ir.Expr
	if f.Pkg() == e.cfg.Pkg {
		oid, ok := e.objIDs[f]
		if !ok {
			e.unsupportedf(id.Pos(), "internal: function %s not in OBJECTS", f.Name())
		}
		fs.root.objs[oid] = true
		ref = &ir.GlobalExpr{Type: e.typeID(f.Type()), Pos: fs.pos(id), Obj: oid}
	} else {
		sid := e.objSym(f)
		fs.root.syms[sid] = true
		ref = &ir.ExtExpr{Type: e.typeID(f.Type()), Pos: fs.pos(id), Sym: sid}
	}
	if inst, ok := e.cfg.Info.Instances[id]; ok {
		return &ir.InstExpr{Type: e.typeID(inst.Type), Pos: fs.pos(id), Fn: ref, Targs: e.typeListIDs(inst.TypeArgs)}
	}
	return ref
}

// instance handles f[T] / f[T1, T2] where f is a generic function.
func (fs *fnState) instance(fun ast.Expr) ir.Expr {
	var id *ast.Ident
	switch f := unparen(fun).(type) {
	case *ast.Ident:
		id = f
	case *ast.SelectorExpr:
		if _, isSel := fs.e.cfg.Info.Selections[f]; !isSel {
			id = f.Sel
		}
	}
	if id == nil {
		return nil
	}
	if _, ok := fs.e.cfg.Info.Instances[id]; !ok {
		return nil
	}
	if f, ok := fs.e.cfg.Info.Uses[id].(*types.Func); ok {
		return fs.funcRef(f, id)
	}
	return nil
}

// fieldPathType returns the type reached by following field indices from t.
func fieldPathType(t types.Type, path []int) types.Type {
	for _, i := range path {
		u := coreType(t)
		if p, ok := u.(*types.Pointer); ok {
			u = coreType(p.Elem())
		}
		t = u.(*types.Struct).Field(i).Type()
	}
	return t
}

func ints(p []int) []uint64 {
	out := make([]uint64, len(p))
	for i, v := range p {
		out[i] = uint64(v)
	}
	return out
}

func (fs *fnState) selector(x *ast.SelectorExpr) ir.Expr {
	e := fs.e
	info := e.cfg.Info
	sel, ok := info.Selections[x]
	if !ok {
		// qualified identifier pkg.Name
		obj := info.Uses[x.Sel]
		switch obj := obj.(type) {
		case *types.Var:
			g := fs.globalRef(obj, x.Pos())
			setPos(g, fs.pos(x))
			return g
		case *types.Func:
			return fs.funcRef(obj, x.Sel)
		}
		e.unsupportedf(x.Pos(), "qualified identifier %s (%T)", x.Sel.Name, obj)
	}
	typ := e.typeID(info.Types[x].Type)
	switch sel.Kind() {
	case types.FieldVal:
		return &ir.FieldExpr{Type: typ, Pos: fs.pos(x), X: fs.expr(x.X), Path: ints(sel.Index())}
	case types.MethodVal:
		m := sel.Obj().(*types.Func)
		recv, isIface := fs.methodRecv(x, sel)
		if isIface {
			name, pkg := methodName(m)
			return &ir.IfaceMethodValExpr{Type: typ, Pos: fs.pos(x), Recv: recv, Name: name, Pkg: pkg}
		}
		sid := fs.methodSymRef(m)
		return &ir.MethodValExpr{Type: typ, Pos: fs.pos(x), Recv: recv, Method: sid, RecvType: fs.recvTypeID(recv), Targs: fs.methodTargs(x)}
	case types.MethodExpr:
		m := sel.Obj().(*types.Func)
		recvT := sel.Recv()
		if types.IsInterface(recvT) {
			name, pkg := methodName(m)
			return &ir.IfaceMethodExprExpr{Type: typ, Pos: fs.pos(x), Iface: e.typeID(recvT), Name: name, Pkg: pkg}
		}
		if len(sel.Index()) > 1 {
			return fs.promotedMethodExpr(x, sel)
		}
		return &ir.MethodExprExpr{Type: typ, Pos: fs.pos(x), Method: fs.methodSymRef(m), RecvType: e.typeID(recvT), Targs: fs.methodTargs(x)}
	}
	e.unsupportedf(x.Pos(), "selector kind %v", sel.Kind())
	return nil
}

func methodName(m *types.Func) (name, pkg string) {
	if !m.Exported() {
		pkg = pkgPath(m.Pkg())
	}
	return m.Name(), pkg
}

func (fs *fnState) methodSymRef(m *types.Func) uint32 {
	fs.e.registerFunc(m.Origin())
	id := fs.e.methodSym(m.Origin())
	fs.root.syms[id] = true
	return id
}

func (fs *fnState) methodTargs(x *ast.SelectorExpr) []ir.TypeID {
	if inst, ok := fs.e.cfg.Info.Instances[x.Sel]; ok {
		return fs.e.typeListIDs(inst.TypeArgs)
	}
	return nil
}

// recvTypeID is the named receiver type of an adjusted receiver
// expression (its pointer base, instantiated).
func (fs *fnState) recvTypeID(recv ir.Expr) ir.TypeID {
	t := fs.e.file.Types[exprType(recv)-1]
	if p, ok := t.Type.(*ir.PointerType); ok {
		return p.Elem
	}
	return exprType(recv)
}

// methodRecv emits the receiver of a method selection x.M, adjusted to
// the method's receiver: through the embedding path, then & or * so that
// its type is exactly the method's receiver type. isIface reports an
// interface method (dynamic dispatch on the returned value).
func (fs *fnState) methodRecv(x *ast.SelectorExpr, sel *types.Selection) (ir.Expr, bool) {
	e := fs.e
	info := e.cfg.Info
	m := sel.Obj().(*types.Func)
	idx := sel.Index()
	path := idx[:len(idx)-1]
	base := info.Types[x.X].Type
	have := base
	if len(path) > 0 {
		have = fieldPathType(base, path)
	}
	recvDecl := m.Signature().Recv().Type()
	isIface := types.IsInterface(recvDecl)
	_, wantPtr := types.Unalias(recvDecl).(*types.Pointer)
	_, havePtr := types.Unalias(have).Underlying().(*types.Pointer)
	if !isIface && wantPtr && !havePtr {
		// implicit &: x (or the embedded field) must be addressable; x
		// itself is when no pointer is followed on the way.
		if !sel.Indirect() {
			fs.markAddrTaken(x.X)
		}
	}
	return fs.adjustRecv(fs.expr(x.X), base, path, m, func() ir.PosID { return fs.pos(x.X) })
}

// adjustRecv adjusts receiver r of type base to method m's receiver:
// through the embedding path, then & or *.
func (fs *fnState) adjustRecv(r ir.Expr, base types.Type, path []int, m *types.Func, pos func() ir.PosID) (ir.Expr, bool) {
	e := fs.e
	have := base
	if len(path) > 0 {
		have = fieldPathType(base, path)
		r = &ir.FieldExpr{Type: e.typeID(have), Pos: pos(), X: r, Path: ints(path)}
	}
	recvDecl := m.Signature().Recv().Type()
	isIface := types.IsInterface(recvDecl)
	_, wantPtr := types.Unalias(recvDecl).(*types.Pointer)
	_, havePtr := types.Unalias(have).Underlying().(*types.Pointer)
	if _, isTP := types.Unalias(have).(*types.TypeParam); isTP {
		return r, true // dispatch through the type parameter's constraint
	}
	if isIface {
		return r, true
	}
	switch {
	case wantPtr && !havePtr:
		r = &ir.AddrExpr{Type: e.typeID(types.NewPointer(have)), Pos: pos(), X: r}
	case !wantPtr && havePtr:
		r = &ir.DerefExpr{Type: e.typeID(have.Underlying().(*types.Pointer).Elem()), Pos: pos(), X: r}
	}
	return r, false
}

// promotedMethodExpr lowers T.M, M promoted through an embedded field, to
// func(recv T, params...) results { return recv.<path>.M(params...) }:
// the IR's MethodExpr names a declared method of T itself.
func (fs *fnState) promotedMethodExpr(x *ast.SelectorExpr, sel *types.Selection) ir.Expr {
	e := fs.e
	sig := e.cfg.Info.Types[x].Type.(*types.Signature)
	m := sel.Obj().(*types.Func)
	pos := fs.pos(x)
	e.stats.Funcs++
	child := e.newFunc(fs, sig, x.Pos())
	lit := &ir.FuncLitExpr{Type: e.typeID(sig), Pos: pos}
	var args []ir.Expr
	var recv ir.Expr
	for i := range sig.Params().Len() {
		v := sig.Params().At(i)
		id := child.declare(types.NewVar(token.NoPos, e.cfg.Pkg, v.Name(), v.Type()), 1)
		child.body.Params = append(child.body.Params, id)
		ref := &ir.LocalExpr{Type: e.typeID(v.Type()), Pos: child.posAt(x.Pos(), 0), Local: id}
		if i == 0 {
			recv = ref
		} else {
			args = append(args, ref)
		}
	}
	for i := range sig.Results().Len() {
		v := sig.Results().At(i)
		child.body.Results = append(child.body.Results, child.declare(types.NewVar(token.NoPos, e.cfg.Pkg, "", v.Type()), 2))
	}
	cpos := child.posAt(x.Pos(), 0)
	idx := sel.Index()
	r, isIface := child.adjustRecv(recv, sel.Recv(), idx[:len(idx)-1], m, func() ir.PosID { return cpos })
	var rt types.Type = sig.Results()
	if sig.Results().Len() == 1 {
		rt = sig.Results().At(0).Type()
	}
	var call ir.Expr
	if isIface {
		name, pkg := methodName(m)
		call = &ir.IfaceCallExpr{Type: e.typeID(rt), Pos: cpos, Recv: r, Name: name, Pkg: pkg, Args: args, Spread: sig.Variadic()}
	} else {
		call = &ir.MethodCallExpr{Type: e.typeID(rt), Pos: cpos, Recv: r, Method: child.methodSymRef(m), RecvType: child.recvTypeID(r), Targs: fs.methodTargs(x), Args: args, Spread: sig.Variadic()}
	}
	if sig.Results().Len() > 0 {
		child.body.Body = []ir.Stmt{&ir.ReturnStmt{Pos: cpos, Results: []ir.Expr{call}}}
	} else {
		child.body.Body = []ir.Stmt{&ir.ExprStmt{Pos: cpos, X: call}}
	}
	e.stats.FuncsOK++
	lit.Body = *child.body
	return lit
}

func exprType(x ir.Expr) ir.TypeID {
	v := reflectField(x, "Type")
	return v
}

func (fs *fnState) call(x *ast.CallExpr) ir.Expr {
	e := fs.e
	info := e.cfg.Info
	typ := e.typeID(info.Types[x].Type)
	funTV := info.Types[x.Fun]
	spread := x.Ellipsis.IsValid()
	switch {
	case funTV.IsType():
		to := funTV.Type
		arg := info.Types[x.Args[0]]
		switch {
		case arg.IsNil():
			// T(nil) is the nil value of T
			return &ir.NilExpr{Type: e.typeID(to), Pos: fs.pos(x)}
		case arg.Value != nil && isTypeParam(to):
			// Conversions of a constant to a type parameter are the only
			// ones go/types does not fold (it records the operand with its
			// default type, which may not hold the value): spec "yields a
			// non-constant value of that type, with the value represented
			// as a value of the type argument" — so the IR constant has the
			// type parameter's type and is materialized per instantiation.
			return &ir.ConstExpr{Type: e.typeID(to), Pos: fs.pos(x), Value: *constValueOf(arg.Value)}
		}
		return &ir.ConvertExpr{Type: e.typeID(to), Pos: fs.pos(x), X: fs.expr(x.Args[0])}
	case funTV.IsBuiltin():
		return fs.builtinCall(x, typ)
	}
	if se, ok := unparen(x.Fun).(*ast.SelectorExpr); ok {
		if sel, ok := info.Selections[se]; ok && sel.Kind() == types.MethodVal {
			m := sel.Obj().(*types.Func)
			recv, isIface := fs.methodRecv(se, sel)
			sig := info.Types[se].Type.(*types.Signature)
			args := fs.args(x, sig)
			if isIface {
				name, pkg := methodName(m)
				return &ir.IfaceCallExpr{Type: typ, Pos: fs.opPos(x, x.Lparen), Recv: recv, Name: name, Pkg: pkg, Args: args, Spread: spread}
			}
			return &ir.MethodCallExpr{Type: typ, Pos: fs.opPos(x, x.Lparen), Recv: recv, Method: fs.methodSymRef(m), RecvType: fs.recvTypeID(recv), Targs: fs.methodTargs(se), Args: args, Spread: spread}
		}
	}
	sig, ok := coreType(funTV.Type).(*types.Signature)
	if !ok {
		e.unsupportedf(x.Pos(), "call of non-function %s", funTV.Type)
	}
	fn := fs.expr(x.Fun)
	return &ir.CallExpr{Type: typ, Pos: fs.opPos(x, x.Lparen), Fn: fn, Args: fs.args(x, sig), Spread: spread}
}

// args emits call arguments converted to the parameter types.
func (fs *fnState) args(x *ast.CallExpr, sig *types.Signature) []ir.Expr {
	params := sig.Params()
	if len(x.Args) == 1 && params.Len() > 1 {
		// f(g()) with a multi-value g
		var targets []types.Type
		for i := range params.Len() {
			targets = append(targets, params.At(i).Type())
		}
		if sig.Variadic() {
			targets = targets[:len(targets)-1]
		}
		return []ir.Expr{fs.multiExpr(x.Args[0], targets)}
	}
	if len(x.Args) == 1 && params.Len() == 1 && sig.Variadic() && !x.Ellipsis.IsValid() {
		if tup, ok := fs.e.cfg.Info.Types[x.Args[0]].Type.(*types.Tuple); ok && tup.Len() > 1 {
			elem := params.At(0).Type().(*types.Slice).Elem()
			var targets []types.Type
			for range tup.Len() {
				targets = append(targets, elem)
			}
			return []ir.Expr{fs.multiExpr(x.Args[0], targets)}
		}
	}
	out := make([]ir.Expr, len(x.Args))
	for i, a := range x.Args {
		var pt types.Type
		switch {
		case sig.Variadic() && i >= params.Len()-1:
			pt = params.At(params.Len() - 1).Type()
			if !x.Ellipsis.IsValid() {
				if s, ok := coreType(pt).(*types.Slice); ok {
					pt = s.Elem()
				}
			}
		case i < params.Len():
			pt = params.At(i).Type()
		}
		if pt != nil {
			out[i] = fs.convExpr(a, pt)
		} else {
			out[i] = fs.expr(a)
		}
	}
	return out
}

func (fs *fnState) builtinCall(x *ast.CallExpr, typ ir.TypeID) ir.Expr {
	e := fs.e
	info := e.cfg.Info
	var id *ast.Ident
	switch f := unparen(x.Fun).(type) {
	case *ast.Ident:
		id = f
	case *ast.SelectorExpr:
		id = f.Sel
	}
	b, ok := info.Uses[id].(*types.Builtin)
	if !ok {
		e.unsupportedf(x.Pos(), "builtin call")
	}
	bi, ok := builtins[b.Name()]
	if !ok {
		e.unsupportedf(x.Pos(), "builtin %s", b.Name())
	}
	call := &ir.BuiltinCallExpr{Type: typ, Pos: fs.opPos(x, x.Lparen), Builtin: bi, Spread: x.Ellipsis.IsValid()}
	args := x.Args
	switch bi {
	case ir.BuiltinMake, ir.BuiltinNew:
		call.Targ = e.typeID(info.Types[args[0]].Type)
		args = args[1:]
	}
	if len(args) == 1 && bi != ir.BuiltinPanic && bi != ir.BuiltinNew && bi != ir.BuiltinMake {
		if tup, ok := info.Types[args[0]].Type.(*types.Tuple); ok && tup.Len() > 1 {
			call.Args = []ir.Expr{fs.commaOkExpr(args[0])}
			return call
		}
	}
	for i, a := range args {
		switch {
		case bi == ir.BuiltinPanic:
			call.Args = append(call.Args, fs.convExpr(a, types.Universe.Lookup("any").Type()))
		case bi == ir.BuiltinAppend && i > 0 && !call.Spread:
			if s, ok := coreType(info.Types[args[0]].Type).(*types.Slice); ok {
				call.Args = append(call.Args, fs.convExpr(a, s.Elem()))
			} else {
				call.Args = append(call.Args, fs.expr(a))
			}
		case bi == ir.BuiltinDelete && i == 1:
			call.Args = append(call.Args, fs.indexOperand(args[0], a))
		default:
			call.Args = append(call.Args, fs.expr(a))
		}
	}
	return call
}

func (fs *fnState) compositeLit(x *ast.CompositeLit, t types.Type) ir.Expr {
	e := fs.e
	info := e.cfg.Info
	if p, ok := coreType(t).(*types.Pointer); ok {
		// elided &T{...} inside an outer literal
		inner := fs.compositeLit(x, p.Elem())
		return &ir.AddrExpr{Type: e.typeID(t), Pos: fs.pos(x), X: inner}
	}
	typ := e.typeID(t)
	switch ct := coreType(t).(type) {
	case *types.Struct:
		lit := &ir.StructLitExpr{Type: typ, Pos: fs.pos(x)}
		for i, el := range x.Elts {
			path := []int{i}
			val := el
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				name := kv.Key.(*ast.Ident).Name
				obj, idx, _ := types.LookupFieldOrMethod(ct, false, fs.e.cfg.Pkg, name)
				if _, isField := obj.(*types.Var); !isField {
					e.unsupportedf(el.Pos(), "internal: struct literal field %s", name)
				}
				path = idx
				val = kv.Value
			}
			ft := fieldPathType(t, path)
			lit.Fields = append(lit.Fields, ir.FieldInit{Path: ints(path), Value: fs.elemExpr(val, ft)})
		}
		return lit
	case *types.Array, *types.Slice:
		var elem types.Type
		if a, ok := ct.(*types.Array); ok {
			elem = a.Elem()
		} else {
			elem = ct.(*types.Slice).Elem()
		}
		lit := &ir.ArrayLitExpr{Type: typ, Pos: fs.pos(x)}
		idx := int64(0)
		for _, el := range x.Elts {
			val := el
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				k := info.Types[kv.Key].Value
				if k == nil {
					e.unsupportedf(kv.Pos(), "internal: non-constant array index")
				}
				idx, _ = constant.Int64Val(constant.ToInt(k))
				val = kv.Value
			}
			lit.Elems = append(lit.Elems, ir.Elem{Index: uint64(idx), Value: fs.elemExpr(val, elem)})
			idx++
		}
		return lit
	case *types.Map:
		lit := &ir.MapLitExpr{Type: typ, Pos: fs.pos(x)}
		for _, el := range x.Elts {
			kv := el.(*ast.KeyValueExpr)
			lit.Entries = append(lit.Entries, ir.MapEntry{Key: fs.elemExpr(kv.Key, ct.Key()), Value: fs.elemExpr(kv.Value, ct.Elem())})
		}
		return lit
	}
	e.unsupportedf(x.Pos(), "composite literal of %s", t)
	return nil
}

// elemExpr emits a composite literal element of type t; an element that
// is itself a literal with elided type takes t.
func (fs *fnState) elemExpr(x ast.Expr, t types.Type) ir.Expr {
	if cl, ok := x.(*ast.CompositeLit); ok && cl.Type == nil {
		return fs.compositeLit(cl, t)
	}
	return fs.convExpr(x, t)
}

// funcLit emits a function literal with its captures (design §3.1).
func (fs *fnState) funcLit(x *ast.FuncLit) ir.Expr {
	e := fs.e
	info := e.cfg.Info
	sig := info.Types[x].Type.(*types.Signature)
	e.stats.Funcs++
	child := e.newFunc(fs, sig, x.Pos())
	lit := &ir.FuncLitExpr{Type: e.typeID(sig), Pos: fs.pos(x)}
	// Free variables: locals of enclosing functions used inside the literal,
	// in order of first use.
	seen := map[*types.Var]bool{}
	ast.Inspect(x.Body, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		v, ok := info.Uses[id].(*types.Var)
		if !ok || v.IsField() || seen[v] || v.Pkg() != e.cfg.Pkg {
			return true
		}
		if v.Parent() == e.cfg.Pkg.Scope() || (v.Pos() >= x.Pos() && v.Pos() < x.End()) {
			return true
		}
		if _, local := fs.locals[v]; !local {
			return true // package-level var reached through Defs-less use
		}
		seen[v] = true
		outer := fs.locals[v]
		fs.body.Locals[outer].Flags |= flagCaptured
		lit.Captures = append(lit.Captures, outer)
		child.declare(v, flagCaptured)
		return true
	})
	for i := range sig.Params().Len() {
		child.body.Params = append(child.body.Params, child.declare(sig.Params().At(i), 1))
	}
	for i := range sig.Results().Len() {
		child.body.Results = append(child.body.Results, child.declare(sig.Results().At(i), 2))
	}
	child.depth = fs.depth
	child.body.Body = child.stmtList(x.Body.List)
	child.endReturn(sig, x.Body.Rbrace)
	e.stats.FuncsOK++
	lit.Body = *child.body
	return lit
}

// reflectField reads the Type field every Expr member carries.
func reflectField(x ir.Expr, name string) ir.TypeID {
	return ir.TypeID(reflect.ValueOf(x).Elem().FieldByName(name).Uint())
}
