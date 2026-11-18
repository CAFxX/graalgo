package emit

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"go/version"
	"slices"

	"graalgo-toolexec/ir"
)

// fnState is one function body (or function literal) being emitted.
type fnState struct {
	e        *emitter
	parent   *fnState
	root     *fnState
	sig      *types.Signature
	body     *ir.FuncBody
	locals   map[*types.Var]uint32
	labels   map[*types.Label]uint32
	posIdx   map[posKey]ir.PosID
	lastLine int
	syms     map[uint32]bool
	objs     map[uint32]bool
	start    token.Pos
	depth    int
	ok       bool
}

func (e *emitter) newFunc(parent *fnState, sig *types.Signature, start token.Pos) *fnState {
	fs := &fnState{
		e: e, parent: parent, sig: sig, body: &ir.FuncBody{Recv: -1},
		locals: map[*types.Var]uint32{}, labels: map[*types.Label]uint32{},
		posIdx: map[posKey]ir.PosID{}, start: start,
	}
	if parent == nil {
		fs.root = fs
		fs.syms, fs.objs = map[uint32]bool{}, map[uint32]bool{}
	} else {
		fs.root = parent.root
	}
	return fs
}

// body runs emit for a top-level body, turning an unsupported construct
// into a recorded error.
func (e *emitter) body(fs *fnState, emit func()) {
	e.stats.Funcs++
	defer func() {
		if p := recover(); p != nil {
			u, ok := p.(unsupported)
			if !ok {
				panic(p)
			}
			e.record(u)
			fs.ok = false
		}
	}()
	emit()
	fs.ok = true
	e.stats.FuncsOK++
}

func (fs *fnState) refs() (syms, objs []uint32) {
	for k := range fs.syms {
		syms = append(syms, k)
	}
	for k := range fs.objs {
		objs = append(objs, k)
	}
	slices.Sort(syms)
	slices.Sort(objs)
	return syms, objs
}

func (fs *fnState) pos(n ast.Node) ir.PosID {
	if n == nil || !n.Pos().IsValid() {
		return 0
	}
	fset := fs.e.cfg.Fset
	length := 0
	if end := n.End(); end.IsValid() {
		length = fset.PositionFor(end, false).Offset - fset.PositionFor(n.Pos(), false).Offset
	}
	return fs.posAt(n.Pos(), max(length, 0))
}

// posKey identifies a position before delta encoding.
type posKey struct {
	file           string
	line, col, len int
}

// posAt interns a position in the body's table. Lines are stored as
// deltas from the previous entry and the file as "" when it is the body's.
func (fs *fnState) posAt(p token.Pos, length int) ir.PosID {
	pp := fs.e.cfg.Fset.Position(p)
	file := fs.e.fileName(pp.Filename)
	key := posKey{file, pp.Line, pp.Column, length}
	if id, ok := fs.posIdx[key]; ok {
		return id
	}
	if len(fs.body.Positions) == 0 && fs.body.File == "" {
		fs.body.File = file
	}
	ent := ir.Pos{Line: int64(pp.Line - fs.lastLine), Col: uint64(pp.Column), Len: uint64(length)}
	if file != fs.body.File {
		ent.File = file
	}
	fs.lastLine = pp.Line
	fs.body.Positions = append(fs.body.Positions, ent)
	id := ir.PosID(len(fs.body.Positions))
	fs.posIdx[key] = id
	return id
}

func (fs *fnState) declare(v *types.Var, flags uint64) uint32 {
	if id, ok := fs.locals[v]; ok {
		return id
	}
	id := uint32(len(fs.body.Locals))
	fs.locals[v] = id
	name := v.Name()
	var pos ir.PosID
	if v.Pos().IsValid() {
		pos = fs.posAt(v.Pos(), len(name))
	}
	fs.body.Locals = append(fs.body.Locals, ir.Local{Name: name, Type: fs.e.typeID(v.Type()), Pos: pos, Flags: flags})
	return id
}

func (fs *fnState) declareParam(v *types.Var, flags uint64) uint32 { return fs.declare(v, flags) }

func (fs *fnState) local(v *types.Var, at token.Pos) uint32 {
	id, ok := fs.locals[v]
	if !ok {
		fs.e.unsupportedf(at, "internal: unresolved local %s", v.Name())
	}
	return id
}

const (
	flagCaptured  = 8
	flagAddrTaken = 16
)

// markAddrTaken flags the local variable at the root of an addressable
// expression whose address is taken (&x, x.f, x[i] of an array, implicit
// & of a method call or slice of an array).
func (fs *fnState) markAddrTaken(x ast.Expr) {
	for {
		switch y := x.(type) {
		case *ast.ParenExpr:
			x = y.X
			continue
		case *ast.SelectorExpr:
			if sel, ok := fs.e.cfg.Info.Selections[y]; ok && sel.Kind() == types.FieldVal && !sel.Indirect() {
				if _, isPtr := types.Unalias(fs.e.cfg.Info.Types[y.X].Type).Underlying().(*types.Pointer); !isPtr {
					x = y.X
					continue
				}
			}
		case *ast.IndexExpr:
			if _, isArr := fs.e.cfg.Info.Types[y.X].Type.Underlying().(*types.Array); isArr {
				x = y.X
				continue
			}
		case *ast.Ident:
			if v, ok := fs.e.cfg.Info.Uses[y].(*types.Var); ok {
				if id, ok := fs.locals[v]; ok {
					fs.body.Locals[id].Flags |= flagAddrTaken
				}
			}
		}
		return
	}
}

func (fs *fnState) globalRef(v types.Object, at token.Pos) ir.Expr {
	e := fs.e
	if v.Pkg() == e.cfg.Pkg {
		id, ok := e.objIDs[v]
		if !ok {
			e.unsupportedf(at, "internal: package object %s not in OBJECTS", v.Name())
		}
		fs.root.objs[id] = true
		return &ir.GlobalExpr{Type: e.typeID(v.Type()), Obj: id}
	}
	id := e.objSym(v)
	fs.root.syms[id] = true
	return &ir.ExtExpr{Type: e.typeID(v.Type()), Sym: id}
}

func constValueOf(v constant.Value) *ir.ConstValue {
	switch v.Kind() {
	case constant.Bool:
		if constant.BoolVal(v) {
			return &ir.ConstValue{Kind: ir.ConstKindBool, Text: "true"}
		}
		return &ir.ConstValue{Kind: ir.ConstKindBool, Text: "false"}
	case constant.String:
		return &ir.ConstValue{Kind: ir.ConstKindString, Text: constant.StringVal(v)}
	case constant.Int:
		return &ir.ConstValue{Kind: ir.ConstKindInt, Text: v.ExactString()}
	case constant.Float:
		return &ir.ConstValue{Kind: ir.ConstKindFloat, Text: v.ExactString()}
	case constant.Complex:
		return &ir.ConstValue{Kind: ir.ConstKindComplex, Text: constant.Real(v).ExactString(), Imag: constant.Imag(v).ExactString()}
	}
	return nil
}

func constValue(v constant.Value) *ir.ConstValue { return constValueOf(v) }

// perIteration reports whether loop variables of the file containing pos
// are per-iteration (go1.22 semantics).
func (fs *fnState) perIteration(pos token.Pos) bool {
	v := fs.e.cfg.Lang
	for _, f := range fs.e.cfg.Files {
		if f.FileStart <= pos && pos <= f.FileEnd {
			if fv := fs.e.cfg.Info.FileVersions[f]; fv != "" {
				v = fv
			}
		}
	}
	return v == "" || version.Compare(v, "go1.22") >= 0
}

// ---------------------------------------------------------------- statements

func (fs *fnState) stmtList(list []ast.Stmt) []ir.Stmt {
	out := make([]ir.Stmt, 0, len(list))
	for _, s := range list {
		if st := fs.stmt(s); st != nil {
			out = append(out, st)
		}
	}
	return out
}

func (fs *fnState) optStmt(s ast.Stmt) ir.Stmt {
	if s == nil {
		return nil
	}
	return fs.stmt(s)
}

func (fs *fnState) enter() {
	fs.depth++
	if fs.depth > fs.e.stats.MaxDepth {
		fs.e.stats.MaxDepth = fs.depth
	}
	if fs.depth > ir.MaxDepth {
		fs.e.unsupportedf(fs.start, "nesting deeper than %d", ir.MaxDepth)
	}
}

func (fs *fnState) leave() { fs.depth-- }

var assignOps = map[token.Token]ir.AssignOp{
	token.ASSIGN: ir.AssignOpPlain, token.ADD_ASSIGN: ir.AssignOpAdd, token.SUB_ASSIGN: ir.AssignOpSub,
	token.MUL_ASSIGN: ir.AssignOpMul, token.QUO_ASSIGN: ir.AssignOpQuo, token.REM_ASSIGN: ir.AssignOpRem,
	token.AND_ASSIGN: ir.AssignOpAnd, token.OR_ASSIGN: ir.AssignOpOr, token.XOR_ASSIGN: ir.AssignOpXor,
	token.SHL_ASSIGN: ir.AssignOpShl, token.SHR_ASSIGN: ir.AssignOpShr, token.AND_NOT_ASSIGN: ir.AssignOpAndNot,
}

func (fs *fnState) stmt(s ast.Stmt) ir.Stmt {
	fs.enter()
	defer fs.leave()
	e := fs.e
	info := e.cfg.Info
	switch s := s.(type) {
	case *ast.EmptyStmt:
		return nil
	case *ast.ExprStmt:
		return &ir.ExprStmt{Pos: fs.pos(s), X: fs.expr(s.X)}
	case *ast.AssignStmt:
		return fs.assign(s)
	case *ast.DeclStmt:
		gd := s.Decl.(*ast.GenDecl)
		if gd.Tok != token.VAR {
			return nil // constants are folded; local types live in the type table
		}
		var out []ir.Stmt
		for _, spec := range gd.Specs {
			vs := spec.(*ast.ValueSpec)
			st := &ir.VarStmt{Pos: fs.pos(vs)}
			var vars []*types.Var
			for _, n := range vs.Names {
				v := info.Defs[n].(*types.Var)
				vars = append(vars, v)
				if n.Name == "_" {
					// still a variable (evaluated, unaddressable)
				}
				st.Locals = append(st.Locals, fs.declare(v, 0))
			}
			switch {
			case len(vs.Values) == 0:
			case len(vs.Values) == len(vs.Names):
				for i, v := range vs.Values {
					st.Values = append(st.Values, fs.convExpr(v, vars[i].Type()))
				}
			default:
				var targets []types.Type
				for _, v := range vars {
					targets = append(targets, v.Type())
				}
				st.Values = []ir.Expr{fs.multiExpr(vs.Values[0], targets)}
			}
			out = append(out, st)
		}
		if len(out) == 1 {
			return out[0]
		}
		return &ir.BlockStmt{Pos: fs.pos(s), Stmts: out}
	case *ast.IncDecStmt:
		return &ir.IncDecStmt{Pos: fs.pos(s), X: fs.expr(s.X), Inc: s.Tok == token.INC}
	case *ast.SendStmt:
		ch := fs.expr(s.Chan)
		elem := coreType(info.Types[s.Chan].Type).(*types.Chan).Elem()
		return &ir.SendStmt{Pos: fs.pos(s), Ch: ch, Value: fs.convExpr(s.Value, elem)}
	case *ast.GoStmt:
		return &ir.GoStmt{Pos: fs.pos(s), Call: fs.expr(s.Call)}
	case *ast.DeferStmt:
		return &ir.DeferStmt{Pos: fs.pos(s), Call: fs.expr(s.Call)}
	case *ast.ReturnStmt:
		st := &ir.ReturnStmt{Pos: fs.pos(s)}
		res := fs.sig.Results()
		if len(s.Results) == res.Len() {
			for i, r := range s.Results {
				st.Results = append(st.Results, fs.convExpr(r, res.At(i).Type()))
			}
		} else if len(s.Results) == 1 {
			st.Results = []ir.Expr{fs.tupleExpr(s.Results[0], res)}
		}
		return st
	case *ast.BranchStmt:
		tok := map[token.Token]ir.BranchTok{token.BREAK: ir.BranchTokBreak, token.CONTINUE: ir.BranchTokContinue, token.GOTO: ir.BranchTokGoto, token.FALLTHROUGH: ir.BranchTokFallthrough}[s.Tok]
		st := &ir.BranchStmt{Pos: fs.pos(s), Tok: tok, Label: -1}
		if s.Label != nil {
			st.Label = int32(fs.label(info.Uses[s.Label].(*types.Label)))
		}
		return st
	case *ast.LabeledStmt:
		l := fs.label(info.Defs[s.Label].(*types.Label))
		inner := fs.stmt(s.Stmt)
		if inner == nil {
			inner = &ir.BlockStmt{Pos: fs.pos(s.Stmt)}
		}
		return &ir.LabeledStmt{Pos: fs.pos(s), Label: l, Stmt: inner}
	case *ast.BlockStmt:
		return &ir.BlockStmt{Pos: fs.pos(s), Stmts: fs.stmtList(s.List)}
	case *ast.IfStmt:
		return &ir.IfStmt{Pos: fs.pos(s), Init: fs.optStmt(s.Init), Cond: fs.expr(s.Cond), Then: fs.stmtList(s.Body.List), Else: fs.optStmt(s.Else)}
	case *ast.SwitchStmt:
		return fs.switchStmt(s)
	case *ast.TypeSwitchStmt:
		st := &ir.TypeSwitchStmt{Pos: fs.pos(s), Init: fs.optStmt(s.Init)}
		var ta *ast.TypeAssertExpr
		switch a := s.Assign.(type) {
		case *ast.AssignStmt:
			ta = a.Rhs[0].(*ast.TypeAssertExpr)
		case *ast.ExprStmt:
			ta = a.X.(*ast.TypeAssertExpr)
		}
		st.X = fs.expr(ta.X)
		for _, c := range s.Body.List {
			cc := c.(*ast.CaseClause)
			cl := ir.TypeClause{Pos: fs.pos(cc), IsDefault: cc.List == nil, Local: -1}
			for _, x := range cc.List {
				tv := info.Types[x]
				if tv.IsNil() {
					cl.Types = append(cl.Types, 0)
				} else {
					cl.Types = append(cl.Types, e.typeID(tv.Type))
				}
			}
			if v, ok := info.Implicits[cc].(*types.Var); ok {
				cl.Local = int32(fs.declare(v, 0))
			}
			cl.Body = fs.stmtList(cc.Body)
			st.Clauses = append(st.Clauses, cl)
		}
		return st
	case *ast.SelectStmt:
		st := &ir.SelectStmt{Pos: fs.pos(s)}
		for _, c := range s.Body.List {
			cc := c.(*ast.CommClause)
			cl := ir.CommClause{Pos: fs.pos(cc), IsDefault: cc.Comm == nil}
			if cc.Comm != nil {
				cl.Comm = fs.stmt(cc.Comm)
			}
			cl.Body = fs.stmtList(cc.Body)
			st.Clauses = append(st.Clauses, cl)
		}
		return st
	case *ast.ForStmt:
		st := &ir.ForStmt{Pos: fs.pos(s), Init: fs.optStmt(s.Init), Post: fs.optStmt(s.Post)}
		if s.Cond != nil {
			st.Cond = fs.expr(s.Cond)
		}
		if def, ok := st.Init.(*ir.DefineStmt); ok && fs.perIteration(s.Pos()) {
			st.PerIter = slices.Clone(def.Fresh)
		}
		st.Body = fs.stmtList(s.Body.List)
		return st
	case *ast.RangeStmt:
		return fs.rangeStmt(s)
	}
	fs.e.unsupportedf(s.Pos(), "statement %T", s)
	return nil
}

func (fs *fnState) label(l *types.Label) uint32 {
	if id, ok := fs.labels[l]; ok {
		return id
	}
	id := uint32(len(fs.body.Labels))
	fs.labels[l] = id
	fs.body.Labels = append(fs.body.Labels, l.Name())
	return id
}

// lhs emits an assignment target (with `_` as Blank).
func (fs *fnState) lhs(x ast.Expr) ir.Expr {
	if id, ok := x.(*ast.Ident); ok && id.Name == "_" {
		var t types.Type
		if tv, ok := fs.e.cfg.Info.Types[x]; ok {
			t = tv.Type
		}
		return &ir.BlankExpr{Type: fs.e.typeID(t), Pos: fs.pos(x)}
	}
	if id, ok := x.(*ast.Ident); ok {
		if v, ok := fs.e.cfg.Info.Defs[id].(*types.Var); ok && v != nil {
			return &ir.LocalExpr{Type: fs.e.typeID(v.Type()), Pos: fs.pos(x), Local: fs.local(v, x.Pos())}
		}
	}
	return fs.expr(x)
}

func (fs *fnState) lhsType(x ast.Expr) types.Type {
	info := fs.e.cfg.Info
	if id, ok := x.(*ast.Ident); ok {
		if id.Name == "_" {
			return nil
		}
		if v, ok := info.Defs[id].(*types.Var); ok && v != nil {
			return v.Type()
		}
	}
	return info.Types[x].Type
}

func (fs *fnState) assign(s *ast.AssignStmt) ir.Stmt {
	info := fs.e.cfg.Info
	if s.Tok == token.DEFINE {
		st := &ir.DefineStmt{Pos: fs.pos(s)}
		for _, x := range s.Lhs {
			if id, ok := x.(*ast.Ident); ok {
				if v, ok := info.Defs[id].(*types.Var); ok && v != nil {
					st.Fresh = append(st.Fresh, fs.declare(v, 0))
				}
			}
		}
		for _, x := range s.Lhs {
			st.Lhs = append(st.Lhs, fs.lhs(x))
		}
		st.Rhs = fs.assignRhs(s.Lhs, s.Rhs)
		return st
	}
	op, ok := assignOps[s.Tok]
	if !ok {
		fs.e.unsupportedf(s.Pos(), "assignment operator %s", s.Tok)
	}
	st := &ir.AssignStmt{Pos: fs.pos(s), Op: op}
	for _, x := range s.Lhs {
		st.Lhs = append(st.Lhs, fs.lhs(x))
	}
	if op == ir.AssignOpPlain {
		st.Rhs = fs.assignRhs(s.Lhs, s.Rhs)
	} else {
		st.Rhs = []ir.Expr{fs.expr(s.Rhs[0])}
		if op == ir.AssignOpShl || op == ir.AssignOpShr {
			st.Rhs[0] = fs.shiftCount(s.Rhs[0], st.Rhs[0])
		}
	}
	return st
}

func (fs *fnState) assignRhs(lhs, rhs []ast.Expr) []ir.Expr {
	if len(lhs) == len(rhs) {
		out := make([]ir.Expr, len(rhs))
		for i, r := range rhs {
			if t := fs.lhsType(lhs[i]); t != nil {
				out[i] = fs.convExpr(r, t)
			} else {
				out[i] = fs.expr(r)
			}
		}
		return out
	}
	// one multi-value rhs: a call or a comma-ok expression
	var targets []types.Type
	for _, l := range lhs {
		targets = append(targets, fs.lhsType(l))
	}
	return []ir.Expr{fs.multiExpr(rhs[0], targets)}
}

// multiExpr emits a multi-valued rhs (call or comma-ok form) assigned to
// targets (nil entries: blank, no conversion), wrapped in TupleConvert when
// an element needs an implicit conversion.
func (fs *fnState) multiExpr(x ast.Expr, targets []types.Type) ir.Expr {
	out := fs.commaOkExpr(x)
	tup, ok := fs.e.cfg.Info.Types[x].Type.(*types.Tuple)
	if !ok {
		return out
	}
	conv := false
	vars := make([]*types.Var, tup.Len())
	for i := range tup.Len() {
		t := tup.At(i).Type()
		if i < len(targets) && targets[i] != nil && needsConversion(t, targets[i]) {
			t, conv = targets[i], true
		}
		vars[i] = types.NewVar(token.NoPos, nil, "", t)
	}
	if !conv {
		return out
	}
	return &ir.TupleConvertExpr{Type: fs.e.typeID(types.NewTuple(vars...)), Pos: fs.pos(x), X: out}
}

func (fs *fnState) tupleExpr(x ast.Expr, results *types.Tuple) ir.Expr {
	var targets []types.Type
	for i := range results.Len() {
		targets = append(targets, results.At(i).Type())
	}
	return fs.multiExpr(x, targets)
}

// commaOkExpr emits x with its comma-ok flag set when it is v, ok := m[k],
// x.(T) or <-ch in a two-value context.
func (fs *fnState) commaOkExpr(x ast.Expr) ir.Expr {
	out := fs.expr(x)
	switch y := out.(type) {
	case *ir.IndexExpr:
		y.CommaOk = true
	case *ir.TypeAssertExpr:
		y.CommaOk = true
	case *ir.RecvExpr:
		y.CommaOk = true
	}
	return out
}

// ---------------------------------------------------------------- conversions

func coreType(t types.Type) types.Type {
	t = types.Unalias(t)
	tp, ok := t.(*types.TypeParam)
	if !ok {
		return t.Underlying()
	}
	var core types.Type
	iface := tp.Underlying().(*types.Interface)
	for i := range iface.NumEmbeddeds() {
		u, ok := iface.EmbeddedType(i).(*types.Union)
		if !ok {
			if ct := coreType(iface.EmbeddedType(i)); ct != nil {
				if _, isIface := ct.(*types.Interface); !isIface {
					core = ct
				}
			}
			continue
		}
		for j := range u.Len() {
			ut := u.Term(j).Type().Underlying()
			if core == nil {
				core = ut
			} else if !types.Identical(core, ut) {
				return nil
			}
		}
	}
	return core
}

// needsConversion reports whether assigning a value of type from to a
// location of type to needs an explicit conversion node: to an interface
// (boxing or itab change) or between channel types of different direction.
func needsConversion(from, to types.Type) bool {
	if from == nil || to == nil || types.Identical(from, to) {
		return false
	}
	if types.IsInterface(to) {
		if _, isTP := types.Unalias(to).(*types.TypeParam); isTP {
			return false
		}
		return true
	}
	_, fc := from.Underlying().(*types.Chan)
	_, tc := to.Underlying().(*types.Chan)
	return fc && tc
}

// convExpr emits x as a value assigned to a location of type to.
func (fs *fnState) convExpr(x ast.Expr, to types.Type) ir.Expr {
	out := fs.expr(x)
	tv := fs.e.cfg.Info.Types[x]
	if n, ok := out.(*ir.NilExpr); ok {
		n.Type = fs.e.typeID(to)
		return out
	}
	if needsConversion(tv.Type, to) {
		return &ir.ConvertExpr{Type: fs.e.typeID(to), Pos: fs.pos(x), X: out, Implicit: true}
	}
	return out
}

// compareOperand emits a switch case value compared against tag type t.
func (fs *fnState) compareOperand(x ast.Expr, t types.Type) ir.Expr {
	if fs.e.cfg.Info.Types[x].IsNil() {
		return &ir.NilExpr{Type: fs.e.typeID(t), Pos: fs.pos(x)}
	}
	if types.IsInterface(t) && !types.IsInterface(fs.e.cfg.Info.Types[x].Type) {
		return fs.convExpr(x, t)
	}
	return fs.expr(x)
}

// switchStmt emits an expression switch. A case of interface type against
// a tag of non-interface type compares the tag converted to the case's
// type; the IR switch compares case values with the tag as is, so such a
// switch is lowered to a tagless one over a fresh local holding the tag.
func (fs *fnState) switchStmt(s *ast.SwitchStmt) ir.Stmt {
	info := fs.e.cfg.Info
	st := &ir.SwitchStmt{Pos: fs.pos(s), Init: fs.optStmt(s.Init)}
	var tagType types.Type
	if s.Tag != nil {
		tagType = concrete(info.Types[s.Tag].Type)
	}
	lower := false
	if tagType != nil && !types.IsInterface(tagType) {
		for _, c := range s.Body.List {
			for _, x := range c.(*ast.CaseClause).List {
				lower = lower || types.IsInterface(info.Types[x].Type)
			}
		}
	}
	var tmp ir.Expr
	if lower {
		v := types.NewVar(token.NoPos, fs.e.cfg.Pkg, "·tag", tagType)
		id := fs.declare(v, 0)
		tmp = &ir.LocalExpr{Type: fs.e.typeID(tagType), Pos: fs.pos(s.Tag), Local: id}
		def := &ir.DefineStmt{Pos: fs.pos(s.Tag), Lhs: []ir.Expr{tmp}, Rhs: []ir.Expr{fs.expr(s.Tag)}, Fresh: []uint32{id}}
		if st.Init != nil {
			st.Init = &ir.BlockStmt{Pos: fs.pos(s), Stmts: []ir.Stmt{st.Init, def}}
		} else {
			st.Init = def
		}
	} else if s.Tag != nil {
		st.Tag = fs.expr(s.Tag)
	}
	boolT := fs.e.typeID(types.Typ[types.Bool])
	for _, c := range s.Body.List {
		cc := c.(*ast.CaseClause)
		cl := ir.CaseClause{Pos: fs.pos(cc), IsDefault: cc.List == nil}
		for _, x := range cc.List {
			switch {
			case lower:
				xt := info.Types[x].Type
				var l ir.Expr = tmp
				r := fs.expr(x)
				if types.IsInterface(xt) {
					l = &ir.ConvertExpr{Type: fs.e.typeID(xt), Pos: fs.pos(x), X: tmp, Implicit: true}
				}
				cl.Exprs = append(cl.Exprs, &ir.BinaryExpr{Type: boolT, Pos: fs.pos(x), Op: ir.BinOpEql, X: l, Y: r})
			case tagType != nil:
				cl.Exprs = append(cl.Exprs, fs.compareOperand(x, tagType))
			default:
				cl.Exprs = append(cl.Exprs, fs.expr(x))
			}
		}
		cl.Body = fs.stmtList(cc.Body)
		st.Clauses = append(st.Clauses, cl)
	}
	return st
}

// rangeTypes returns the iteration value types of ranging over a value of
// type t (nil where the form has no such value).
func rangeTypes(t types.Type) (key, val types.Type) {
	switch u := coreType(t).(type) {
	case *types.Basic:
		if u.Info()&types.IsString != 0 {
			return types.Typ[types.Int], types.Universe.Lookup("rune").Type()
		}
		return t, nil // range over an integer
	case *types.Array:
		return types.Typ[types.Int], u.Elem()
	case *types.Pointer:
		return types.Typ[types.Int], coreType(u.Elem()).(*types.Array).Elem()
	case *types.Slice:
		return types.Typ[types.Int], u.Elem()
	case *types.Map:
		return u.Key(), u.Elem()
	case *types.Chan:
		return u.Elem(), nil
	case *types.Signature:
		yield := coreType(u.Params().At(0).Type()).(*types.Signature).Params()
		if yield.Len() > 0 {
			key = yield.At(0).Type()
		}
		if yield.Len() > 1 {
			val = yield.At(1).Type()
		}
		return key, val
	}
	return nil, nil
}

func (fs *fnState) rangeStmt(s *ast.RangeStmt) ir.Stmt {
	info := fs.e.cfg.Info
	st := &ir.RangeStmt{Pos: fs.pos(s), X: fs.expr(s.X), Define: s.Tok == token.DEFINE}
	if st.Define {
		for _, x := range []ast.Expr{s.Key, s.Value} {
			id, ok := x.(*ast.Ident)
			if !ok {
				continue
			}
			if v, ok := info.Defs[id].(*types.Var); ok && v != nil {
				st.Fresh = append(st.Fresh, fs.declare(v, 0))
			}
		}
		st.PerIter = fs.perIteration(s.Pos())
	}
	// for k, v = range x where an iteration value needs an implicit
	// conversion to its target (interface, channel direction): the IR range
	// assigns iteration values as they are, so this is lowered to
	// for k', v' := range x { k, v = T(k'), T(v'); body } — the spec's
	// per-iteration assignment, with the conversions explicit.
	var pre ir.Stmt
	if !st.Define && s.Tok == token.ASSIGN {
		kt, vt := rangeTypes(info.Types[s.X].Type)
		conv := false
		for i, x := range []ast.Expr{s.Key, s.Value} {
			from := []types.Type{kt, vt}[i]
			if x != nil && from != nil && needsConversion(from, fs.lhsType(x)) {
				conv = true
			}
		}
		if conv {
			as := &ir.AssignStmt{Pos: fs.pos(s), Op: ir.AssignOpPlain}
			for i, x := range []ast.Expr{s.Key, s.Value} {
				from := []types.Type{kt, vt}[i]
				if x == nil {
					continue
				}
				v := types.NewVar(token.NoPos, fs.e.cfg.Pkg, []string{"·key", "·value"}[i], from)
				id := fs.declare(v, 0)
				tmp := &ir.LocalExpr{Type: fs.e.typeID(from), Pos: fs.pos(x), Local: id}
				st.Fresh = append(st.Fresh, id)
				if i == 0 {
					st.Key = tmp
				} else {
					st.Value = tmp
				}
				var rhs ir.Expr = tmp
				if to := fs.lhsType(x); to != nil && needsConversion(from, to) {
					rhs = &ir.ConvertExpr{Type: fs.e.typeID(to), Pos: fs.pos(x), X: tmp, Implicit: true}
				}
				as.Lhs = append(as.Lhs, fs.lhs(x))
				as.Rhs = append(as.Rhs, rhs)
			}
			st.Define = true
			pre = as
		}
	}
	if pre == nil {
		if s.Key != nil {
			st.Key = fs.lhs(s.Key)
		}
		if s.Value != nil {
			st.Value = fs.lhs(s.Value)
		}
	}
	st.Body = fs.stmtList(s.Body.List)
	if pre != nil {
		st.Body = append([]ir.Stmt{pre}, st.Body...)
	}
	return st
}
