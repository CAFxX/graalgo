package girinterp

import (
	"fmt"
	"strconv"
	"strings"

	"graalgo-toolexec/ir"
)

// Kind is a run-time type kind.
type Kind int

const (
	KInvalid Kind = iota
	KBool
	KInt
	KInt8
	KInt16
	KInt32
	KInt64
	KUint
	KUint8
	KUint16
	KUint32
	KUint64
	KUintptr
	KFloat32
	KFloat64
	KComplex64
	KComplex128
	KString
	KUnsafePointer
	KPointer
	KSlice
	KArray
	KMap
	KChan
	KStruct
	KFunc
	KInterface
	KTuple
)

var basicKind = map[ir.BasicKind]Kind{
	ir.BasicKindBool: KBool, ir.BasicKindInt: KInt, ir.BasicKindInt8: KInt8, ir.BasicKindInt16: KInt16,
	ir.BasicKindInt32: KInt32, ir.BasicKindInt64: KInt64, ir.BasicKindUint: KUint, ir.BasicKindUint8: KUint8,
	ir.BasicKindUint16: KUint16, ir.BasicKindUint32: KUint32, ir.BasicKindUint64: KUint64,
	ir.BasicKindUintptr: KUintptr, ir.BasicKindFloat32: KFloat32, ir.BasicKindFloat64: KFloat64,
	ir.BasicKindComplex64: KComplex64, ir.BasicKindComplex128: KComplex128, ir.BasicKindString: KString,
	ir.BasicKindUnsafePointer: KUnsafePointer,
}

var basicNames = map[Kind]string{
	KBool: "bool", KInt: "int", KInt8: "int8", KInt16: "int16", KInt32: "int32", KInt64: "int64",
	KUint: "uint", KUint8: "uint8", KUint16: "uint16", KUint32: "uint32", KUint64: "uint64",
	KUintptr: "uintptr", KFloat32: "float32", KFloat64: "float64", KComplex64: "complex64",
	KComplex128: "complex128", KString: "string", KUnsafePointer: "unsafe.Pointer",
}

// RType is a run-time type: identity is the canonical key (the same key
// format as ir.TypeKeyer, recomputed here after type-parameter
// substitution), interned per program.
type RType struct {
	Kind Kind
	Key  string // canonical key
	Str  string // gc's type string (runtime/reflect String form)

	// Named types.
	NamedPkg  string // declaring package path ("" for unnamed and universe types)
	NamedName string // gc symbol name (local types: Name·Func·N)
	Targs     []*RType
	def       *typeDef // declaring package's TYPEDEFS entry (nil for universe)
	under     *RType   // resolved lazily (recursive types)
	underFn   func() *RType

	// Structure (of the type itself when unnamed).
	Elem, KeyT     *RType
	Len            int64
	Dir            ir.ChanDir
	Fields         []RField
	Params         []*RType
	Results        []*RType
	Variadic       bool
	IMethods       []IMethod
	Tuple          []*RType
	unnamedMethods *typeDef // unnamed struct with promoted methods
}

// RField is a struct field.
type RField struct {
	Name     string
	Pkg      string
	Type     *RType
	Embedded bool
	Tag      string
}

// IMethod is an interface method.
type IMethod struct {
	Name, Pkg string
	Sig       *RType
}

// Under returns the underlying type.
func (t *RType) Under() *RType {
	if t.NamedName == "" {
		return t
	}
	if t.under == nil {
		t.under = t.underFn()
		if t.under.NamedName != "" {
			t.under = t.under.Under()
		}
		t.Kind = t.under.Kind
	}
	return t.under
}

// IsNamed reports a defined type.
func (t *RType) IsNamed() bool { return t.NamedName != "" }

func (t *RType) String() string { return t.Str }

// typeDef is a TYPEDEFS entry with the package that declares it.
type typeDef struct {
	pkg *Pkg
	td  *ir.TypeDef
}

// env maps type parameters ("owner.index") to their type arguments.
type env struct {
	m   map[string]*RType
	key string
}

func (e *env) lookup(k string) *RType {
	if e == nil {
		return nil
	}
	return e.m[k]
}

// extend returns e with the given bindings added.
func (e *env) extend(keys []string, ts []*RType) *env {
	if len(keys) == 0 {
		return e
	}
	n := &env{m: map[string]*RType{}}
	if e != nil {
		for k, v := range e.m {
			n.m[k] = v
		}
	}
	for i, k := range keys {
		n.m[k] = ts[i]
	}
	var parts []string
	for k, v := range n.m {
		parts = append(parts, k+"="+v.Key)
	}
	sortStrings(parts)
	n.key = strings.Join(parts, ";")
	return n
}

func (in *Interp) intern(t *RType) *RType {
	if old, ok := in.types[t.Key]; ok {
		return old
	}
	in.types[t.Key] = t
	return t
}

func (in *Interp) basic(k Kind) *RType {
	return in.intern(&RType{Kind: k, Key: basicNames[k], Str: basicNames[k]})
}

// tparamKey is the env key of a TypeParam entry.
func (p *Pkg) tparamKey(tp *ir.TypeParamType) string {
	return p.f.SymRefs[tp.Owner].Name + "." + strconv.FormatUint(tp.Index, 10)
}

// mentionsTP reports whether type id mentions a type parameter (cached).
func (p *Pkg) mentionsTP(id ir.TypeID) bool {
	if id == 0 {
		return false
	}
	if v, ok := p.tpCache[id]; ok {
		return v
	}
	p.tpCache[id] = false // cycle guard (named types recurse only via targs)
	var r bool
	switch t := p.f.Types[id-1].Type.(type) {
	case *ir.TypeParamType:
		r = true
	case *ir.PointerType:
		r = p.mentionsTP(t.Elem)
	case *ir.SliceType:
		r = p.mentionsTP(t.Elem)
	case *ir.ArrayType:
		r = p.mentionsTP(t.Elem)
	case *ir.MapType:
		r = p.mentionsTP(t.Key) || p.mentionsTP(t.Elem)
	case *ir.ChanType:
		r = p.mentionsTP(t.Elem)
	case *ir.StructType:
		for _, f := range t.Fields {
			r = r || p.mentionsTP(f.Type)
		}
	case *ir.SignatureType:
		for _, x := range append(append([]ir.TypeID{}, t.Params...), t.Results...) {
			r = r || p.mentionsTP(x)
		}
	case *ir.InterfaceType:
		for _, m := range t.Methods {
			r = r || p.mentionsTP(m.Sig)
		}
	case *ir.TupleType:
		for _, x := range t.Elems {
			r = r || p.mentionsTP(x)
		}
	case *ir.NamedType:
		for _, x := range t.Targs {
			r = r || p.mentionsTP(x)
		}
	}
	p.tpCache[id] = r
	return r
}

// typ resolves type id of p under e.
func (p *Pkg) typ(id ir.TypeID, e *env) *RType {
	if id == 0 {
		return nil
	}
	generic := p.mentionsTP(id)
	if !generic {
		if t := p.typeCache[id]; t != nil {
			return t
		}
	} else {
		ck := cacheKey{id, e.key}
		if t := p.genCache[ck]; t != nil {
			return t
		}
	}
	t := p.build(id, e)
	if !generic {
		p.typeCache[id] = t
		// The emitter's key must be the key of the structure it emitted.
		if want := p.f.Types[id-1].Key; t.Key != want && !p.in.skipKeyCheck(p.f.Types[id-1].Type) {
			panic(interpError(fmt.Sprintf("type key mismatch: GIR %q, structure %q", want, t.Key)))
		}
	} else {
		p.genCache[cacheKey{id, e.key}] = t
	}
	return t
}

type cacheKey struct {
	id  ir.TypeID
	env string
}

// skipKeyCheck: interface keys encode type sets the interpreter does not
// recompute (constraints), and the untyped kinds have no run-time form.
func (in *Interp) skipKeyCheck(t ir.Type) bool {
	switch t := t.(type) {
	case *ir.InterfaceType:
		return len(t.Embeddeds) > 0
	case *ir.UnionType, *ir.TypeParamType:
		return true
	case *ir.BasicType:
		return t.Kind >= ir.BasicKindUntypedBool
	}
	return false
}

func qualified(pkg, name string) string {
	if pkg == "" {
		return name
	}
	return pkg + "." + name
}

func (p *Pkg) build(id ir.TypeID, e *env) *RType {
	in := p.in
	switch t := p.f.Types[id-1].Type.(type) {
	case *ir.BasicType:
		if k, ok := basicKind[t.Kind]; ok {
			return in.basic(k)
		}
		return in.intern(&RType{Kind: KInvalid, Key: p.f.Types[id-1].Key, Str: p.f.Types[id-1].Key})
	case *ir.PointerType:
		return in.pointerTo(p.typ(t.Elem, e))
	case *ir.SliceType:
		return in.sliceOf(p.typ(t.Elem, e))
	case *ir.ArrayType:
		el := p.typ(t.Elem, e)
		n := strconv.FormatUint(t.Len, 10)
		return in.intern(&RType{Kind: KArray, Key: "[" + n + "]" + el.Key, Str: "[" + n + "]" + el.Str, Elem: el, Len: int64(t.Len)})
	case *ir.MapType:
		k, v := p.typ(t.Key, e), p.typ(t.Elem, e)
		return in.intern(&RType{Kind: KMap, Key: "map[" + k.Key + "]" + v.Key, Str: "map[" + k.Str + "]" + v.Str, KeyT: k, Elem: v})
	case *ir.ChanType:
		el := p.typ(t.Elem, e)
		pre := map[ir.ChanDir]string{ir.ChanDirBoth: "chan ", ir.ChanDirSend: "chan<- ", ir.ChanDirRecv: "<-chan "}[t.Dir]
		ek, es := el.Key, el.Str
		if el.Kind == KChan && !el.IsNamed() {
			ek, es = "("+ek+")", "("+es+")"
		}
		return in.intern(&RType{Kind: KChan, Key: pre + ek, Str: pre + es, Elem: el, Dir: t.Dir})
	case *ir.StructType:
		r := &RType{Kind: KStruct}
		var kb, sb strings.Builder
		kb.WriteString("struct {")
		sb.WriteString("struct {")
		for i, f := range t.Fields {
			ft := p.typ(f.Type, e)
			r.Fields = append(r.Fields, RField{Name: f.Name, Pkg: f.Pkg, Type: ft, Embedded: f.Embedded, Tag: f.Tag})
			if i > 0 {
				kb.WriteByte(';')
				sb.WriteByte(';')
			}
			kb.WriteByte(' ')
			sb.WriteByte(' ')
			if f.Embedded {
				kb.WriteString("embed ")
				sb.WriteString(ft.Str)
			} else {
				kb.WriteString(qualified(f.Pkg, f.Name) + " ")
				sb.WriteString(f.Name + " " + ft.Str)
			}
			kb.WriteString(ft.Key)
			if f.Tag != "" {
				kb.WriteString(" " + strconv.Quote(f.Tag))
				sb.WriteString(" " + strconv.Quote(f.Tag))
			}
		}
		kb.WriteString(" }")
		sb.WriteString(" }")
		if len(t.Fields) == 0 {
			r.Key, r.Str = "struct { }", "struct {}"
		} else {
			r.Key, r.Str = kb.String(), sb.String()
		}
		r = in.intern(r)
		if r.unnamedMethods == nil {
			r.unnamedMethods = in.structMethods[r.Key]
		}
		return r
	case *ir.SignatureType:
		r := &RType{Kind: KFunc, Variadic: t.Variadic}
		for _, x := range t.Params {
			r.Params = append(r.Params, p.typ(x, e))
		}
		for _, x := range t.Results {
			r.Results = append(r.Results, p.typ(x, e))
		}
		k, s := sigString(r.Params, r.Results, r.Variadic)
		r.Key, r.Str = "func"+k, "func"+s
		return in.intern(r)
	case *ir.InterfaceType:
		r := &RType{Kind: KInterface}
		var kb, sb strings.Builder
		for i, m := range t.Methods {
			sig := p.typ(m.Sig, e)
			r.IMethods = append(r.IMethods, IMethod{Name: m.Name, Pkg: m.Pkg, Sig: sig})
			if i > 0 {
				kb.WriteByte(';')
				sb.WriteByte(';')
			}
			ks, ss := sigString(sig.Params, sig.Results, sig.Variadic)
			kb.WriteString(" " + qualified(m.Pkg, m.Name) + ks)
			sb.WriteString(" " + m.Name + ss)
		}
		if len(t.Methods) == 0 {
			r.Key, r.Str = "interface {}", "interface {}"
		} else {
			r.Key, r.Str = "interface {"+kb.String()+" }", "interface {"+sb.String()+" }"
		}
		if len(t.Embeddeds) > 0 {
			r.Key = p.f.Types[id-1].Key // constraint: never a value type
		}
		return in.intern(r)
	case *ir.TupleType:
		r := &RType{Kind: KTuple}
		var ks, ss []string
		for _, x := range t.Elems {
			et := p.typ(x, e)
			r.Tuple = append(r.Tuple, et)
			ks, ss = append(ks, et.Key), append(ss, et.Str)
		}
		r.Key, r.Str = "("+strings.Join(ks, ", ")+")", "("+strings.Join(ss, ", ")+")"
		return in.intern(r)
	case *ir.NamedType:
		sym := p.f.SymRefs[t.Sym]
		var targs []*RType
		for _, x := range t.Targs {
			targs = append(targs, p.typ(x, e))
		}
		return in.named(sym.Pkg, sym.Name, targs)
	case *ir.TypeParamType:
		if r := e.lookup(p.tparamKey(t)); r != nil {
			return r
		}
		panic(interpError("unbound type parameter " + p.tparamKey(t)))
	case *ir.UnionType:
		return in.intern(&RType{Kind: KInvalid, Key: p.f.Types[id-1].Key})
	}
	panic(interpError(fmt.Sprintf("type %T", p.f.Types[id-1].Type)))
}

func sigString(params, results []*RType, variadic bool) (key, str string) {
	var kb, sb strings.Builder
	kb.WriteByte('(')
	sb.WriteByte('(')
	for i, pt := range params {
		if i > 0 {
			kb.WriteString(", ")
			sb.WriteString(", ")
		}
		if variadic && i == len(params)-1 {
			kb.WriteString("..." + pt.Elem.Key)
			sb.WriteString("..." + pt.Elem.Str)
		} else {
			kb.WriteString(pt.Key)
			sb.WriteString(pt.Str)
		}
	}
	kb.WriteByte(')')
	sb.WriteByte(')')
	switch len(results) {
	case 0:
	case 1:
		kb.WriteString(" " + results[0].Key)
		sb.WriteString(" " + results[0].Str)
	default:
		var ks, ss []string
		for _, r := range results {
			ks, ss = append(ks, r.Key), append(ss, r.Str)
		}
		kb.WriteString(" (" + strings.Join(ks, ", ") + ")")
		sb.WriteString(" (" + strings.Join(ss, ", ") + ")")
	}
	return kb.String(), sb.String()
}

func (in *Interp) pointerTo(el *RType) *RType {
	return in.intern(&RType{Kind: KPointer, Key: "*" + el.Key, Str: "*" + el.Str, Elem: el})
}

func (in *Interp) sliceOf(el *RType) *RType {
	return in.intern(&RType{Kind: KSlice, Key: "[]" + el.Key, Str: "[]" + el.Str, Elem: el})
}

// named returns the defined type (pkg, name) instantiated with targs.
func (in *Interp) named(pkg, name string, targs []*RType) *RType {
	key := qualified(pkg, name)
	str := name
	if pkg != "" {
		pp := in.pkgs[pkg]
		pname := pkg
		if pp != nil {
			pname = pp.f.Pkg.Name
		}
		if i := strings.IndexByte(name, '·'); i >= 0 {
			str = name[:i] // local type: gc prints its source name
		}
		str = pname + "." + str
	}
	if len(targs) > 0 {
		var ks, ss []string
		for _, a := range targs {
			ks, ss = append(ks, a.Key), append(ss, a.Str)
		}
		key += "[" + strings.Join(ks, ",") + "]"
		str += "[" + strings.Join(ss, ",") + "]"
	}
	if t, ok := in.types[key]; ok {
		return t
	}
	t := &RType{Key: key, Str: str, NamedPkg: pkg, NamedName: name, Targs: targs}
	in.types[key] = t
	switch {
	case pkg == "" && name == "error":
		t.underFn = func() *RType { return in.errorIface() }
	case pkg == "" && name == "comparable":
		t.underFn = func() *RType { return in.intern(&RType{Kind: KInterface, Key: "interface { comparable }"}) }
	default:
		def := in.typedefs[qualified(pkg, name)]
		if def == nil {
			panic(interpError("no TYPEDEFS entry for " + qualified(pkg, name)))
		}
		t.def = def
		t.underFn = func() *RType {
			e := def.pkg.defEnv(def.td, targs)
			return def.pkg.typ(def.td.Underlying, e)
		}
	}
	t.Under()
	return t
}

// defEnv binds a typedef's type parameters to targs.
func (p *Pkg) defEnv(td *ir.TypeDef, targs []*RType) *env {
	if len(td.Tparams) != len(targs) {
		panic(interpError(fmt.Sprintf("typedef with %d tparams instantiated with %d targs", len(td.Tparams), len(targs))))
	}
	var keys []string
	for _, tp := range td.Tparams {
		keys = append(keys, p.tparamKey(p.f.Types[tp-1].Type.(*ir.TypeParamType)))
	}
	return (*env)(nil).extend(keys, targs)
}

func (in *Interp) errorIface() *RType {
	str := in.basic(KString)
	sig := in.intern(&RType{Kind: KFunc, Key: "func() string", Str: "func() string", Results: []*RType{str}})
	return in.intern(&RType{Kind: KInterface, Key: "interface { Error() string }", Str: "interface { Error() string }", IMethods: []IMethod{{Name: "Error", Sig: sig}}})
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
