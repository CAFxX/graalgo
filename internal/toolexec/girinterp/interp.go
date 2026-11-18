// Package girinterp is a reference interpreter for GIR, used only by tests:
// it executes the emitter's output for self-contained programs (println,
// builtins, the whole language except goroutines and unsafe) so that an
// emitter bug shows as an output difference against gc before the JVM
// backend (W4) exists. It is deliberately simple and slow, and it takes
// the IR at its word: method sets, capture lists, address-taken flags,
// implicit conversions, init order and positions come from the GIR, never
// recomputed — so that a GIR mistake is visible, not papered over.
package girinterp

import (
	"bytes"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"

	"graalgo-toolexec/ir"
)

// InterpError is an interpreter-detected inconsistency in the GIR (or an
// unsupported construct); it aborts the run.
type InterpError struct{ Msg string }

func (e *InterpError) Error() string { return "girinterp: " + e.Msg }

func interpError(msg string) *InterpError { return &InterpError{Msg: msg} }

// Interp is one program run.
type Interp struct {
	pkgs          map[string]*Pkg
	order         []*Pkg
	types         map[string]*RType
	typedefs      map[string]*typeDef // "pkg.Name" (gc symbol name) -> TYPEDEFS entry
	structMethods map[string]*typeDef // unnamed struct key -> TYPEDEFS entry
	stdout        io.Writer
	stderr        io.Writer
	stack         []*frame
	steps         int
	MaxSteps      int
	nanKeys       int
	ptrIDs        map[*Value]int
	chanIDs       map[*Chan]int
	closures      map[closureKey]*Closure
	rtErrType     *RType
	plainErr      *RType
	pendingDefer  bool     // the next frame is a deferred call ...
	deferPanic    *goPanic // ... run by this panic (nil: normal return)
	recvPending   tuple    // select: the value received for the clause's assignment
	panics        *goPanic // the panic stack
}

// Pkg is one package's GIR.
type Pkg struct {
	in         *Interp
	f          *ir.File
	globals    []*Value // by OBJECTS index; nil for non-variables
	funcByName map[string]int
	objByName  map[string]int
	typeCache  map[ir.TypeID]*RType
	genCache   map[cacheKey]*RType
	tpCache    map[ir.TypeID]bool
	lines      map[*ir.FuncBody][]int
}

// Closure is a function value.
type Closure struct {
	pkg    *Pkg
	body   *ir.FuncBody
	name   string // gc's function name (tracebacks)
	env    *env
	caps   []*Value
	bound  bool
	recv   Value
	native func(in *Interp, args []Value) []Value
	sig    *RType
	nlits  *int // closure counter of the enclosing top-level function
	// wrapper: a generated forwarding literal (FuncLit.Wrapper), gc's
	// WRAPPER frame: it leaves recover() eligibility to the frame it calls.
	wrapper bool
}

type closureKey struct {
	pkg *Pkg
	fn  int
	env string
}

// New loads the packages of a program. files may be in any order.
func New(files []*ir.File, stdout, stderr io.Writer) (*Interp, error) {
	in := &Interp{
		pkgs: map[string]*Pkg{}, types: map[string]*RType{}, typedefs: map[string]*typeDef{},
		structMethods: map[string]*typeDef{}, stdout: stdout, stderr: stderr, MaxSteps: 200_000_000,
		ptrIDs: map[*Value]int{}, chanIDs: map[*Chan]int{}, closures: map[closureKey]*Closure{},
	}
	for _, f := range files {
		if err := f.Validate(); err != nil {
			return nil, err
		}
		p := &Pkg{in: in, f: f, funcByName: map[string]int{}, objByName: map[string]int{},
			typeCache: map[ir.TypeID]*RType{}, genCache: map[cacheKey]*RType{}, tpCache: map[ir.TypeID]bool{},
			lines: map[*ir.FuncBody][]int{}}
		in.pkgs[f.Pkg.Path] = p
		for i, fn := range f.Funcs {
			p.funcByName[fn.Name] = i
		}
		for i, o := range f.Objects {
			p.objByName[o.Name] = i
		}
	}
	// TYPEDEFS by declaring symbol (defined types) or struct key.
	for _, p := range in.pkgs {
		for i := range p.f.TypeDefs {
			td := &p.f.TypeDefs[i]
			switch t := p.f.Types[td.Type-1].Type.(type) {
			case *ir.NamedType:
				sym := p.f.SymRefs[t.Sym]
				in.typedefs[qualified(sym.Pkg, sym.Name)] = &typeDef{pkg: p, td: td}
			case *ir.StructType:
				in.structMethods[p.f.Types[td.Type-1].Key] = &typeDef{pkg: p, td: td}
			}
		}
	}
	str := in.basic(KString)
	in.rtErrType = &RType{Kind: KString, Key: "runtime.Error·", Str: "runtime.Error", NamedPkg: "runtime", NamedName: "Error·"}
	in.rtErrType.under = str
	in.types[in.rtErrType.Key] = in.rtErrType
	for _, p := range in.pkgs {
		p.globals = make([]*Value, len(p.f.Objects))
		for i, o := range p.f.Objects {
			if o.Kind == ir.SymKindVar {
				v := in.zero(p.typ(o.Type, nil))
				p.globals[i] = &v
			}
		}
	}
	// gc's package initialization order (go1.21+): repeatedly initialize
	// the first package, in import path order, whose imports are all done.
	var paths []string
	for path := range in.pkgs {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	done := map[string]bool{}
	for len(in.order) < len(paths) {
		progress := false
		for _, path := range paths {
			if done[path] {
				continue
			}
			ready := true
			for _, imp := range in.pkgs[path].f.Pkg.Imports {
				if _, ours := in.pkgs[imp]; ours && !done[imp] {
					ready = false
				}
			}
			if ready {
				done[path] = true
				in.order = append(in.order, in.pkgs[path])
				progress = true
				break
			}
		}
		if !progress {
			return nil, interpError("import cycle")
		}
	}
	return in, nil
}

// Run initializes every package and runs main.main. It returns the exit
// status (0, or 2 after a fatal panic / deadlock).
func (in *Interp) Run() (code int, err error) {
	defer func() {
		if r := recover(); r != nil {
			switch x := r.(type) {
			case *goPanic:
				in.printFatal(x)
				code = 2
			case *fatalError:
				fmt.Fprintf(in.stderr, "fatal error: %s\n\ngoroutine 1 [%s]:\n", x.msg, x.wait)
				in.printStack(in.snapshot())
				code = 2
			case *InterpError:
				err = x
			default:
				err = fmt.Errorf("girinterp: internal panic: %v", r)
			}
		}
	}()
	for _, p := range in.order {
		if p.f.Init.VarInit >= 0 {
			in.callFunc(p, int(p.f.Init.VarInit), nil, nil, nil)
		}
		for _, i := range p.f.Init.Inits {
			in.callFunc(p, int(i), nil, nil, nil)
		}
	}
	main := in.pkgs["main"]
	if main == nil {
		return 0, interpError("no package main")
	}
	idx, ok := main.funcByName["main"]
	if !ok {
		return 0, interpError("no main.main")
	}
	in.callFunc(main, idx, nil, nil, nil)
	return 0, nil
}

type fatalError struct{ msg, wait string }

// ---------------------------------------------------------------- functions

// gcName is gc's name of function fn of p in tracebacks.
func (p *Pkg) gcName(fn *ir.Func) string {
	name := fn.Name
	switch {
	case fn.Flags&16 != 0:
		name = "init"
	case fn.Flags&2 != 0:
		// generic: F[...] / T[...].M / (*T[...]).M
		if i := strings.LastIndexByte(name, '.'); i >= 0 {
			base := name[:i]
			if strings.HasPrefix(base, "(*") {
				name = base[:len(base)-1] + "[...])" + name[i:]
			} else {
				name = base + "[...]" + name[i:]
			}
		} else {
			name += "[...]"
		}
	}
	return p.f.Pkg.Path + "." + name
}

// funcClosure returns the closure of FUNCINDEX entry idx under e.
func (in *Interp) funcClosure(p *Pkg, idx int, e *env) *Closure {
	k := closureKey{p, idx, ""}
	if e != nil {
		k.env = e.key
	}
	if c := in.closures[k]; c != nil {
		return c
	}
	fn := &p.f.Funcs[idx]
	body, err := p.f.Body(idx)
	if err != nil {
		panic(interpError(err.Error()))
	}
	if body == nil {
		panic(interpError("call of bodyless function " + fn.Name))
	}
	c := &Closure{pkg: p, body: body, name: p.gcName(fn), env: e, sig: p.typ(fn.Sig, e), nlits: new(int)}
	in.closures[k] = c
	return c
}

// funcEnv binds a function's type parameters (receiver's first) to targs.
func (p *Pkg) funcEnv(fn *ir.Func, targs []*RType) *env {
	if len(fn.Tparams) != len(targs) {
		panic(interpError(fmt.Sprintf("%s: %d type parameters, %d type arguments", fn.Name, len(fn.Tparams), len(targs))))
	}
	var keys []string
	for _, tp := range fn.Tparams {
		keys = append(keys, p.tparamKey(p.f.Types[tp-1].Type.(*ir.TypeParamType)))
	}
	return (*env)(nil).extend(keys, targs)
}

func (in *Interp) callFunc(p *Pkg, idx int, e *env, args []Value, at *frame) []Value {
	return in.call(in.funcClosure(p, idx, e), args)
}

// frame is one activation.
type frame struct {
	cl         *Closure
	pkg        *Pkg
	body       *ir.FuncBody
	env        *env
	locals     []Value
	cells      []*Value // non-nil for captured / address-taken locals
	defers     []func()
	deferOf    *goPanic // the panic whose deferred calls invoked this frame
	pos        ir.PosID
	inDefer    bool
	retDone    bool
	litCounter *int
}

const (
	flagParam    = 1
	flagResult   = 2
	flagRecv     = 4
	flagCaptured = 8
	flagAddr     = 16
)

// declare (re)creates local i: a fresh variable each time its declaration
// executes (a new cell for a flagged local).
func (fr *frame) declare(in *Interp, i uint32, v Value) {
	l := &fr.body.Locals[i]
	if v == nil {
		v = in.zero(fr.pkg.typ(l.Type, fr.env))
	} else {
		v = copyVal(v)
	}
	if l.Flags&(flagCaptured|flagAddr) != 0 {
		c := new(Value)
		*c = v
		fr.cells[i] = c
		return
	}
	fr.locals[i] = v
}

// slot is the storage of local i for reading and writing by the frame
// itself.
func (fr *frame) slot(i uint32) *Value {
	if c := fr.cells[i]; c != nil {
		return c
	}
	return &fr.locals[i]
}

// addrOfLocal is &local: only legal for locals the GIR flags.
func (fr *frame) addrOfLocal(i uint32) *Value {
	if c := fr.cells[i]; c != nil {
		return c
	}
	panic(interpError(fmt.Sprintf("address of local %q, which the GIR does not flag address-taken or captured", fr.body.Locals[i].Name)))
}

// call runs a closure with already converted (and variadic-packed)
// arguments and returns its results.
func (in *Interp) call(cl *Closure, args []Value) []Value {
	if cl == nil {
		in.runtimeNilDeref()
	}
	if cl.native != nil {
		return cl.native(in, args)
	}
	if cl.bound {
		args = append([]Value{cl.recv}, args...)
	}
	b := cl.body
	fr := &frame{cl: cl, pkg: cl.pkg, body: b, env: cl.env, locals: make([]Value, len(b.Locals)), cells: make([]*Value, len(b.Locals)), litCounter: cl.nlits}
	for i, c := range cl.caps {
		fr.cells[i] = c
	}
	if in.pendingDefer && !cl.wrapper {
		fr.inDefer, fr.deferOf = true, in.deferPanic
		in.pendingDefer, in.deferPanic = false, nil
	}
	ai := 0
	if b.Recv >= 0 {
		fr.declare(in, uint32(b.Recv), args[0])
		ai = 1
	}
	if len(args)-ai != len(b.Params) {
		panic(interpError(fmt.Sprintf("%s: %d arguments for %d parameters", cl.name, len(args)-ai, len(b.Params))))
	}
	for i, l := range b.Params {
		fr.declare(in, l, args[ai+i])
	}
	for _, l := range b.Results {
		fr.declare(in, l, nil)
	}
	return in.runFrame(fr)
}

func (in *Interp) runFrame(fr *frame) []Value {
	depth := len(in.stack)
	if depth > 20000 {
		panic(&fatalError{msg: "stack overflow", wait: "running"})
	}
	in.stack = append(in.stack, fr)
	var p *goPanic
	func() {
		defer func() {
			if r := recover(); r != nil {
				gp, ok := r.(*goPanic)
				if !ok {
					panic(r)
				}
				p = gp
			}
		}()
		in.execBody(fr)
	}()
	// Deferred calls, LIFO; each may recover the active panic or panic anew.
	for len(fr.defers) > 0 {
		d := fr.defers[len(fr.defers)-1]
		fr.defers = fr.defers[:len(fr.defers)-1]
		cur := p
		func() {
			defer func() {
				if r := recover(); r != nil {
					gp, ok := r.(*goPanic)
					if !ok {
						panic(r)
					}
					p = gp
				}
			}()
			in.deferPanic = cur
			d()
		}()
		if p != nil && p.recovered && p == cur {
			// recovered: the function returns normally with its current
			// results; the panicking frames above it are gone.
			p = nil
			in.stack = in.stack[:depth+1]
			for in.panics != nil && in.panics.depth > depth {
				in.panics = in.panics.link
			}
		}
	}
	if p != nil {
		panic(p)
	}
	in.stack = in.stack[:depth]
	res := make([]Value, len(fr.body.Results))
	for i, l := range fr.body.Results {
		res[i] = *fr.slot(l)
	}
	return res
}

// ---------------------------------------------------------------- panics

// goPanic is an active Go panic.
type goPanic struct {
	val        Iface
	link       *goPanic
	recovered  bool
	repanicked bool
	stack      []stackEntry
	depth      int // stack depth where it was raised
}

type stackEntry struct {
	name string
	file string
	line int
}

func (in *Interp) snapshot() []stackEntry {
	var out []stackEntry
	for i := len(in.stack) - 1; i >= 0; i-- {
		fr := in.stack[i]
		file, line := fr.pkg.posOf(fr.body, fr.pos)
		out = append(out, stackEntry{name: fr.cl.name, file: file, line: line})
	}
	return out
}

// posOf returns the file and line of position id in body.
func (p *Pkg) posOf(b *ir.FuncBody, id ir.PosID) (string, int) {
	lines, ok := p.lines[b]
	if !ok {
		l := 0
		for _, ps := range b.Positions {
			l += int(ps.Line)
			lines = append(lines, l)
		}
		p.lines[b] = lines
	}
	if id == 0 || int(id) > len(lines) {
		return b.File, 0
	}
	file := b.File
	if f := b.Positions[id-1].File; f != "" {
		file = f
	}
	return file, lines[id-1]
}

// panicValue starts a panic with value v.
func (in *Interp) panicValue(v Iface) {
	// The goroutine's panic stack, as gc keeps it: a new panic links to
	// the current head (printed "panic: first\n\tpanic: second"); a
	// recovered panic stays on it until the recovering frame returns.
	gp := &goPanic{val: v, stack: in.snapshot(), link: in.panics, depth: len(in.stack)}
	in.panics = gp
	panic(gp)
}

// runtimePanic panics with a runtime.Error whose Error() is
// "runtime error: " + msg.
func (in *Interp) runtimePanic(msg string) {
	in.panicValue(Iface{T: in.rtErrType, V: "runtime error: " + msg})
}

const nilDerefMsg = "runtime error: invalid memory address or nil pointer dereference"

func (in *Interp) runtimeNilDeref() {
	in.panicValue(Iface{T: in.rtErrType, V: nilDerefMsg})
}

// plainErrType is the run-time error type whose Error() is the text
// itself (runtime's plainError and TypeAssertionError).
func (in *Interp) plainErrType() *RType { return in.rtErrType }

// errorText is the text printpanics shows for a panic value: Error() or
// String() results, else the value itself.
func (in *Interp) panicText(v Iface) string {
	if v.T == nil {
		return "nil"
	}
	if v.T == in.rtErrType {
		return v.V.(string)
	}
	for _, m := range []string{"Error", "String"} {
		if r, ok := in.callMethodByName(v, m); ok {
			return printIndented(r)
		}
	}
	u := v.T.Under()
	if u.Kind <= KString && u.Kind != KInvalid {
		s := formatScalar(v.V)
		if u.Kind == KString {
			s = printIndented(s)
		}
		if !v.T.IsNamed() {
			return s
		}
		switch u.Kind {
		case KString:
			return v.T.Str + `("` + s + `")`
		case KComplex64, KComplex128:
			return v.T.Str + s
		}
		return v.T.Str + "(" + s + ")"
	}
	return "(" + v.T.Str + ") 0xADDR"
}

func printIndented(s string) string { return strings.ReplaceAll(s, "\n", "\n\t") }

// callMethodByName calls a niladic string method of v, if its method set
// has one with that signature.
func (in *Interp) callMethodByName(v Iface, name string) (string, bool) {
	m := in.lookupMethod(v.T, name, "")
	if m == nil {
		return "", false
	}
	sig := m.sig
	if len(sig.Params) != 0 || len(sig.Results) != 1 || sig.Results[0].Key != "string" {
		return "", false
	}
	res := in.callMethod(m, v.V, v.T, nil)
	return res[0].(string), true
}

func (in *Interp) printFatal(p *goPanic) {
	// preprintpanics: mark repanics of the same value.
	for q := p; q != nil; q = q.link {
		if q.link != nil && q.link.val.T == q.val.T && q.val.T != nil && in.comparable(q.val.T) && in.equal(q.link.val.V, q.val.V, q.val.T) {
			q.link.repanicked = true
		}
	}
	// preprintpanics: every value's text is rendered (running its Error()/
	// String()) before anything prints; a method that panics turns the
	// report into gc's "panic while printing panic value" throw.
	texts, inner := in.renderPanicTexts(p)
	if inner != nil {
		fmt.Fprintf(in.stderr, "fatal error: panic while printing panic value: %s\n\ngoroutine 1 [running]:\n", in.throwValueText(inner.val))
		in.printStack(inner.stack)
		return
	}
	var b bytes.Buffer
	var print func(q *goPanic)
	print = func(q *goPanic) {
		if q.link != nil {
			print(q.link)
			if q.link.repanicked {
				return
			}
			b.WriteString("\t")
		}
		b.WriteString("panic: " + texts[q])
		switch {
		case q.recovered && q.repanicked:
			b.WriteString(" [recovered, repanicked]")
		case q.recovered:
			b.WriteString(" [recovered]")
		}
		b.WriteString("\n")
	}
	print(p)
	// gc prints the signal line once, after the whole chain, when any panic
	// of the goroutine came from a fault (gp.sig stays set).
	for q := p; q != nil; q = q.link {
		if q.val.T == in.rtErrType && q.val.V == nilDerefMsg {
			b.WriteString("[signal SIGSEGV: segmentation violation code=0x1 addr=0x0 pc=0x0]\n")
			break
		}
	}
	b.WriteString("\ngoroutine 1 [running]:\n")
	io.Copy(in.stderr, &b)
	in.printStack(p.stack)
}

// renderPanicTexts renders the text of every panic in p's chain, or
// returns the panic raised by an Error()/String() method while doing so.
func (in *Interp) renderPanicTexts(p *goPanic) (texts map[*goPanic]string, inner *goPanic) {
	defer func() {
		if r := recover(); r != nil {
			x, ok := r.(*goPanic)
			if !ok {
				panic(r)
			}
			texts, inner = nil, x
		}
	}()
	texts = make(map[*goPanic]string)
	for q := p; q != nil; q = q.link {
		texts[q] = in.panicText(q.val)
	}
	return texts, nil
}

// throwValueText is gc preprintpanics' recover: a value of the predeclared
// type string prints as is, any other value as "type <T>".
func (in *Interp) throwValueText(v Iface) string {
	if v.T != nil && !v.T.IsNamed() && v.T.Kind == KString {
		if s, ok := v.V.(string); ok {
			return s
		}
	}
	if v.T == nil {
		return "type <nil>"
	}
	return "type " + v.T.Str
}

func (in *Interp) printStack(st []stackEntry) {
	for _, e := range st {
		fmt.Fprintf(in.stderr, "%s(...)\n\t%s:%d\n", e.name, e.file, e.line)
	}
}

// ---------------------------------------------------------------- methods

// method is a resolved concrete method with the receiver adjustment.
type method struct {
	path    []uint64
	ptrRecv bool
	clos    *Closure // nil: promoted interface method (dynamic again)
	iname   string
	ipkg    string
	sig     *RType
	native  func(v Value) Value
}

// methodSet returns the TYPEDEFS method set of t (value or pointer form).
func (in *Interp) methodSet(t *RType) ([]ir.MsetEntry, *typeDef, *RType) {
	base, ptr := t, false
	if t.Kind == KPointer && !t.IsNamed() {
		base, ptr = t.Elem, true
	}
	var def *typeDef
	switch {
	case base.IsNamed():
		def = base.def
	case base.Kind == KStruct:
		def = base.unnamedMethods
	}
	if def == nil {
		return nil, nil, base
	}
	if ptr {
		return def.td.PtrMset, def, base
	}
	return def.td.Mset, def, base
}

func (in *Interp) lookupMethod(t *RType, name, pkg string) *method {
	if t == in.rtErrType {
		if name == "Error" {
			s := in.basic(KString)
			return &method{native: func(v Value) Value { return v.(string) },
				sig: in.intern(&RType{Kind: KFunc, Key: "func() string", Str: "func() string", Results: []*RType{s}})}
		}
		if name == "RuntimeError" {
			return &method{native: func(Value) Value { return nil }, sig: in.intern(&RType{Kind: KFunc, Key: "func()", Str: "func()"})}
		}
		return nil
	}
	ms, def, base := in.methodSet(t)
	for i := range ms {
		ent := &ms[i]
		if ent.Name != name || ent.Pkg != pkg {
			continue
		}
		e := (*env)(nil)
		if base.IsNamed() && len(base.Targs) > 0 {
			e = def.pkg.defEnv(def.td, base.Targs)
		}
		m := &method{path: ent.Path}
		if ent.Method < 0 {
			// promoted from an embedded interface
			it := def.pkg.typ(ent.RecvType, e)
			for _, im := range it.Under().IMethods {
				if im.Name == name && im.Pkg == pkg {
					m.sig = im.Sig
				}
			}
			m.iname, m.ipkg = name, pkg
			return m
		}
		sym := def.pkg.f.SymRefs[ent.Method]
		rt := def.pkg.typ(ent.RecvType, e) // the named type declaring the method
		dp := in.pkgs[sym.Pkg]
		if dp == nil {
			panic(interpError("method of unknown package " + sym.Pkg))
		}
		fi, ok := dp.funcByName[sym.Name]
		if !ok {
			panic(interpError("no function " + sym.Pkg + "." + sym.Name))
		}
		m.ptrRecv = strings.HasPrefix(sym.Name, "(*")
		fn := &dp.f.Funcs[fi]
		m.clos = in.funcClosure(dp, fi, dp.funcEnv(fn, rt.Targs))
		m.sig = m.clos.sig
		return m
	}
	return nil
}

// callMethod invokes m on receiver v of type t (adjusting through the
// embedding path, then & or *).
func (in *Interp) callMethod(m *method, v Value, t *RType, args []Value) []Value {
	if m.native != nil {
		return []Value{m.native(v)}
	}
	recv := in.adjustRecv(m, v, t)
	if m.clos == nil {
		inner := recv.(Iface)
		if inner.T == nil {
			in.runtimeNilDeref()
		}
		mm := in.lookupMethod(inner.T, m.iname, m.ipkg)
		return in.callMethod(mm, inner.V, inner.T, args)
	}
	return in.call(m.clos, append([]Value{recv}, args...))
}

func (in *Interp) adjustRecv(m *method, v Value, t *RType) Value {
	var addr *Value // the receiver's address when it is reached through a pointer
	cur := v
	ct := t
	if ct.Under().Kind == KPointer {
		p := cur.(*Value)
		if len(m.path) > 0 || !m.ptrRecv {
			if p == nil {
				in.runtimeNilDeref()
			}
			addr, cur, ct = p, *p, ct.Under().Elem
		}
	}
	for _, idx := range m.path {
		if ct.Under().Kind == KPointer {
			p := cur.(*Value)
			if p == nil {
				in.runtimeNilDeref()
			}
			cur, ct = *p, ct.Under().Elem
		}
		s := cur.(*Struct)
		addr = &s.F[idx]
		cur, ct = s.F[idx], ct.Under().Fields[idx].Type
	}
	if m.clos == nil {
		return cur
	}
	isPtr := ct.Under().Kind == KPointer
	switch {
	case m.ptrRecv && !isPtr:
		if addr == nil {
			panic(interpError("pointer method on an unaddressable receiver"))
		}
		return addr
	case !m.ptrRecv && isPtr:
		p := cur.(*Value)
		if p == nil {
			in.runtimeNilDeref()
		}
		return copyVal(*p)
	case m.ptrRecv:
		return cur
	}
	return copyVal(cur)
}

// implements reports whether dynamic type t has every method of
// interface it (by name and signature).
func (in *Interp) implements(t, it *RType) (bool, string) {
	for _, im := range it.Under().IMethods {
		m := in.lookupMethod(t, im.Name, im.Pkg)
		if m == nil || m.sig == nil || m.sig.Key != im.Sig.Key {
			return false, im.Name
		}
	}
	return true, ""
}

var _ = path.Base
var _ = strconv.Itoa
