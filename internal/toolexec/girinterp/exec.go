package girinterp

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"graalgo-toolexec/ir"
)

type ctrl int

const (
	cNormal ctrl = iota
	cBreak
	cContinue
	cReturn
	cGoto
	cFallthrough
)

type flow struct {
	c     ctrl
	label int32 // -1: unlabeled
}

var normal = flow{c: cNormal, label: -1}

// tuple is a multi-value result.
type tuple []Value

func (in *Interp) execBody(fr *frame) {
	f := in.execList(fr, fr.body.Body)
	switch f.c {
	case cNormal, cReturn:
	default:
		panic(interpError(fmt.Sprintf("control flow %d escaped the function", f.c)))
	}
}

func (in *Interp) step() {
	in.steps++
	if in.steps > in.MaxSteps {
		panic(interpError("step limit exceeded"))
	}
}

func (in *Interp) execList(fr *frame, list []ir.Stmt) flow {
	for i := 0; i < len(list); i++ {
		f := in.exec(fr, list[i], -1)
		if f.c == cGoto {
			if j := findLabel(list, f.label); j >= 0 {
				i = j - 1
				continue
			}
		}
		if f.c != cNormal {
			return f
		}
	}
	return normal
}

func findLabel(list []ir.Stmt, l int32) int {
	for i, s := range list {
		if ls, ok := s.(*ir.LabeledStmt); ok && int32(ls.Label) == l {
			return i
		}
	}
	return -1
}

func (in *Interp) exec(fr *frame, s ir.Stmt, label int32) flow {
	in.step()
	switch s := s.(type) {
	case *ir.ExprStmt:
		fr.pos = s.Pos
		in.eval(fr, s.X)
	case *ir.AssignStmt:
		fr.pos = s.Pos
		in.assign(fr, s)
	case *ir.DefineStmt:
		fr.pos = s.Pos
		in.define(fr, s.Lhs, s.Rhs, s.Fresh)
	case *ir.VarStmt:
		fr.pos = s.Pos
		var vals []Value
		switch {
		case len(s.Values) == len(s.Locals):
			for _, x := range s.Values {
				vals = append(vals, in.eval(fr, x))
			}
		case len(s.Values) == 1:
			vals = in.eval(fr, s.Values[0]).(tuple)
		}
		for i, l := range s.Locals {
			var v Value
			if vals != nil {
				v = vals[i]
			}
			fr.declare(in, l, v)
		}
	case *ir.IncDecStmt:
		fr.pos = s.Pos
		pl := in.place(fr, s.X, false)
		v := pl.load(in)
		var one Value
		if i, ok := asInt64(v); ok {
			_ = i
			one = in.convert(int(1), in.basic(KInt), in.typeOfValue(v))
		} else {
			one = in.convert(float64(1), in.basic(KFloat64), in.typeOfValue(v))
		}
		op := ir.BinOpAdd
		if !s.Inc {
			op = ir.BinOpSub
		}
		pl.store(in, in.arith(op, v, one))
	case *ir.SendStmt:
		fr.pos = s.Pos
		ch := in.eval(fr, s.Ch).(*Chan)
		v := in.eval(fr, s.Value)
		in.send(ch, v)
	case *ir.GoStmt:
		panic(interpError("go statements are not supported"))
	case *ir.DeferStmt:
		fr.pos = s.Pos
		fr.defers = append(fr.defers, in.deferThunk(fr, s.Call))
	case *ir.ReturnStmt:
		fr.pos = s.Pos
		res := fr.body.Results
		switch {
		case len(s.Results) == len(res):
			vals := make([]Value, len(res))
			for i, x := range s.Results {
				vals[i] = in.eval(fr, x)
			}
			for i, l := range res {
				store(fr.slot(l), vals[i])
			}
		case len(s.Results) == 1:
			vals := in.eval(fr, s.Results[0]).(tuple)
			for i, l := range res {
				store(fr.slot(l), vals[i])
			}
		case len(s.Results) != 0:
			panic(interpError("return arity"))
		}
		return flow{c: cReturn, label: -1}
	case *ir.BranchStmt:
		fr.pos = s.Pos
		switch s.Tok {
		case ir.BranchTokBreak:
			return flow{c: cBreak, label: s.Label}
		case ir.BranchTokContinue:
			return flow{c: cContinue, label: s.Label}
		case ir.BranchTokGoto:
			return flow{c: cGoto, label: s.Label}
		case ir.BranchTokFallthrough:
			return flow{c: cFallthrough, label: -1}
		}
	case *ir.LabeledStmt:
		f := in.exec(fr, s.Stmt, int32(s.Label))
		if f.c == cBreak && f.label == int32(s.Label) {
			return normal
		}
		return f
	case *ir.BlockStmt:
		return in.execList(fr, s.Stmts)
	case *ir.IfStmt:
		fr.pos = s.Pos
		if s.Init != nil {
			if f := in.exec(fr, s.Init, -1); f.c != cNormal {
				return f
			}
		}
		if in.eval(fr, s.Cond).(bool) {
			return in.execList(fr, s.Then)
		} else if s.Else != nil {
			return in.exec(fr, s.Else, -1)
		}
	case *ir.SwitchStmt:
		return in.execSwitch(fr, s, label)
	case *ir.TypeSwitchStmt:
		return in.execTypeSwitch(fr, s, label)
	case *ir.SelectStmt:
		return in.execSelect(fr, s, label)
	case *ir.ForStmt:
		return in.execFor(fr, s, label)
	case *ir.RangeStmt:
		return in.execRange(fr, s, label)
	default:
		panic(interpError(fmt.Sprintf("statement %T", s)))
	}
	return normal
}

// loopBody runs a loop body and reports whether the loop continues, and
// the flow to propagate when it does not.
func loopBody(f flow, label int32) (cont bool, out flow) {
	switch f.c {
	case cNormal:
		return true, normal
	case cBreak:
		if f.label < 0 || f.label == label {
			return false, normal
		}
		return false, f
	case cContinue:
		if f.label < 0 || f.label == label {
			return true, normal
		}
		return false, f
	}
	return false, f
}

func (in *Interp) execFor(fr *frame, s *ir.ForStmt, label int32) flow {
	fr.pos = s.Pos
	if s.Init != nil {
		in.exec(fr, s.Init, -1)
	}
	for {
		in.step()
		if s.Cond != nil && !in.eval(fr, s.Cond).(bool) {
			return normal
		}
		cont, out := loopBody(in.execList(fr, s.Body), label)
		if !cont {
			return out
		}
		// go1.22: each iteration has its own copy of the loop variables,
		// made before the post statement.
		for _, l := range s.PerIter {
			if c := fr.cells[l]; c != nil {
				n := new(Value)
				*n = copyVal(*c)
				fr.cells[l] = n
			}
		}
		if s.Post != nil {
			in.exec(fr, s.Post, -1)
		}
	}
}

func (in *Interp) execRange(fr *frame, s *ir.RangeStmt, label int32) flow {
	fr.pos = s.Pos
	xt := in.typeOf(fr, s.X)
	x := in.eval(fr, s.X)
	if !s.PerIter && s.Define {
		for _, l := range s.Fresh {
			fr.declare(in, l, nil)
		}
	}
	// iter assigns one iteration's values and runs the body.
	iter := func(k, v Value) (bool, flow) {
		in.step()
		if s.Define && s.PerIter {
			for _, l := range s.Fresh {
				fr.declare(in, l, nil)
			}
		}
		if s.Key != nil {
			in.place(fr, s.Key, false).store(in, k)
		}
		if s.Value != nil {
			in.place(fr, s.Value, false).store(in, v)
		}
		return loopBody(in.execList(fr, s.Body), label)
	}
	u := xt.Under()
	switch u.Kind {
	case KInt, KInt8, KInt16, KInt32, KInt64, KUint, KUint8, KUint16, KUint32, KUint64, KUintptr:
		n, _ := asInt64(x)
		for i := int64(0); i < n; i++ {
			if cont, out := iter(fromInt64(i, false, u.Kind), nil); !cont {
				return out
			}
		}
	case KString:
		str := x.(string)
		for i := 0; i < len(str); {
			r, w := utf8.DecodeRuneInString(str[i:])
			if cont, out := iter(i, int32(r)); !cont {
				return out
			}
			i += w
		}
	case KArray, KPointer:
		var arr *Array
		if u.Kind == KPointer {
			p := x.(*Value)
			if p == nil {
				if s.Value != nil {
					in.runtimeNilDeref()
				}
				n := u.Elem.Under().Len
				for i := int64(0); i < n; i++ {
					if cont, out := iter(int(i), nil); !cont {
						return out
					}
				}
				return normal
			}
			arr = (*p).(*Array)
		} else {
			arr = copyVal(x).(*Array) // the range expression is a copy
		}
		for i := range arr.E {
			if cont, out := iter(i, arr.E[i]); !cont {
				return out
			}
		}
	case KSlice:
		sl := x.(Slice)
		for i := 0; i < sl.Len; i++ {
			if cont, out := iter(i, sl.A[sl.Off+i]); !cont {
				return out
			}
		}
	case KMap:
		m := x.(*Map)
		if m == nil {
			return normal
		}
		n := len(m.keys)
		for i := 0; i < n; i++ {
			if !m.live[i] {
				continue
			}
			if cont, out := iter(m.keys[i], m.vals[i]); !cont {
				return out
			}
		}
	case KChan:
		ch := x.(*Chan)
		for {
			v, ok := in.recv(ch, u.Elem)
			if !ok {
				return normal
			}
			if cont, out := iter(v, nil); !cont {
				return out
			}
		}
	case KFunc:
		return in.rangeFunc(fr, x.(*Closure), u, iter)
	default:
		panic(interpError("range over " + xt.Str))
	}
	return normal
}

// rangeFunc runs a range-over-func loop: the body runs inside yield.
func (in *Interp) rangeFunc(fr *frame, seq *Closure, sig *RType, iter func(k, v Value) (bool, flow)) flow {
	exit := normal
	done := false
	yield := &Closure{native: func(in *Interp, args []Value) []Value {
		if done {
			in.runtimePanic("range function continued iteration after function for loop body returned false")
		}
		var k, v Value
		if len(args) > 0 {
			k = args[0]
		}
		if len(args) > 1 {
			v = args[1]
		}
		cont, out := iter(k, v)
		if !cont {
			done = true
			exit = out
		}
		return []Value{cont}
	}}
	in.call(seq, []Value{yield})
	done = true
	return exit
}

func (in *Interp) execSwitch(fr *frame, s *ir.SwitchStmt, label int32) flow {
	fr.pos = s.Pos
	if s.Init != nil {
		in.exec(fr, s.Init, -1)
	}
	var tag Value = true
	tagT := in.basic(KBool)
	if s.Tag != nil {
		tag = in.eval(fr, s.Tag)
		tagT = in.typeOf(fr, s.Tag)
	}
	match := -1
	for i, cl := range s.Clauses {
		for _, x := range cl.Exprs {
			if in.equal(tag, in.eval(fr, x), tagT) {
				match = i
				break
			}
		}
		if match >= 0 {
			break
		}
	}
	if match < 0 {
		for i, cl := range s.Clauses {
			if cl.IsDefault {
				match = i
			}
		}
	}
	if match < 0 {
		return normal
	}
	for i := match; i < len(s.Clauses); i++ {
		f := in.execList(fr, s.Clauses[i].Body)
		if f.c == cFallthrough {
			continue
		}
		if f.c == cBreak && (f.label < 0 || f.label == label) {
			return normal
		}
		return f
	}
	return normal
}

func (in *Interp) execTypeSwitch(fr *frame, s *ir.TypeSwitchStmt, label int32) flow {
	fr.pos = s.Pos
	if s.Init != nil {
		in.exec(fr, s.Init, -1)
	}
	x := in.eval(fr, s.X).(Iface)
	match := -1
	single := false
	for i, cl := range s.Clauses {
		for _, tid := range cl.Types {
			var ok bool
			if tid == 0 {
				ok = x.T == nil
			} else {
				ok = in.hasType(x, fr.pkg.typ(tid, fr.env))
			}
			if ok {
				match, single = i, len(cl.Types) == 1 && tid != 0
				break
			}
		}
		if match >= 0 {
			break
		}
	}
	if match < 0 {
		for i, cl := range s.Clauses {
			if cl.IsDefault {
				match = i
			}
		}
	}
	if match < 0 {
		return normal
	}
	cl := &s.Clauses[match]
	if cl.Local >= 0 {
		lt := fr.pkg.typ(fr.body.Locals[cl.Local].Type, fr.env)
		var v Value = x
		if single && lt.Under().Kind != KInterface {
			v = copyVal(x.V)
		}
		fr.declare(in, uint32(cl.Local), v)
	}
	f := in.execList(fr, cl.Body)
	if f.c == cBreak && (f.label < 0 || f.label == label) {
		return normal
	}
	return f
}

// hasType reports whether interface value x holds type t (or implements
// interface t).
func (in *Interp) hasType(x Iface, t *RType) bool {
	if x.T == nil {
		return false
	}
	if t.Under().Kind == KInterface {
		ok, _ := in.implements(x.T, t)
		return ok
	}
	return x.T == t
}

func (in *Interp) execSelect(fr *frame, s *ir.SelectStmt, label int32) flow {
	fr.pos = s.Pos
	type cas struct {
		ch   *Chan
		send bool
		val  Value
		et   *RType
	}
	cases := make([]cas, len(s.Clauses))
	def := -1
	for i, cl := range s.Clauses {
		if cl.IsDefault {
			def = i
			continue
		}
		switch c := cl.Comm.(type) {
		case *ir.SendStmt:
			cases[i] = cas{ch: in.eval(fr, c.Ch).(*Chan), send: true, val: in.eval(fr, c.Value)}
		default:
			r := recvOf(cl.Comm)
			cases[i] = cas{ch: in.eval(fr, r.Ch).(*Chan), et: in.typeOf(fr, r.Ch).Under().Elem}
		}
	}
	chosen := -1
	for i, c := range cases {
		if i == def || c.ch == nil {
			continue
		}
		if c.send && (c.ch.closed || len(c.ch.buf) < c.ch.cap) || !c.send && (len(c.ch.buf) > 0 || c.ch.closed) {
			chosen = i
			break
		}
	}
	if chosen < 0 {
		if def < 0 {
			panic(&fatalError{msg: "all goroutines are asleep - deadlock!", wait: "select"})
		}
		chosen = def
	}
	cl := &s.Clauses[chosen]
	if chosen != def {
		c := cases[chosen]
		if c.send {
			fr.pos = cl.Comm.(*ir.SendStmt).Pos
			in.send(c.ch, c.val)
		} else {
			v, ok := in.recv(c.ch, c.et)
			in.recvPending = tuple{v, ok}
			switch cm := cl.Comm.(type) {
			case *ir.ExprStmt:
			case *ir.AssignStmt:
				in.assign(fr, cm)
			case *ir.DefineStmt:
				in.define(fr, cm.Lhs, cm.Rhs, cm.Fresh)
			}
			in.recvPending = nil
		}
	}
	f := in.execList(fr, cl.Body)
	if f.c == cBreak && (f.label < 0 || f.label == label) {
		return normal
	}
	return f
}

// recvOf finds the receive expression of a select receive clause (under
// any implicit conversion of its assignment).
func recvOf(s ir.Stmt) *ir.RecvExpr {
	var x ir.Expr
	switch c := s.(type) {
	case *ir.ExprStmt:
		x = c.X
	case *ir.AssignStmt:
		x = c.Rhs[0]
	case *ir.DefineStmt:
		x = c.Rhs[0]
	}
	for {
		switch y := x.(type) {
		case *ir.RecvExpr:
			return y
		case *ir.ConvertExpr:
			x = y.X
		case *ir.TupleConvertExpr:
			x = y.X
		default:
			panic(interpError(fmt.Sprintf("select receive clause %T", x)))
		}
	}
}

func (in *Interp) send(ch *Chan, v Value) {
	if ch == nil {
		panic(&fatalError{msg: "all goroutines are asleep - deadlock!", wait: "chan send (nil chan)"})
	}
	if ch.closed {
		in.panicValue(Iface{T: in.plainErrType(), V: "send on closed channel"})
	}
	if len(ch.buf) >= ch.cap {
		panic(&fatalError{msg: "all goroutines are asleep - deadlock!", wait: "chan send"})
	}
	ch.buf = append(ch.buf, copyVal(v))
}

func (in *Interp) recv(ch *Chan, et *RType) (Value, bool) {
	if ch == nil {
		panic(&fatalError{msg: "all goroutines are asleep - deadlock!", wait: "chan receive (nil chan)"})
	}
	if len(ch.buf) > 0 {
		v := ch.buf[0]
		ch.buf = ch.buf[1:]
		return v, true
	}
	if ch.closed {
		return in.zero(et), false
	}
	panic(&fatalError{msg: "all goroutines are asleep - deadlock!", wait: "chan receive"})
}

// ---------------------------------------------------------------- assignment

// place is an assignable location.
type place struct {
	slot  *Value
	m     *Map
	mkey  Value
	mt    *RType
	blank bool
}

func (pl place) load(in *Interp) Value {
	if pl.mt != nil {
		v, ok := pl.m.lookup(in, pl.mkey, pl.mt.Under().KeyT)
		if !ok {
			return in.zero(pl.mt.Under().Elem)
		}
		return v
	}
	return *pl.slot
}

func (pl place) store(in *Interp, v Value) {
	switch {
	case pl.blank:
	case pl.mt != nil:
		if pl.m == nil {
			in.panicValue(Iface{T: in.plainErrType(), V: "assignment to entry in nil map"})
		}
		pl.m.set(in, pl.mkey, pl.mt.Under().KeyT, v)
	default:
		store(pl.slot, v)
	}
}

// place evaluates an addressable expression (or map index / blank). addr
// requests a real address: then a local must be flagged.
func (in *Interp) place(fr *frame, x ir.Expr, addr bool) place {
	switch x := x.(type) {
	case *ir.BlankExpr:
		return place{blank: true}
	case *ir.LocalExpr:
		if addr {
			return place{slot: fr.addrOfLocal(x.Local)}
		}
		return place{slot: fr.slot(x.Local)}
	case *ir.GlobalExpr:
		return place{slot: fr.pkg.global(x.Obj)}
	case *ir.ExtExpr:
		p, i := in.extObj(fr.pkg, x.Sym)
		return place{slot: p.global(uint32(i))}
	case *ir.DerefExpr:
		fr.pos = x.Pos
		p := in.eval(fr, x.X).(*Value)
		if p == nil {
			in.runtimeNilDeref()
		}
		return place{slot: p}
	case *ir.FieldExpr:
		xt := in.typeOf(fr, x.X)
		var slot *Value
		if xt.Under().Kind == KPointer {
			fr.pos = x.Pos
			p := in.eval(fr, x.X).(*Value)
			if p == nil {
				in.runtimeNilDeref()
			}
			slot = p
		} else {
			slot = in.place(fr, x.X, addr).slot
		}
		for _, idx := range x.Path {
			if p, ok := (*slot).(*Value); ok {
				if p == nil {
					in.runtimeNilDeref()
				}
				slot = p
			}
			slot = &(*slot).(*Struct).F[idx]
		}
		return place{slot: slot}
	case *ir.IndexExpr:
		xt := in.typeOf(fr, x.X).Under()
		switch xt.Kind {
		case KMap:
			m := in.eval(fr, x.X).(*Map)
			k := in.eval(fr, x.Index)
			return place{m: m, mkey: k, mt: xt}
		case KArray:
			base := in.place(fr, x.X, addr).slot
			i := in.eval(fr, x.Index)
			fr.pos = x.Pos
			arr := (*base).(*Array)
			return place{slot: &arr.E[in.index(i, len(arr.E))]}
		case KPointer:
			p := in.eval(fr, x.X).(*Value)
			i := in.eval(fr, x.Index)
			fr.pos = x.Pos
			if p == nil {
				in.runtimeNilDeref()
			}
			arr := (*p).(*Array)
			return place{slot: &arr.E[in.index(i, len(arr.E))]}
		case KSlice:
			s := in.eval(fr, x.X).(Slice)
			i := in.eval(fr, x.Index)
			fr.pos = x.Pos
			return place{slot: &s.A[s.Off+in.index(i, s.Len)]}
		}
	case *ir.ConvertExpr:
		// a conversion between identical underlying types keeps
		// addressability only for pointers; not an lvalue otherwise
	}
	panic(interpError(fmt.Sprintf("not addressable: %T", x)))
}

func (in *Interp) index(i Value, n int) int {
	v, _ := asInt64(i)
	if isUnsignedVal(i) && uint64(v) >= uint64(n) || !isUnsignedVal(i) && (v < 0 || v >= int64(n)) {
		if !isUnsignedVal(i) && v < 0 {
			in.runtimePanic("index out of range [" + strconv.FormatInt(v, 10) + "]")
		}
		in.runtimePanic("index out of range [" + formatScalar(i) + "] with length " + strconv.Itoa(n))
	}
	return int(v)
}

func (in *Interp) assign(fr *frame, s *ir.AssignStmt) {
	if s.Op != ir.AssignOpPlain {
		pl := in.place(fr, s.Lhs[0], false)
		y := in.eval(fr, s.Rhs[0])
		v := pl.load(in)
		fr.pos = s.Pos
		var r Value
		switch s.Op {
		case ir.AssignOpShl:
			r = in.shift(v, y, true)
		case ir.AssignOpShr:
			r = in.shift(v, y, false)
		default:
			r = in.arith(assignBinOp[s.Op], v, y)
		}
		pl.store(in, r)
		return
	}
	pls := make([]place, len(s.Lhs))
	for i, l := range s.Lhs {
		pls[i] = in.place(fr, l, false)
	}
	vals := in.evalRhs(fr, s.Rhs, len(s.Lhs))
	for i, pl := range pls {
		pl.store(in, vals[i])
	}
}

var assignBinOp = map[ir.AssignOp]ir.BinOp{
	ir.AssignOpAdd: ir.BinOpAdd, ir.AssignOpSub: ir.BinOpSub, ir.AssignOpMul: ir.BinOpMul,
	ir.AssignOpQuo: ir.BinOpQuo, ir.AssignOpRem: ir.BinOpRem, ir.AssignOpAnd: ir.BinOpAnd,
	ir.AssignOpOr: ir.BinOpOr, ir.AssignOpXor: ir.BinOpXor, ir.AssignOpAndNot: ir.BinOpAndNot,
}

func (in *Interp) evalRhs(fr *frame, rhs []ir.Expr, n int) []Value {
	if len(rhs) == n {
		vals := make([]Value, n)
		for i, x := range rhs {
			vals[i] = copyVal(in.eval(fr, x))
		}
		return vals
	}
	t := in.eval(fr, rhs[0]).(tuple)
	if len(t) != n {
		panic(interpError("multi-value arity"))
	}
	return t
}

func (in *Interp) define(fr *frame, lhs, rhs []ir.Expr, fresh []uint32) {
	isFresh := map[uint32]bool{}
	for _, l := range fresh {
		isFresh[l] = true
	}
	pls := make([]place, len(lhs))
	for i, l := range lhs {
		if lx, ok := l.(*ir.LocalExpr); ok && isFresh[lx.Local] {
			continue
		}
		pls[i] = in.place(fr, l, false)
	}
	vals := in.evalRhs(fr, rhs, len(lhs))
	for _, l := range fresh {
		fr.declare(in, l, nil)
	}
	for i, l := range lhs {
		if lx, ok := l.(*ir.LocalExpr); ok && isFresh[lx.Local] {
			store(fr.slot(lx.Local), vals[i])
			continue
		}
		pls[i].store(in, vals[i])
	}
}

// ---------------------------------------------------------------- defer

func (in *Interp) deferThunk(fr *frame, call ir.Expr) func() {
	switch c := call.(type) {
	case *ir.CallExpr:
		fn := in.eval(fr, c.Fn).(*Closure)
		args := in.callArgs(fr, c.Args, c.Spread, in.typeOf(fr, c.Fn).Under())
		return func() { in.deferredCall(func() { in.call(fn, args) }) }
	case *ir.MethodCallExpr:
		cl, recv := in.methodTarget(fr, c.Method, c.RecvType, c.Targs, c.Recv)
		args := in.callArgs(fr, c.Args, c.Spread, cl.sig)
		return func() { in.deferredCall(func() { in.call(cl, append([]Value{recv}, args...)) }) }
	case *ir.IfaceCallExpr:
		m, v, t := in.ifaceTarget(fr, c.Recv, c.Name, c.Pkg)
		args := in.callArgs(fr, c.Args, c.Spread, m.sig)
		return func() { in.deferredCall(func() { in.callMethod(m, v, t, args) }) }
	case *ir.BuiltinCallExpr:
		args := in.builtinArgs(fr, c)
		return func() { in.deferredCall(func() { in.applyBuiltin(fr, c, args, true) }) }
	}
	panic(interpError(fmt.Sprintf("defer of %T", call)))
}

// deferredCall runs a deferred call: the frame it creates may recover the
// panic that is running the deferred calls (in.pending).
func (in *Interp) deferredCall(run func()) {
	in.pendingDefer = true
	defer func() { in.pendingDefer, in.deferPanic = false, nil }()
	run()
}

var _ = math.NaN
var _ = strings.Repeat
