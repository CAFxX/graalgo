package ir

import (
	"go/token"
	"go/types"
	"slices"
	"strconv"
	"strings"
)

// TypeKeyer computes canonical type keys (docs/arch/toolexec-design.md
// §3.4): a string per type, equal for identical types (types.Identical)
// and different otherwise, in gc's link-name style with full import paths
// (map[string]*net/http.Request). Two packages that mention the same type
// produce the same key, which is what lets the backend intern descriptors
// across packages.
//
// Names the type alone does not determine are supplied by the caller:
// LocalName for types declared inside functions (suffixed with the
// enclosing function and a declaration index, so two local types named T
// differ), and TypeParamOwner for type parameters (owner symbol + index).
type TypeKeyer struct {
	LocalName      func(*types.TypeName) string // "" if not local
	TypeParamOwner func(*types.TypeParam) string
}

// Key returns the canonical key of t.
func (k *TypeKeyer) Key(t types.Type) string {
	var b strings.Builder
	k.write(&b, t)
	return b.String()
}

var basicNames = map[types.BasicKind]string{
	types.Bool: "bool", types.Int: "int", types.Int8: "int8", types.Int16: "int16",
	types.Int32: "int32", types.Int64: "int64", types.Uint: "uint", types.Uint8: "uint8",
	types.Uint16: "uint16", types.Uint32: "uint32", types.Uint64: "uint64",
	types.Uintptr: "uintptr", types.Float32: "float32", types.Float64: "float64",
	types.Complex64: "complex64", types.Complex128: "complex128", types.String: "string",
	types.UnsafePointer: "unsafe.Pointer", types.UntypedBool: "untyped bool",
	types.UntypedInt: "untyped int", types.UntypedRune: "untyped rune",
	types.UntypedFloat: "untyped float", types.UntypedComplex: "untyped complex",
	types.UntypedString: "untyped string", types.UntypedNil: "untyped nil",
}

// qualifiedName returns name, prefixed with pkg's path when the name is
// unexported (unexported field and method names of different packages are
// different names).
func qualifiedName(pkg *types.Package, name string) string {
	if pkg == nil || token.IsExported(name) || name == "_" {
		return name
	}
	return pkg.Path() + "." + name
}

func (k *TypeKeyer) write(b *strings.Builder, t types.Type) {
	switch t := t.(type) {
	case *types.Alias:
		k.write(b, types.Unalias(t))
	case *types.Basic:
		b.WriteString(basicNames[t.Kind()])
	case *types.Pointer:
		b.WriteByte('*')
		k.write(b, t.Elem())
	case *types.Slice:
		b.WriteString("[]")
		k.write(b, t.Elem())
	case *types.Array:
		b.WriteString("[" + strconv.FormatInt(t.Len(), 10) + "]")
		k.write(b, t.Elem())
	case *types.Map:
		b.WriteString("map[")
		k.write(b, t.Key())
		b.WriteByte(']')
		k.write(b, t.Elem())
	case *types.Chan:
		switch t.Dir() {
		case types.SendRecv:
			b.WriteString("chan ")
		case types.SendOnly:
			b.WriteString("chan<- ")
		case types.RecvOnly:
			b.WriteString("<-chan ")
		}
		_, elemChan := types.Unalias(t.Elem()).(*types.Chan)
		if elemChan {
			b.WriteByte('(')
		}
		k.write(b, t.Elem())
		if elemChan {
			b.WriteByte(')')
		}
	case *types.Struct:
		b.WriteString("struct {")
		for i := range t.NumFields() {
			f := t.Field(i)
			if i > 0 {
				b.WriteByte(';')
			}
			b.WriteByte(' ')
			if f.Embedded() {
				b.WriteString("embed ")
			} else {
				b.WriteString(qualifiedName(f.Pkg(), f.Name()) + " ")
			}
			k.write(b, f.Type())
			if tag := t.Tag(i); tag != "" {
				b.WriteString(" " + strconv.Quote(tag))
			}
		}
		b.WriteString(" }")
	case *types.Signature:
		b.WriteString("func")
		k.writeSig(b, t)
	case *types.Interface:
		b.WriteString("interface {")
		n := 0
		sep := func() {
			if n > 0 {
				b.WriteByte(';')
			}
			b.WriteByte(' ')
			n++
		}
		for i := range t.NumEmbeddeds() {
			e := t.EmbeddedType(i)
			if _, isIface := e.Underlying().(*types.Interface); isIface {
				if named, ok := e.(*types.Named); !ok || named.Obj().Pkg() != nil || named.Obj().Name() != "comparable" {
					continue // its methods are in the method set below
				}
			}
			sep()
			k.write(b, e)
		}
		for i := range t.NumMethods() {
			m := t.Method(i)
			sep()
			b.WriteString(qualifiedName(m.Pkg(), m.Name()))
			k.writeSig(b, m.Type().(*types.Signature))
		}
		if n > 0 {
			b.WriteByte(' ')
		}
		b.WriteByte('}')
	case *types.Union:
		terms := make([]string, t.Len())
		for i := range t.Len() {
			tm := t.Term(i)
			s := k.Key(tm.Type())
			if tm.Tilde() {
				s = "~" + s
			}
			terms[i] = s
		}
		slices.Sort(terms)
		b.WriteString(strings.Join(terms, " | "))
	case *types.Tuple:
		b.WriteByte('(')
		for i := range t.Len() {
			if i > 0 {
				b.WriteString(", ")
			}
			k.write(b, t.At(i).Type())
		}
		b.WriteByte(')')
	case *types.Named:
		obj := t.Obj()
		switch {
		case obj.Pkg() == nil:
			b.WriteString(obj.Name()) // error, comparable
		default:
			name := ""
			if k.LocalName != nil {
				name = k.LocalName(t.Origin().Obj())
			}
			if name == "" {
				name = obj.Name()
			}
			b.WriteString(obj.Pkg().Path() + "." + name)
		}
		if targs := t.TypeArgs(); targs.Len() > 0 {
			b.WriteByte('[')
			for i := range targs.Len() {
				if i > 0 {
					b.WriteByte(',')
				}
				k.write(b, targs.At(i))
			}
			b.WriteByte(']')
		}
	case *types.TypeParam:
		b.WriteString("$" + k.TypeParamOwner(t) + "." + strconv.Itoa(t.Index()))
	default:
		panic("ir: no type key for " + t.String())
	}
}

func (k *TypeKeyer) writeSig(b *strings.Builder, s *types.Signature) {
	b.WriteByte('(')
	for i := range s.Params().Len() {
		if i > 0 {
			b.WriteString(", ")
		}
		pt := s.Params().At(i).Type()
		if s.Variadic() && i == s.Params().Len()-1 {
			b.WriteString("...")
			pt = pt.(*types.Slice).Elem()
		}
		k.write(b, pt)
	}
	b.WriteByte(')')
	switch r := s.Results(); r.Len() {
	case 0:
	case 1:
		b.WriteByte(' ')
		k.write(b, r.At(0).Type())
	default:
		b.WriteByte(' ')
		k.write(b, r)
	}
}
