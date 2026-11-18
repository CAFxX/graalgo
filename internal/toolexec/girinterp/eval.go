package girinterp

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"

	"graalgo-toolexec/ir"
)

// typeOf is the static type of expression x in frame fr.
func (in *Interp) typeOf(fr *frame, x ir.Expr) *RType {
	return fr.pkg.typ(exprTypeID(x), fr.env)
}

func exprTypeID(x ir.Expr) ir.TypeID {
	switch x := x.(type) {
	case *ir.ConstExpr:
		return x.Type
	case *ir.NilExpr:
		return x.Type
	case *ir.LocalExpr:
		return x.Type
	case *ir.GlobalExpr:
		return x.Type
	case *ir.ExtExpr:
		return x.Type
	case *ir.InstExpr:
		return x.Type
	case *ir.BlankExpr:
		return x.Type
	case *ir.UnaryExpr:
		return x.Type
	case *ir.BinaryExpr:
		return x.Type
	case *ir.DerefExpr:
		return x.Type
	case *ir.AddrExpr:
		return x.Type
	case *ir.RecvExpr:
		return x.Type
	case *ir.FieldExpr:
		return x.Type
	case *ir.IndexExpr:
		return x.Type
	case *ir.SliceExpr:
		return x.Type
	case *ir.TypeAssertExpr:
		return x.Type
	case *ir.ConvertExpr:
		return x.Type
	case *ir.CallExpr:
		return x.Type
	case *ir.MethodCallExpr:
		return x.Type
	case *ir.IfaceCallExpr:
		return x.Type
	case *ir.BuiltinCallExpr:
		return x.Type
	case *ir.MethodValExpr:
		return x.Type
	case *ir.IfaceMethodValExpr:
		return x.Type
	case *ir.MethodExprExpr:
		return x.Type
	case *ir.IfaceMethodExprExpr:
		return x.Type
	case *ir.StructLitExpr:
		return x.Type
	case *ir.ArrayLitExpr:
		return x.Type
	case *ir.MapLitExpr:
		return x.Type
	case *ir.FuncLitExpr:
		return x.Type
	case *ir.TupleConvertExpr:
		return x.Type
	case *ir.DataExpr:
		return x.Type
	}
	panic(interpError(fmt.Sprintf("expression %T", x)))
}

// typeOfValue is a basic type with v's Go representation (only the kind
// matters to its users).
func (in *Interp) typeOfValue(v Value) *RType {
	switch v.(type) {
	case int:
		return in.basic(KInt)
	case int8:
		return in.basic(KInt8)
	case int16:
		return in.basic(KInt16)
	case int32:
		return in.basic(KInt32)
	case int64:
		return in.basic(KInt64)
	case uint:
		return in.basic(KUint)
	case uint8:
		return in.basic(KUint8)
	case uint16:
		return in.basic(KUint16)
	case uint32:
		return in.basic(KUint32)
	case uint64:
		return in.basic(KUint64)
	case uintptr:
		return in.basic(KUintptr)
	case float32:
		return in.basic(KFloat32)
	case float64:
		return in.basic(KFloat64)
	case complex64:
		return in.basic(KComplex64)
	case complex128:
		return in.basic(KComplex128)
	}
	panic(interpError("typeOfValue"))
}

func (p *Pkg) global(obj uint32) *Value {
	g := p.globals[obj]
	if g == nil {
		panic(interpError("object " + p.f.Objects[obj].Name + " is not a variable"))
	}
	return g
}

// extObj resolves a SYMREFS entry to (package, OBJECTS index).
func (in *Interp) extObj(p *Pkg, sym uint32) (*Pkg, int) {
	s := p.f.SymRefs[sym]
	q := in.pkgs[s.Pkg]
	if q == nil {
		panic(interpError("reference to package " + s.Pkg + ", which is not part of the program"))
	}
	i, ok := q.objByName[s.Name]
	if !ok {
		panic(interpError("no object " + s.Pkg + "." + s.Name))
	}
	return q, i
}

// objValue is the value of package object i of p (a variable or a
// non-generic function).
func (in *Interp) objValue(p *Pkg, i int) Value {
	o := &p.f.Objects[i]
	switch o.Kind {
	case ir.SymKindVar:
		return *p.globals[i]
	case ir.SymKindFunc:
		if o.Fn < 0 {
			panic(interpError("function " + o.Name + " has no FUNCINDEX entry"))
		}
		return in.funcClosure(p, int(o.Fn), nil)
	}
	panic(interpError("object " + o.Name + " used as a value"))
}

func (in *Interp) eval(fr *frame, x ir.Expr) Value {
	switch x := x.(type) {
	case *ir.ConstExpr:
		return in.constValue(&x.Value, fr.pkg.typ(x.Type, fr.env))
	case *ir.NilExpr:
		return in.zero(fr.pkg.typ(x.Type, fr.env))
	case *ir.DataExpr:
		d := fr.pkg.f.Data
		if x.Off > uint64(len(d)) || x.Len > uint64(len(d))-x.Off {
			panic(interpError("Data expression outside DATA"))
		}
		return string(d[x.Off : x.Off+x.Len])
	case *ir.LocalExpr:
		return *fr.slot(x.Local)
	case *ir.GlobalExpr:
		return in.objValue(fr.pkg, int(x.Obj))
	case *ir.ExtExpr:
		p, i := in.extObj(fr.pkg, x.Sym)
		return in.objValue(p, i)
	case *ir.InstExpr:
		var p *Pkg
		var i int
		switch f := x.Fn.(type) {
		case *ir.GlobalExpr:
			p, i = fr.pkg, int(f.Obj)
		case *ir.ExtExpr:
			p, i = in.extObj(fr.pkg, f.Sym)
		default:
			panic(interpError(fmt.Sprintf("instantiation of %T", x.Fn)))
		}
		var targs []*RType
		for _, t := range x.Targs {
			targs = append(targs, fr.pkg.typ(t, fr.env))
		}
		fi := int(p.f.Objects[i].Fn)
		return in.funcClosure(p, fi, p.funcEnv(&p.f.Funcs[fi], targs))
	case *ir.UnaryExpr:
		return in.unary(x.Op, in.eval(fr, x.X))
	case *ir.BinaryExpr:
		return in.binary(fr, x)
	case *ir.DerefExpr:
		p := in.eval(fr, x.X).(*Value)
		fr.pos = x.Pos
		if p == nil {
			in.runtimeNilDeref()
		}
		return *p
	case *ir.AddrExpr:
		switch y := x.X.(type) {
		case *ir.StructLitExpr, *ir.ArrayLitExpr, *ir.MapLitExpr:
			c := new(Value)
			*c = in.eval(fr, y)
			return c
		}
		return in.place(fr, x.X, true).slot
	case *ir.RecvExpr:
		ch := in.eval(fr, x.Ch).(*Chan)
		fr.pos = x.Pos
		if in.recvPending != nil {
			// a select clause's receive, already performed
			t := in.recvPending
			if x.CommaOk {
				return t
			}
			return t[0]
		}
		v, ok := in.recv(ch, in.typeOf(fr, x.Ch).Under().Elem)
		if x.CommaOk {
			return tuple{v, ok}
		}
		return v
	case *ir.FieldExpr:
		v := in.eval(fr, x.X)
		fr.pos = x.Pos
		for _, idx := range x.Path {
			if p, ok := v.(*Value); ok {
				if p == nil {
					in.runtimeNilDeref()
				}
				v = *p
			}
			v = v.(*Struct).F[idx]
		}
		return v
	case *ir.IndexExpr:
		return in.indexExpr(fr, x)
	case *ir.SliceExpr:
		return in.sliceExpr(fr, x)
	case *ir.TypeAssertExpr:
		v := in.eval(fr, x.X).(Iface)
		fr.pos = x.Pos
		t := fr.pkg.typ(x.Target, fr.env)
		ok := in.hasType(v, t)
		var r Value
		switch {
		case ok && t.Under().Kind == KInterface:
			r = v
		case ok:
			r = copyVal(v.V)
		case x.CommaOk:
			r = in.zero(t)
		default:
			in.assertPanic(in.typeOf(fr, x.X), v, t)
		}
		if x.CommaOk {
			return tuple{r, ok}
		}
		return r
	case *ir.ConvertExpr:
		v := in.eval(fr, x.X)
		return in.convert(v, in.typeOf(fr, x.X), fr.pkg.typ(x.Type, fr.env))
	case *ir.TupleConvertExpr:
		v := in.eval(fr, x.X).(tuple)
		from := in.typeOf(fr, x.X).Tuple
		to := fr.pkg.typ(x.Type, fr.env).Tuple
		out := make(tuple, len(v))
		for i := range v {
			out[i] = in.convert(v[i], from[i], to[i])
		}
		return out
	case *ir.CallExpr:
		fn := in.eval(fr, x.Fn).(*Closure)
		args := in.callArgs(fr, x.Args, x.Spread, in.typeOf(fr, x.Fn).Under())
		fr.pos = x.Pos
		return results(in.call(fn, args))
	case *ir.MethodCallExpr:
		cl, recv := in.methodTarget(fr, x.Method, x.RecvType, x.Targs, x.Recv)
		args := in.callArgs(fr, x.Args, x.Spread, cl.sig)
		fr.pos = x.Pos
		return results(in.call(cl, append([]Value{recv}, args...)))
	case *ir.IfaceCallExpr:
		m, v, t := in.ifaceTarget(fr, x.Recv, x.Name, x.Pkg)
		args := in.callArgs(fr, x.Args, x.Spread, m.sig)
		fr.pos = x.Pos
		return results(in.callMethod(m, v, t, args))
	case *ir.BuiltinCallExpr:
		args := in.builtinArgs(fr, x)
		fr.pos = x.Pos
		return in.applyBuiltin(fr, x, args, false)
	case *ir.MethodValExpr:
		cl, recv := in.methodTarget(fr, x.Method, x.RecvType, x.Targs, x.Recv)
		b := *cl
		b.bound, b.recv = true, recv
		return &b
	case *ir.IfaceMethodValExpr:
		m, v, t := in.ifaceTarget(fr, x.Recv, x.Name, x.Pkg)
		return &Closure{name: "method value", native: func(in *Interp, args []Value) []Value {
			return in.callMethod(m, v, t, args)
		}}
	case *ir.MethodExprExpr:
		rt := fr.pkg.typ(x.RecvType, fr.env)
		cl := in.methodClosure(fr, x.Method, rt, x.Targs)
		sym := fr.pkg.f.SymRefs[x.Method]
		if rt.Under().Kind == KPointer && !strings.HasPrefix(sym.Name, "(*") {
			// (*T).M of a value method: dereference the receiver
			return &Closure{name: cl.name, native: func(in *Interp, args []Value) []Value {
				p := args[0].(*Value)
				if p == nil {
					in.runtimeNilDeref()
				}
				return in.call(cl, append([]Value{copyVal(*p)}, args[1:]...))
			}}
		}
		return cl
	case *ir.IfaceMethodExprExpr:
		name, pkg := x.Name, x.Pkg
		return &Closure{name: "method expression", native: func(in *Interp, args []Value) []Value {
			v := args[0].(Iface)
			if v.T == nil {
				in.runtimeNilDeref()
			}
			return in.callMethod(in.lookupMethod(v.T, name, pkg), v.V, v.T, args[1:])
		}}
	case *ir.StructLitExpr:
		t := fr.pkg.typ(x.Type, fr.env)
		s := in.zero(t).(*Struct)
		for _, f := range x.Fields {
			v := in.eval(fr, f.Value)
			cur := s
			for _, idx := range f.Path[:len(f.Path)-1] {
				cur = cur.F[idx].(*Struct)
			}
			store(&cur.F[f.Path[len(f.Path)-1]], v)
		}
		return s
	case *ir.ArrayLitExpr:
		t := fr.pkg.typ(x.Type, fr.env).Under()
		n := 0
		for _, e := range x.Elems {
			n = max(n, int(e.Index)+1)
		}
		if t.Kind == KArray {
			n = int(t.Len)
		}
		elems := make([]Value, n)
		for i := range elems {
			elems[i] = in.zero(t.Elem)
		}
		for _, e := range x.Elems {
			store(&elems[e.Index], in.eval(fr, e.Value))
		}
		if t.Kind == KArray {
			return &Array{E: elems}
		}
		return Slice{A: elems, Len: n, Cap: n}
	case *ir.MapLitExpr:
		t := fr.pkg.typ(x.Type, fr.env).Under()
		m := newMap()
		for _, e := range x.Entries {
			k := in.eval(fr, e.Key)
			v := in.eval(fr, e.Value)
			m.set(in, k, t.KeyT, v)
		}
		return m
	case *ir.FuncLitExpr:
		caps := make([]*Value, len(x.Captures))
		for i, l := range x.Captures {
			c := fr.cells[l]
			if c == nil {
				panic(interpError(fmt.Sprintf("closure captures local %q, which the GIR does not flag captured", fr.body.Locals[l].Name)))
			}
			caps[i] = c
		}
		*fr.litCounter++
		name := fr.cl.name
		if fr.cl.body != nil && fr.body != fr.cl.body || strings.Contains(name, ".func") {
			name += "." + strconv.Itoa(*fr.litCounter)
		} else {
			name += ".func" + strconv.Itoa(*fr.litCounter)
		}
		return &Closure{pkg: fr.pkg, body: &x.Body, name: name, env: fr.env, caps: caps, sig: fr.pkg.typ(x.Type, fr.env), nlits: fr.litCounter, wrapper: x.Wrapper}
	case *ir.BlankExpr:
		panic(interpError("blank used as a value"))
	}
	panic(interpError(fmt.Sprintf("expression %T", x)))
}

func results(r []Value) Value {
	switch len(r) {
	case 0:
		return nil
	case 1:
		return r[0]
	}
	return tuple(r)
}

func (in *Interp) binary(fr *frame, x *ir.BinaryExpr) Value {
	switch x.Op {
	case ir.BinOpLAnd:
		return in.eval(fr, x.X).(bool) && in.eval(fr, x.Y).(bool)
	case ir.BinOpLOr:
		return in.eval(fr, x.X).(bool) || in.eval(fr, x.Y).(bool)
	}
	l := in.eval(fr, x.X)
	r := in.eval(fr, x.Y)
	fr.pos = x.Pos
	switch x.Op {
	case ir.BinOpShl:
		return in.shift(l, r, true)
	case ir.BinOpShr:
		return in.shift(l, r, false)
	case ir.BinOpEql, ir.BinOpNeq:
		eq := in.equal(l, r, in.typeOf(fr, x.X))
		return eq == (x.Op == ir.BinOpEql)
	}
	return in.arith(x.Op, l, r)
}

func (in *Interp) indexExpr(fr *frame, x *ir.IndexExpr) Value {
	xt := in.typeOf(fr, x.X).Under()
	v := in.eval(fr, x.X)
	i := in.eval(fr, x.Index)
	fr.pos = x.Pos
	switch xt.Kind {
	case KString:
		s := v.(string)
		return s[in.index(i, len(s))]
	case KArray:
		a := v.(*Array)
		return a.E[in.index(i, len(a.E))]
	case KPointer:
		p := v.(*Value)
		if p == nil {
			in.runtimeNilDeref()
		}
		a := (*p).(*Array)
		return a.E[in.index(i, len(a.E))]
	case KSlice:
		s := v.(Slice)
		return s.A[s.Off+in.index(i, s.Len)]
	case KMap:
		r, ok := v.(*Map).lookup(in, i, xt.KeyT)
		if !ok {
			r = in.zero(xt.Elem)
		}
		if x.CommaOk {
			return tuple{r, ok}
		}
		return r
	}
	panic(interpError("index of " + xt.Str))
}

func (in *Interp) sliceExpr(fr *frame, x *ir.SliceExpr) Value {
	xt := in.typeOf(fr, x.X).Under()
	v := in.eval(fr, x.X)
	bound := func(e ir.Expr, def int) (int, bool) {
		if e == nil {
			return def, false
		}
		n, _ := asInt64(in.eval(fr, e))
		return int(n), true
	}
	var length, capacity int
	var base []Value
	off := 0
	switch xt.Kind {
	case KString:
		length = len(v.(string))
		capacity = length
	case KArray, KPointer:
		var a *Array
		if xt.Kind == KPointer {
			p := v.(*Value)
			if p == nil {
				in.runtimeNilDeref()
			}
			a = (*p).(*Array)
		} else {
			a = v.(*Array)
		}
		base, length, capacity = a.E, len(a.E), len(a.E)
	case KSlice:
		s := v.(Slice)
		base, off, length, capacity = s.A, s.Off, s.Len, s.Cap
	}
	lo, _ := bound(x.Lo, 0)
	hi, hasHi := bound(x.Hi, length)
	mx, hasMax := bound(x.Max, capacity)
	fr.pos = x.Pos
	str := strconv.Itoa
	isStr := xt.Kind == KString
	switch {
	case hasMax:
		if mx < 0 || mx > capacity {
			in.runtimePanic("slice bounds out of range [::" + str(mx) + "] with capacity " + str(capacity))
		}
		if hi < 0 || hi > mx {
			in.runtimePanic("slice bounds out of range [:" + str(hi) + ":" + str(mx) + "]")
		}
		if lo < 0 || lo > hi {
			in.runtimePanic("slice bounds out of range [" + str(lo) + ":" + str(hi) + ":]")
		}
	default:
		limit, what := capacity, "capacity"
		if isStr || xt.Kind == KArray || xt.Kind == KPointer {
			what = "length"
		}
		if hasHi && (hi < 0 || hi > limit) {
			in.runtimePanic("slice bounds out of range [:" + str(hi) + "] with " + what + " " + str(limit))
		}
		if lo < 0 || lo > hi {
			if !hasHi && lo > limit {
				in.runtimePanic("slice bounds out of range [" + str(lo) + ":" + str(hi) + "]")
			}
			in.runtimePanic("slice bounds out of range [" + str(lo) + ":" + str(hi) + "]")
		}
		mx = capacity
	}
	if isStr {
		return v.(string)[lo:hi]
	}
	return Slice{A: base, Off: off + lo, Len: hi - lo, Cap: mx - lo}
}

// assertPanic reports a failed x.(T).
func (in *Interp) assertPanic(static *RType, v Iface, t *RType) {
	var msg string
	switch {
	case v.T == nil:
		msg = "interface conversion: interface is nil, not " + t.Str
		if len(static.Under().IMethods) == 0 {
			msg = "interface conversion: interface {} is nil, not " + t.Str
		} else {
			msg = "interface conversion: interface is nil, not " + t.Str
		}
	case t.Under().Kind == KInterface:
		_, missing := in.implements(v.T, t)
		msg = "interface conversion: " + v.T.Str + " is not " + t.Str + ": missing method " + missing
	default:
		msg = "interface conversion: " + static.Str + " is " + v.T.Str + ", not " + t.Str
	}
	in.panicValue(Iface{T: in.plainErrType(), V: msg})
}

// callArgs evaluates call arguments (expanding a multi-value argument)
// and packs the variadic tail into a slice.
func (in *Interp) callArgs(fr *frame, xs []ir.Expr, spread bool, sig *RType) []Value {
	var args []Value
	if len(xs) == 1 {
		if _, isTuple := in.typeOf(fr, xs[0]).Under(), false; !isTuple {
		}
	}
	for _, x := range xs {
		v := in.eval(fr, x)
		if t, ok := v.(tuple); ok {
			args = append(args, t...)
			continue
		}
		args = append(args, copyVal(v))
	}
	if sig.Variadic && !spread {
		n := len(sig.Params) - 1
		if len(args) < n {
			panic(interpError("too few arguments"))
		}
		tail := append([]Value(nil), args[n:]...)
		var s Slice
		if len(tail) > 0 {
			s = Slice{A: tail, Len: len(tail), Cap: len(tail)}
		}
		args = append(args[:n:n], s)
	}
	return args
}

// methodClosure returns the closure of a static method under the
// receiver type's (and the method's own) type arguments.
func (in *Interp) methodClosure(fr *frame, msym uint32, rt *RType, mtargs []ir.TypeID) *Closure {
	sym := fr.pkg.f.SymRefs[msym]
	dp := in.pkgs[sym.Pkg]
	if dp == nil {
		panic(interpError("method of package " + sym.Pkg + ", which is not part of the program"))
	}
	fi, ok := dp.funcByName[sym.Name]
	if !ok {
		panic(interpError("no method " + sym.Pkg + "." + sym.Name))
	}
	if rt.Under().Kind == KPointer && !rt.IsNamed() {
		rt = rt.Elem
	}
	targs := append([]*RType(nil), rt.Targs...)
	for _, t := range mtargs {
		targs = append(targs, fr.pkg.typ(t, fr.env))
	}
	fn := &dp.f.Funcs[fi]
	return in.funcClosure(dp, fi, dp.funcEnv(fn, targs))
}

func (in *Interp) methodTarget(fr *frame, msym uint32, recvType ir.TypeID, mtargs []ir.TypeID, recv ir.Expr) (*Closure, Value) {
	r := copyVal(in.eval(fr, recv))
	return in.methodClosure(fr, msym, fr.pkg.typ(recvType, fr.env), mtargs), r
}

// ifaceTarget evaluates the receiver of a dynamic call: an interface
// value, or a value of an (instantiated) type parameter type.
func (in *Interp) ifaceTarget(fr *frame, recv ir.Expr, name, pkg string) (*method, Value, *RType) {
	st := in.typeOf(fr, recv)
	v := in.eval(fr, recv)
	t := st
	if st.Under().Kind == KInterface {
		iv := v.(Iface)
		if iv.T == nil {
			fr.pos = 0
			in.runtimeNilDeref()
		}
		v, t = iv.V, iv.T
	}
	m := in.lookupMethod(t, name, pkg)
	if m == nil {
		panic(interpError(fmt.Sprintf("type %s has no method %s in its GIR method set", t.Str, name)))
	}
	return m, v, t
}

// ---------------------------------------------------------------- builtins

func (in *Interp) builtinArgs(fr *frame, x *ir.BuiltinCallExpr) []Value {
	var args []Value
	for _, a := range x.Args {
		v := in.eval(fr, a)
		if t, ok := v.(tuple); ok && x.Builtin != ir.BuiltinRecover {
			args = append(args, t...)
			continue
		}
		args = append(args, v)
	}
	return args
}

func (in *Interp) argTypes(fr *frame, x *ir.BuiltinCallExpr) []*RType {
	var ts []*RType
	for _, a := range x.Args {
		t := in.typeOf(fr, a)
		if t.Kind == KTuple {
			ts = append(ts, t.Tuple...)
			continue
		}
		ts = append(ts, t)
	}
	return ts
}

func (in *Interp) applyBuiltin(fr *frame, x *ir.BuiltinCallExpr, args []Value, deferred bool) Value {
	ts := in.argTypes(fr, x)
	switch x.Builtin {
	case ir.BuiltinPrint, ir.BuiltinPrintln:
		var b strings.Builder
		for i, a := range args {
			if i > 0 && x.Builtin == ir.BuiltinPrintln {
				b.WriteByte(' ')
			}
			b.WriteString(printArg(a))
		}
		if x.Builtin == ir.BuiltinPrintln {
			b.WriteByte('\n')
		}
		fmt.Fprint(in.stderr, b.String())
		return nil
	case ir.BuiltinPanic:
		v := args[0].(Iface)
		if v.T == nil {
			in.panicValue(Iface{T: in.rtErrType, V: "runtime error: panic called with nil argument"})
		}
		in.panicValue(v)
	case ir.BuiltinRecover:
		if deferred || fr.rangeBody > 0 {
			// recover() deferred directly, or in a range-over-func body (which
			// runs inside yield), is not called by a deferred function
			return Iface{}
		}
		p := fr.deferOf
		if p == nil || p.recovered {
			return Iface{}
		}
		p.recovered = true
		return p.val
	case ir.BuiltinLen, ir.BuiltinCap:
		return in.lenCap(args[0], ts[0], x.Builtin == ir.BuiltinCap)
	case ir.BuiltinNew:
		c := new(Value)
		*c = in.zero(fr.pkg.typ(x.Targ, fr.env))
		return c
	case ir.BuiltinMake:
		return in.makeBuiltin(fr.pkg.typ(x.Targ, fr.env), args)
	case ir.BuiltinAppend:
		return in.appendBuiltin(fr, x, args, ts)
	case ir.BuiltinCopy:
		dst := args[0].(Slice)
		var src []Value
		switch s := args[1].(type) {
		case string:
			for i := 0; i < len(s); i++ {
				src = append(src, s[i])
			}
		case Slice:
			src = append([]Value(nil), s.A[s.Off:s.Off+s.Len]...)
		}
		n := min(dst.Len, len(src))
		for i := 0; i < n; i++ {
			dst.A[dst.Off+i] = copyVal(src[i])
		}
		return n
	case ir.BuiltinDelete:
		args[0].(*Map).del(in, args[1], ts[0].Under().KeyT)
		return nil
	case ir.BuiltinClear:
		switch v := args[0].(type) {
		case *Map:
			v.clear()
		case Slice:
			for i := 0; i < v.Len; i++ {
				v.A[v.Off+i] = in.zero(ts[0].Under().Elem)
			}
		}
		return nil
	case ir.BuiltinClose:
		ch := args[0].(*Chan)
		if ch == nil {
			in.panicValue(Iface{T: in.plainErrType(), V: "close of nil channel"})
		}
		if ch.closed {
			in.panicValue(Iface{T: in.plainErrType(), V: "close of closed channel"})
		}
		ch.closed = true
		return nil
	case ir.BuiltinComplex:
		switch re := args[0].(type) {
		case float32:
			return complex(re, args[1].(float32))
		case float64:
			return complex(re, args[1].(float64))
		}
	case ir.BuiltinReal, ir.BuiltinImag:
		re := x.Builtin == ir.BuiltinReal
		switch c := args[0].(type) {
		case complex64:
			if re {
				return real(c)
			}
			return imag(c)
		case complex128:
			if re {
				return real(c)
			}
			return imag(c)
		}
	case ir.BuiltinMin, ir.BuiltinMax:
		return in.minMax(args, x.Builtin == ir.BuiltinMin)
	}
	panic(interpError("builtin " + x.Builtin.String()))
}

func (in *Interp) lenCap(v Value, t *RType, isCap bool) Value {
	u := t.Under()
	switch u.Kind {
	case KString:
		return len(v.(string))
	case KArray:
		return int(u.Len)
	case KPointer:
		return int(u.Elem.Under().Len)
	case KSlice:
		if isCap {
			return v.(Slice).Cap
		}
		return v.(Slice).Len
	case KMap:
		if m := v.(*Map); m != nil {
			return m.n
		}
		return 0
	case KChan:
		ch := v.(*Chan)
		if ch == nil {
			return 0
		}
		if isCap {
			return ch.cap
		}
		return len(ch.buf)
	}
	panic(interpError("len of " + t.Str))
}

func (in *Interp) makeBuiltin(t *RType, args []Value) Value {
	u := t.Under()
	size := func(i int, what string) int {
		if i >= len(args) {
			return 0
		}
		n, _ := asInt64(args[i])
		if !isUnsignedVal(args[i]) && n < 0 || n > 1<<40 {
			in.runtimePanic("makeslice: " + what + " out of range")
		}
		return int(n)
	}
	switch u.Kind {
	case KSlice:
		n := size(0, "len")
		c := n
		if len(args) > 1 {
			c = size(1, "cap")
			if c < n {
				in.runtimePanic("makeslice: cap out of range")
			}
		}
		a := make([]Value, c)
		for i := range a {
			a[i] = in.zero(u.Elem)
		}
		return Slice{A: a, Len: n, Cap: c}
	case KMap:
		return newMap()
	case KChan:
		return &Chan{cap: size(0, "size")}
	}
	panic(interpError("make of " + t.Str))
}

func (in *Interp) appendBuiltin(fr *frame, x *ir.BuiltinCallExpr, args []Value, ts []*RType) Value {
	s := args[0].(Slice)
	st := in.typeOf(fr, x.Args[0]).Under()
	var add []Value
	if x.Spread {
		switch t := args[1].(type) {
		case string:
			for i := 0; i < len(t); i++ {
				add = append(add, t[i])
			}
		case Slice:
			add = append(add, t.A[t.Off:t.Off+t.Len]...)
		}
	} else {
		add = args[1:]
	}
	if len(add) == 0 {
		return s
	}
	n := s.Len + len(add)
	if n <= s.Cap {
		for i, v := range add {
			s.A[s.Off+s.Len+i] = copyVal(v)
		}
		s.Len = n
		return s
	}
	newCap := in.growCap(n, s.Cap, st.Elem)
	a := make([]Value, newCap)
	copy(a, s.A[s.Off:s.Off+s.Len])
	for i, v := range add {
		a[s.Len+i] = copyVal(v)
	}
	for i := n; i < newCap; i++ {
		a[i] = in.zero(st.Elem)
	}
	return Slice{A: a, Len: n, Cap: newCap}
}

// growCap is runtime.growslice's capacity: nextslicecap, then rounded up
// to the malloc size class.
func (in *Interp) growCap(newLen, oldCap int, elem *RType) int {
	newcap := oldCap
	doublecap := newcap + newcap
	switch {
	case newLen > doublecap:
		newcap = newLen
	case oldCap < 256:
		newcap = doublecap
	default:
		for newcap < newLen {
			newcap += (newcap + 3*256) >> 2
		}
	}
	size := in.sizeof(elem)
	if size == 0 {
		return newLen
	}
	mem := roundupsize(uint64(newcap)*uint64(size), !in.hasPointers(elem))
	return int(mem / uint64(size))
}

func (in *Interp) minMax(args []Value, isMin bool) Value {
	r := args[0]
	for _, a := range args[1:] {
		switch x := a.(type) {
		case float32:
			r = floatMinMax(float64(r.(float32)), float64(x), isMin, true)
			continue
		case float64:
			r = floatMinMax(r.(float64), x, isMin, false)
			continue
		}
		less := in.arith(ir.BinOpLss, a, r).(bool)
		if less == isMin && in.arith(ir.BinOpNeq, a, r).(bool) {
			r = a
		}
	}
	return r
}

func floatMinMax(r, x float64, isMin, f32 bool) Value {
	var v float64
	switch {
	case math.IsNaN(r) || math.IsNaN(x):
		v = math.NaN()
	case isMin:
		v = math.Min(r, x)
	default:
		v = math.Max(r, x)
	}
	if f32 {
		return float32(v)
	}
	return v
}

// addrOf renders the address a gc program would print for a reference-like
// value: the interpreter object's own address (the Go heap's, so shaped like
// gc's), 0x0 for nil. Only the differential normalizer masks it — the
// interpreter never prints the mask token itself.
func addrOf(v Value) string {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Chan, reflect.Func, reflect.Slice:
		if rv.IsNil() {
			return "0x0"
		}
		return fmt.Sprintf("%#x", rv.Pointer())
	case reflect.Invalid:
		return "0x0"
	}
	return fmt.Sprintf("%p", &v) // a value copied into the interface: its box
}

// printArg formats one print/println argument as the runtime does.
func printArg(v Value) string {
	switch x := v.(type) {
	case Iface:
		if x.T == nil {
			return "(0x0,0x0)"
		}
		return "(" + addrOf(x.T) + "," + addrOf(x.V) + ")"
	case *Value, *Map, *Chan, *Closure:
		return addrOf(x)
	case Slice:
		return "[" + strconv.Itoa(x.Len) + "/" + strconv.Itoa(x.Cap) + "]" + addrOf(x.A)
	case float32:
		return strconv.FormatFloat(float64(x), 'g', -1, 32)
	}
	return formatScalar(v)
}
