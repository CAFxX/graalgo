//go:build imgcheck_oracle

package main

// The gc leg's check of the walker itself: every fact main.go reads from a
// compiler-emitted descriptor must be the answer of gc's own package reflect
// for that type. With it, "the walker prints the same facts on graalgo's
// images" means "reflect would give gc's answers on them". Using
// reflect.Type.Method here also makes gc's linker keep the type of every
// exported method (cmd/link deadcode's ReflectMethod), which main.go prints.

import (
	"os"
	"reflect"
	"unsafe"
)

//go:linkname toType reflect.toType
func toType(t *abiType) reflect.Type

func oracle(w *walker) {
	bad := 0
	for t := range w.seen {
		rt := toType(t)
		fail := func(what string) {
			bad++
			w.lines = append(w.lines, "ORACLE MISMATCH "+t.str()+": "+what)
		}
		check := func(what string, ok bool) {
			if !ok {
				fail(what)
			}
		}
		check("String", rt.String() == t.str())
		check("Kind", uint8(rt.Kind()) == t.Kind_)
		check("Size", rt.Size() == t.Size_)
		check("Align", rt.Align() == int(t.Align_))
		check("FieldAlign", rt.FieldAlign() == int(t.FieldAlign_))
		check("Comparable", rt.Comparable() == (t.Equal != nil))
		check("named", (rt.Name() != "") == (t.TFlag&tflagNamed != 0))
		u := t.uncommon()
		if u != nil {
			check("PkgPath", rt.Name() == "" || rt.PkgPath() == t.nameOff(u.PkgPath).name())
		}
		_ = rt.Method // the linker keeps exported methods' types
		switch t.Kind_ {
		case kInterface:
			it := (*interfaceType)(unsafe.Pointer(t))
			check("NumMethod", rt.NumMethod() == len(it.Methods))
			for i := 0; i < rt.NumMethod() && i < len(it.Methods); i++ {
				m := rt.Method(i)
				check("Method name "+m.Name, m.Name == t.nameOff(it.Methods[i].Name).name())
				check("Method type "+m.Name, m.Type.String() == t.typeOff(it.Methods[i].Typ).str())
			}
		default:
			x := 0
			var ms []abiMethod
			if u != nil {
				x, ms = int(u.Xcount), u.methods()
			}
			check("NumMethod", rt.NumMethod() == x)
			for i := 0; i < rt.NumMethod() && i < x; i++ {
				m := rt.Method(i)
				check("Method name "+m.Name, m.Name == t.nameOff(ms[i].Name).name())
				// reflect's Method(i).Type has the receiver first.
				mt := (*funcType)(unsafe.Pointer(t.typeOff(ms[i].Mtyp)))
				check("Method in "+m.Name, m.Type.NumIn() == int(mt.InCount)+1 && m.Type.NumOut() == int(mt.OutCount&(1<<15-1)))
			}
		}
		switch t.Kind_ {
		case kArray:
			a := (*arrayType)(unsafe.Pointer(t))
			check("Elem", rt.Elem() == toType(a.Elem))
			check("Len", rt.Len() == int(a.Len))
			check("Slice", reflect.SliceOf(rt.Elem()) == toType(a.Slice))
		case kChan:
			c := (*chanType)(unsafe.Pointer(t))
			check("Elem", rt.Elem() == toType(c.Elem))
			check("ChanDir", int(rt.ChanDir()) == c.Dir)
		case kPointer:
			check("Elem", rt.Elem() == toType((*ptrType)(unsafe.Pointer(t)).Elem))
		case kSlice:
			check("Elem", rt.Elem() == toType((*sliceType)(unsafe.Pointer(t)).Elem))
		case kMap:
			m := (*mapType)(unsafe.Pointer(t))
			check("Key", rt.Key() == toType(m.Key))
			check("Elem", rt.Elem() == toType(m.Elem))
		case kFunc:
			f := (*funcType)(unsafe.Pointer(t))
			ps := f.params()
			check("NumIn", rt.NumIn() == int(f.InCount))
			check("NumOut", rt.NumOut() == int(f.OutCount&(1<<15-1)))
			check("IsVariadic", rt.IsVariadic() == (f.OutCount&(1<<15) != 0))
			for i := 0; i < rt.NumIn() && i < len(ps); i++ {
				check("In", rt.In(i) == toType(ps[i]))
			}
			for i := 0; i < rt.NumOut() && rt.NumIn()+i < len(ps); i++ {
				check("Out", rt.Out(i) == toType(ps[rt.NumIn()+i]))
			}
		case kStruct:
			st := (*structType)(unsafe.Pointer(t))
			check("NumField", rt.NumField() == len(st.Fields))
			for i := 0; i < rt.NumField() && i < len(st.Fields); i++ {
				f, g := rt.Field(i), &st.Fields[i]
				check("Field name "+f.Name, f.Name == g.Name.name())
				check("Field type "+f.Name, f.Type == toType(g.Typ))
				check("Field offset "+f.Name, f.Offset == g.Offset)
				check("Field tag "+f.Name, string(f.Tag) == g.Name.tag())
				check("Field embedded "+f.Name, f.Anonymous == (*g.Name.data(0)&8 != 0))
				check("Field exported "+f.Name, f.IsExported() == (*g.Name.data(0)&1 != 0))
				check("Field pkgpath "+f.Name, f.IsExported() || f.PkgPath == st.PkgPath.name())
			}
		}
	}
	os.Stdout.WriteString("oracle: " + itoa(len(w.seen)) + " descriptors checked against reflect, " + itoa(bad) + " mismatches\n")
}
