package main

import (
	"go/ast"
	"go/types"
	"strings"
)

// P3-M8 (wave 2): synthetic reflect package (frontend overlay).//
// This file embeds the synthetic reflect source as text (type-checked on
// demand with the overlay importer) plus the registration/emission glue:
//   - syntheticReflectSource / syntheticReflectliteSource / syntheticIterSource
//   - reflectIntrinsicNames: the __reflect_* names intercepted at call sites
//   - isReflectIntrinsicFunc: call-site predicate
//   - exportReflectIntrinsic: emits the "reflectop" wire node
//
// The GoRType runtime table lives in phases/phase3 (p3reflect package);
// the wire "reflectop" node carries {"op","args","type","pkgpath"} and the
// Java ReflectOpNode dispatches on op. Shared-file edits (synth_machinery.go
// registration, mrdesugar_m5.go entries) are staged patches, not applied.

const syntheticReflectSource = `
// P3-M8 (wave 2): synthetic reflect package.
//
// REAL Go source, embedded as text and type-checked on demand with the
// overlay importer (no linknames, no asm — the 13 reflect asm files are
// superseded by the trampolines below). Exact gc 1.27.1 public API
// signatures.
//
// Architecture: the wire type table already carries everything reflect
// needs (field offsets/tags, method tables, signatures). The runtime
// GoRType table (phases/phase3/.../p3reflect, built from the wire table,
// canonical per GoTypeDescriptor) backs the __reflect_* intrinsics —
// each a small wire-node op mapped to an existing Java capability
// (GoCalls.invoke, ChanOps, Intrinsics.map*, the wire table). The
// synthetic Go mirrors gc's Value{typ,ptr,flag} layout; DeepEqual,
// Swapper, VisibleFields, StructTag are pure synthetic Go.
//
// Fail-fasts (named, with re-test triggers — never silent):
//   - iter family: Type.Methods/Fields/Ins/Outs/CanSeq/CanSeq2,
//     Value.Methods/Fields/Seq/Seq2, MapIter.Seq (needs the iter package)
//   - type constructors: ArrayOf/SliceOf/MapOf/ChanOf/FuncOf/PtrTo/
//     PointerTo/StructOf (needs runtime type synthesis)
//   - TypeAssert/TypeFor (needs generics in synthetic scope)
//   - Value.Pointer/UnsafeAddr/InterfaceData (uintptr escape — D5 traps)
package reflect

import (
	"errors"
	"iter"
	"strconv"
	"unsafe"
)

// A Kind represents the specific kind of type that a Type represents.
// The zero Kind is not a valid kind.
type Kind uint

const (
	Invalid Kind = iota
	Bool
	Int
	Int8
	Int16
	Int32
	Int64
	Uint
	Uint8
	Uint16
	Uint32
	Uint64
	Uintptr
	Float32
	Float64
	Complex64
	Complex128
	Array
	Chan
	Func
	Interface
	Map
	Pointer
	Slice
	String
	Struct
	UnsafePointer
)

// Ptr is the old name for the Pointer kind.
const Ptr = Pointer

func (k Kind) String() string {
	switch k {
	case Invalid:
		return "invalid"
	case Bool:
		return "bool"
	case Int:
		return "int"
	case Int8:
		return "int8"
	case Int16:
		return "int16"
	case Int32:
		return "int32"
	case Int64:
		return "int64"
	case Uint:
		return "uint"
	case Uint8:
		return "uint8"
	case Uint16:
		return "uint16"
	case Uint32:
		return "uint32"
	case Uint64:
		return "uint64"
	case Uintptr:
		return "uintptr"
	case Float32:
		return "float32"
	case Float64:
		return "float64"
	case Complex64:
		return "complex64"
	case Complex128:
		return "complex128"
	case Array:
		return "array"
	case Chan:
		return "chan"
	case Func:
		return "func"
	case Interface:
		return "interface"
	case Map:
		return "map"
	case Pointer:
		return "ptr"
	case Slice:
		return "slice"
	case String:
		return "string"
	case Struct:
		return "struct"
	case UnsafePointer:
		return "unsafe.Pointer"
	}
	return "kind" + reflectItoa(int(k))
}

// ChanDir represents a channel type's direction.
type ChanDir int

const (
	RecvDir ChanDir = 1 << iota // <-chan
	SendDir                     // chan<-
	BothDir = RecvDir | SendDir // chan
)

func (d ChanDir) String() string {
	switch d {
	case RecvDir:
		return "<-chan"
	case SendDir:
		return "chan<-"
	case BothDir:
		return "chan"
	}
	return "ChanDir(" + reflectItoa(int(d)) + ")"
}

// Method represents a single method.
type Method struct {
	Name string
	// PkgPath is the package path that qualifies a lower case (unexported)
	// method name. It is empty for upper case (exported) method names.
	PkgPath string
	Type    Type
	Func    Value
	Index   int
}

// IsExported reports whether the method is exported.
func (m Method) IsExported() bool {
	return m.PkgPath == ""
}

// A StructField's Offset field is the byte offset within the struct.
type StructField struct {
	Name string
	// PkgPath is the package path that qualifies a lower case (unexported)
	// field name. It is empty for upper case (exported) field names.
	PkgPath string
	Type    Type
	Tag     StructTag
	Offset  uintptr
	Index   []int
	Anonymous bool
}

// IsExported reports whether the field is exported.
func (f StructField) IsExported() bool {
	return f.PkgPath == ""
}

// A StructTag is the tag string in a struct field.
type StructTag string

// Get returns the value associated with key in the tag string.
// If there is no such key in the tag, Get returns the empty string.
func (tag StructTag) Get(key string) string {
	v, _ := tag.Lookup(key)
	return v
}

// Lookup returns the value associated with key in the tag string.
func (tag StructTag) Lookup(key string) (value string, ok bool) {
	p := __reflect_taglookup(tag, key)
	return p.value, p.ok
}

// SelectDir describes the communication direction of a select case.
type SelectDir int

const (
	SelectSend SelectDir = iota
	SelectRecv
	SelectDefault
)

// A SelectCase describes a single case in Select.
type SelectCase struct {
	Dir  SelectDir
	Chan Value
	Send Value
}

// A MapIter is an iterator for ranging over a map.
type MapIter struct {
	h unsafe.Pointer // handle to the Java iterator state
}

// Key returns the key of the iterator's current map entry.
func (it *MapIter) Key() Value {
	return __reflect_mapiterkey(it.h)
}

// Value returns the value of the iterator's current map entry.
func (it *MapIter) Value() Value {
	return __reflect_mapiterval(it.h)
}

// Next advances the map iterator and reports whether there is another
// entry remaining.
func (it *MapIter) Next() bool {
	return __reflect_mapiternext(it.h)
}

// Reset seeks the iterator to the beginning of the map v.
func (it *MapIter) Reset(v Value) {
	__reflect_mapiterreset(it.h, v)
}

// Seq returns an iterator over the map's entries.
// M8 scope: fail-fast (needs the iter package).
func (it *MapIter) Seq() iter.Seq2[Value, Value] {
	panic("reflect: MapIter.Seq: not in M8 scope (needs iter; re-test when the iter package lands)")
}

// ValueError is the error type for value-method misuse (gc-exact).
type ValueError struct {
	Method string
	Kind   Kind
}

func (e *ValueError) Error() string {
	if e.Kind == Invalid {
		return "reflect: call of " + e.Method + " on zero Value"
	}
	return "reflect: call of " + e.Method + " on " + e.Kind.String() + " Value"
}

// Value flag bits (gc-exact values).
type flag uintptr

const (
	flagKindWidth   = 5 // there are 27 kinds
	flagKindMask    = 1<<flagKindWidth - 1
	flagStickyRO    = 1 << 5
	flagEmbedRO     = 1 << 6
	flagIndir       = 1 << 7
	flagAddr        = 1 << 8
	flagMethod      = 1 << 9
	flagMethodShift = 10
	flagRO          = flagStickyRO | flagEmbedRO
)

// Value is the reflection interface to a Go value.
//
// The synthetic layout mirrors gc: typ is the rtype token, ptr the
// interned Ref handle, flag the kind/RO/addr bits. The zero Value is
// invalid (typ == nil).
type Value struct {
	typ unsafe.Pointer
	ptr unsafe.Pointer
	flag
}

// Type is the reflection interface to a Go type.
type Type interface {
	Align() int
	FieldAlign() int
	Method(int) Method
	Methods() iter.Seq[Method]
	MethodByName(string) (Method, bool)
	NumMethod() int
	Name() string
	PkgPath() string
	Size() uintptr
	String() string
	Kind() Kind
	Implements(u Type) bool
	AssignableTo(u Type) bool
	ConvertibleTo(u Type) bool
	Comparable() bool
	Bits() int
	ChanDir() ChanDir
	IsVariadic() bool
	Elem() Type
	Field(i int) StructField
	FieldByIndex(index []int) StructField
	FieldByName(name string) (StructField, bool)
	FieldByNameFunc(match func(string) bool) (StructField, bool)
	Fields() iter.Seq[StructField]
	Ins() iter.Seq[Type]
	Outs() iter.Seq[Type]
	CanSeq() bool
	CanSeq2() bool
	Key() Type
	Len() int
	NumField() int
	In(i int) Type
	NumIn() int
	Out(i int) Type
	NumOut() int
	OverflowComplex(x complex128) bool
	OverflowFloat(x float64) bool
	OverflowInt(x int64) bool
	OverflowUint(x uint64) bool
}

// rtype is the runtime implementation of Type: a thin wrapper over the
// GoRType token (an interned handle identifying the canonical type).
type rtype struct {
	tok unsafe.Pointer
}

// ------------------------------------------------------------------
// Intrinsics: each is intercepted at call sites into the "reflectop"
// wire node (isReflectIntrinsicFunc / exportReflectIntrinsic). Declared
// here with exact working signatures for type-checking; the panic
// bodies never execute.
// ------------------------------------------------------------------

// Type-token operations.
func __reflect_tKind(tok unsafe.Pointer) Kind                  { panic("reflect: intrinsic __reflect_tKind") }
func __reflect_tString(tok unsafe.Pointer) string              { panic("reflect: intrinsic __reflect_tString") }
func __reflect_tName(tok unsafe.Pointer) string                { panic("reflect: intrinsic __reflect_tName") }
func __reflect_tPkgPath(tok unsafe.Pointer) string             { panic("reflect: intrinsic __reflect_tPkgPath") }
func __reflect_tSize(tok unsafe.Pointer) uintptr               { panic("reflect: intrinsic __reflect_tSize") }
func __reflect_tAlign(tok unsafe.Pointer) int                  { panic("reflect: intrinsic __reflect_tAlign") }
func __reflect_tFieldAlign(tok unsafe.Pointer) int             { panic("reflect: intrinsic __reflect_tFieldAlign") }
func __reflect_tBits(tok unsafe.Pointer) int                  { panic("reflect: intrinsic __reflect_tBits") }
func __reflect_tChanDir(tok unsafe.Pointer) ChanDir             { panic("reflect: intrinsic __reflect_tChanDir") }
func __reflect_tLen(tok unsafe.Pointer) int                   { panic("reflect: intrinsic __reflect_tLen") }
func __reflect_tNumField(tok unsafe.Pointer) int              { panic("reflect: intrinsic __reflect_tNumField") }
func __reflect_tNumMethod(tok unsafe.Pointer) int             { panic("reflect: intrinsic __reflect_tNumMethod") }
func __reflect_tNumIn(tok unsafe.Pointer) int                 { panic("reflect: intrinsic __reflect_tNumIn") }
func __reflect_tNumOut(tok unsafe.Pointer) int                { panic("reflect: intrinsic __reflect_tNumOut") }
func __reflect_tIsVariadic(tok unsafe.Pointer) bool           { panic("reflect: intrinsic __reflect_tIsVariadic") }
func __reflect_tComparable(tok unsafe.Pointer) bool           { panic("reflect: intrinsic __reflect_tComparable") }
func __reflect_tField(tok unsafe.Pointer, i int) StructField  { panic("reflect: intrinsic __reflect_tField") }
func __reflect_tMethod(tok unsafe.Pointer, i int) Method      { panic("reflect: intrinsic __reflect_tMethod") }
func __reflect_tElem(tok unsafe.Pointer) unsafe.Pointer       { panic("reflect: intrinsic __reflect_tElem") }
func __reflect_tKey(tok unsafe.Pointer) unsafe.Pointer        { panic("reflect: intrinsic __reflect_tKey") }
func __reflect_tIn(tok unsafe.Pointer, i int) unsafe.Pointer  { panic("reflect: intrinsic __reflect_tIn") }
func __reflect_tOut(tok unsafe.Pointer, i int) unsafe.Pointer { panic("reflect: intrinsic __reflect_tOut") }
func __reflect_tImplements(tok, u unsafe.Pointer) bool        { panic("reflect: intrinsic __reflect_tImplements") }
func __reflect_tAssignableTo(tok, u unsafe.Pointer) bool      { panic("reflect: intrinsic __reflect_tAssignableTo") }
func __reflect_tConvertibleTo(tok, u unsafe.Pointer) bool     { panic("reflect: intrinsic __reflect_tConvertibleTo") }
func __reflect_tPtrTo(tok unsafe.Pointer) unsafe.Pointer      { panic("reflect: intrinsic __reflect_tPtrTo") }

// Value operations.
func __reflect_typeof(v any) unsafe.Pointer              { panic("reflect: intrinsic __reflect_typeof") }
func __reflect_valueof(v any) Value                      { panic("reflect: intrinsic __reflect_valueof") }
func __reflect_new(tok unsafe.Pointer) Value             { panic("reflect: intrinsic __reflect_new") }
func __reflect_newat(tok, p unsafe.Pointer) Value        { panic("reflect: intrinsic __reflect_newat") }
func __reflect_field(v Value, i int) Value               { panic("reflect: intrinsic __reflect_field") }
func __reflect_index(v Value, i int) Value               { panic("reflect: intrinsic __reflect_index") }
func __reflect_elem(v Value) Value                       { panic("reflect: intrinsic __reflect_elem") }
func __reflect_len(v Value) int                          { panic("reflect: intrinsic __reflect_len") }
func __reflect_cap(v Value) int                          { panic("reflect: intrinsic __reflect_cap") }
func __reflect_isnil(v Value) bool                       { panic("reflect: intrinsic __reflect_isnil") }
func __reflect_iszero(v Value) bool                      { panic("reflect: intrinsic __reflect_iszero") }
func __reflect_getint(v Value) int64                     { panic("reflect: intrinsic __reflect_getint") }
func __reflect_getuint(v Value) uint64                   { panic("reflect: intrinsic __reflect_getuint") }
func __reflect_getfloat(v Value) float64                 { panic("reflect: intrinsic __reflect_getfloat") }
func __reflect_getcomplex(v Value) complex128            { panic("reflect: intrinsic __reflect_getcomplex") }
func __reflect_getbool(v Value) bool                     { panic("reflect: intrinsic __reflect_getbool") }
func __reflect_getstring(v Value) string                 { panic("reflect: intrinsic __reflect_getstring") }
func __reflect_getbytes(v Value) []byte                  { panic("reflect: intrinsic __reflect_getbytes") }
func __reflect_setint(v Value, x int64)                  { panic("reflect: intrinsic __reflect_setint") }
func __reflect_setuint(v Value, x uint64)                { panic("reflect: intrinsic __reflect_setuint") }
func __reflect_setfloat(v Value, x float64)              { panic("reflect: intrinsic __reflect_setfloat") }
func __reflect_setcomplex(v Value, x complex128)        { panic("reflect: intrinsic __reflect_setcomplex") }
func __reflect_setbool(v Value, x bool)                  { panic("reflect: intrinsic __reflect_setbool") }
func __reflect_setstring(v Value, x string)             { panic("reflect: intrinsic __reflect_setstring") }
func __reflect_setbytes(v Value, x []byte)              { panic("reflect: intrinsic __reflect_setbytes") }
func __reflect_set(v Value, x Value)                    { panic("reflect: intrinsic __reflect_set") }
func __reflect_setzero(v Value)                         { panic("reflect: intrinsic __reflect_setzero") }
func __reflect_setmapindex(v Value, key Value, elem Value) { panic("reflect: intrinsic __reflect_setmapindex") }
func __reflect_mapindex(v Value, key Value) Value        { panic("reflect: intrinsic __reflect_mapindex") }
func __reflect_interface(v Value) any                    { panic("reflect: intrinsic __reflect_interface") }
func __reflect_rawinterface(v Value) any                { panic("reflect: intrinsic __reflect_rawinterface") }
func __reflect_convert(v Value, tok unsafe.Pointer) Value { panic("reflect: intrinsic __reflect_convert") }
func __reflect_slice(v Value, i, j int) Value            { panic("reflect: intrinsic __reflect_slice") }
func __reflect_slice3(v Value, i, j, k int) Value        { panic("reflect: intrinsic __reflect_slice3") }
func __reflect_grow(v Value, n int)                      { panic("reflect: intrinsic __reflect_grow") }
func __reflect_setlen(v Value, n int)                    { panic("reflect: intrinsic __reflect_setlen") }
func __reflect_setcap(v Value, n int)                    { panic("reflect: intrinsic __reflect_setcap") }
func __reflect_clear(v Value)                            { panic("reflect: intrinsic __reflect_clear") }
func __reflect_call(fn Value, args []Value) []Value      { panic("reflect: intrinsic __reflect_call") }
func __reflect_callslice(fn Value, args []Value) []Value { panic("reflect: intrinsic __reflect_callslice") }
func __reflect_methodval(v Value, i int) Value           { panic("reflect: intrinsic __reflect_methodval") }
func __reflect_makefunc(tok unsafe.Pointer, fn Value) Value { panic("reflect: intrinsic __reflect_makefunc") }
func __reflect_append(v Value, xs []Value) Value         { panic("reflect: intrinsic __reflect_append") }
func __reflect_makeslice(tok unsafe.Pointer, len, cap int) Value { panic("reflect: intrinsic __reflect_makeslice") }
func __reflect_makemap(tok unsafe.Pointer) Value         { panic("reflect: intrinsic __reflect_makemap") }
func __reflect_makechan(tok unsafe.Pointer, buffer int) Value { panic("reflect: intrinsic __reflect_makechan") }
func __reflect_sliceat(tok, p unsafe.Pointer, n int) Value { panic("reflect: intrinsic __reflect_sliceat") }
func __reflect_send(v Value, x Value)                 { panic("reflect: intrinsic __reflect_send") }
func __reflect_recv(v Value) reflectValueBoolPair    { panic("reflect: intrinsic __reflect_recv") }
func __reflect_trysend(v Value, x Value) bool        { panic("reflect: intrinsic __reflect_trysend") }
func __reflect_close(v Value)                        { panic("reflect: intrinsic __reflect_close") }

// Map iterator operations.
func __reflect_mapiterinit(v Value) unsafe.Pointer        { panic("reflect: intrinsic __reflect_mapiterinit") }
func __reflect_mapiternext(h unsafe.Pointer) bool        { panic("reflect: intrinsic __reflect_mapiternext") }
func __reflect_mapiterkey(h unsafe.Pointer) Value        { panic("reflect: intrinsic __reflect_mapiterkey") }
func __reflect_mapiterval(h unsafe.Pointer) Value        { panic("reflect: intrinsic __reflect_mapiterval") }
func __reflect_mapiterreset(h unsafe.Pointer, v Value)   { panic("reflect: intrinsic __reflect_mapiterreset") }

// Struct tag parsing is pure Go (__reflect_taglookup below), not an
// intrinsic.
func __reflect_swap(v Value, i, j int)              { panic("reflect: intrinsic __reflect_swap") }

// Select: the multi-result Select keeps its exact gc signature with a
// fail-fast body; the Appendix-M desugar rewrites 3-target call sites to
// the single-result __reflectSelect helper, which calls the intrinsic.
type reflectSelectTriple struct {
	chosen int
	recv   Value
	recvOK bool
}

func __reflect_selectimpl(cases []SelectCase) reflectSelectTriple {
	panic("reflect: intrinsic __reflect_selectimpl")
}

func __reflectSelect(cases []SelectCase) reflectSelectTriple {
	return __reflect_selectimpl(cases)
}

// Desugar helpers for multi-result members: real Go over the
// single-result intrinsics (Appendix M). The multi-result declarations
// keep exact gc signatures with fail-fast bodies.
type reflectMethodPair struct {
	m  Method
	ok bool
}

func __reflectTMethodByName(t Type, name string) reflectMethodPair {
	tok := t.(rtype).tok
	n := __reflect_tNumMethod(tok)
	for i := 0; i < n; i++ {
		m := __reflect_tMethod(tok, i)
		if m.Name == name {
			return reflectMethodPair{m, true}
		}
	}
	return reflectMethodPair{Method{}, false}
}

type reflectStructFieldPair struct {
	f  StructField
	ok bool
}

func __reflectTFieldByName(t Type, name string) reflectStructFieldPair {
	tok := t.(rtype).tok
	n := __reflect_tNumField(tok)
	for i := 0; i < n; i++ {
		f := __reflect_tField(tok, i)
		if f.Name == name {
			return reflectStructFieldPair{f, true}
		}
	}
	return reflectStructFieldPair{StructField{}, false}
}

func __reflectTFieldByNameFunc(t Type, match func(string) bool) reflectStructFieldPair {
	tok := t.(rtype).tok
	n := __reflect_tNumField(tok)
	for i := 0; i < n; i++ {
		f := __reflect_tField(tok, i)
		if match(f.Name) {
			return reflectStructFieldPair{f, true}
		}
	}
	return reflectStructFieldPair{StructField{}, false}
}

type reflectTagPair struct {
	value string
	ok    bool
}

type reflectValuePair struct {
	v   Value
	err error
}

func __reflectFieldByIndexErr(v Value, index []int) reflectValuePair {
	if len(index) == 1 {
		return reflectValuePair{v.Field(index[0]), nil}
	}
	if v.Kind() != Struct {
		panic(&ValueError{"reflect.Value.FieldByIndexErr", v.Kind()})
	}
	for i, x := range index {
		if i > 0 {
			if v.Kind() == Pointer && v.Type().Elem().Kind() == Struct {
				if v.IsNil() {
					return reflectValuePair{Value{}, errors.New("reflect: indirection through nil pointer to embedded struct field " + v.Type().Elem().String())}
				}
				v = v.Elem()
			}
		}
		v = v.Field(x)
	}
	return reflectValuePair{v, nil}
}

type reflectValueBoolPair struct {
	x  Value
	ok bool
}

func __reflectTryRecv(v Value) reflectValueBoolPair {
	ch := v
	ch.__mustBeExported("reflect.Value.TryRecv")
	if ch.Kind() != Chan {
		panic(&ValueError{"reflect.Value.TryRecv", ch.Kind()})
	}
	// Non-blocking receive via a select with a default case.
	// (Intra-package call: the Appendix-M desugar only rewrites
	// package-qualified call sites, so call the __-helper directly.)
	p := __reflectSelect([]SelectCase{
		{Dir: SelectRecv, Chan: ch},
		{Dir: SelectDefault},
	})
	chosen, recv, recvOK := p.chosen, p.recv, p.recvOK
	if chosen == 1 {
		return reflectValueBoolPair{Value{}, false}
	}
	return reflectValueBoolPair{recv, recvOK}
}

// ------------------------------------------------------------------
// rtype: the Type interface implementation.
// ------------------------------------------------------------------

// __mustBeValid panics like gc when the Type is nil.
func (t rtype) __mustBeValid(method string) {
	if t.tok == nil {
		panic("reflect: " + method + " of nil Type")
	}
}

func (t rtype) Kind() Kind {
	if t.tok == nil {
		panic("reflect: Kind of nil Type")
	}
	return __reflect_tKind(t.tok)
}

func (t rtype) Name() string {
	if t.tok == nil {
		return ""
	}
	return __reflect_tName(t.tok)
}

func (t rtype) PkgPath() string {
	if t.tok == nil {
		return ""
	}
	return __reflect_tPkgPath(t.tok)
}

func (t rtype) String() string {
	if t.tok == nil {
		return "<nil>"
	}
	return __reflect_tString(t.tok)
}

func (t rtype) Size() uintptr {
	t.__mustBeValid("Size")
	return __reflect_tSize(t.tok)
}

func (t rtype) Align() int {
	t.__mustBeValid("Align")
	return __reflect_tAlign(t.tok)
}

func (t rtype) FieldAlign() int {
	t.__mustBeValid("FieldAlign")
	return __reflect_tFieldAlign(t.tok)
}

func (t rtype) Bits() int {
	k := t.Kind()
	switch k {
	case Int, Int8, Int16, Int32, Int64, Uint, Uint8, Uint16, Uint32, Uint64, Uintptr, Float32, Float64, Complex64, Complex128:
		return __reflect_tBits(t.tok)
	}
	panic("reflect: Bits of non-arithmetic type " + t.String())
}

func (t rtype) ChanDir() ChanDir {
	if t.Kind() != Chan {
		panic("reflect: ChanDir of non-chan type " + t.String())
	}
	return __reflect_tChanDir(t.tok)
}

func (t rtype) Len() int {
	if t.Kind() != Array {
		panic("reflect: Len of non-array type " + t.String())
	}
	return __reflect_tLen(t.tok)
}

func (t rtype) NumField() int {
	if t.Kind() != Struct {
		panic("reflect: NumField of non-struct type " + t.String())
	}
	return __reflect_tNumField(t.tok)
}

func (t rtype) NumMethod() int {
	t.__mustBeValid("NumMethod")
	return __reflect_tNumMethod(t.tok)
}

func (t rtype) NumIn() int {
	if t.Kind() != Func {
		panic("reflect: NumIn of non-func type " + t.String())
	}
	return __reflect_tNumIn(t.tok)
}

func (t rtype) NumOut() int {
	if t.Kind() != Func {
		panic("reflect: NumOut of non-func type " + t.String())
	}
	return __reflect_tNumOut(t.tok)
}

func (t rtype) IsVariadic() bool {
	if t.Kind() != Func {
		panic("reflect: IsVariadic of non-func type " + t.String())
	}
	return __reflect_tIsVariadic(t.tok)
}

func (t rtype) Comparable() bool {
	t.__mustBeValid("Comparable")
	return __reflect_tComparable(t.tok)
}

func (t rtype) Field(i int) StructField {
	if t.Kind() != Struct {
		panic("reflect: Field of non-struct type " + t.String())
	}
	if i < 0 || i >= t.NumField() {
		panic("reflect: Field index out of range")
	}
	return __reflect_tField(t.tok, i)
}

func (t rtype) FieldByIndex(index []int) StructField {
	// gc evaluates FieldByIndex via Field on each level; an empty index
	// panics with index out of range.
	tt := t
	var f StructField
	for _, i := range index {
		f = tt.Field(i)
		tt = f.Type.(rtype)
	}
	return f
}

func (t rtype) FieldByName(name string) (StructField, bool) {
	panic("reflect: reflect.Type.FieldByName: not in M8 scope (unreachable: desugared)")
}

func (t rtype) FieldByNameFunc(match func(string) bool) (StructField, bool) {
	panic("reflect: reflect.Type.FieldByNameFunc: not in M8 scope (unreachable: desugared)")
}

func (t rtype) Method(i int) Method {
	t.__mustBeValid("Method")
	if i < 0 || i >= t.NumMethod() {
		panic("reflect: Method index out of range")
	}
	return __reflect_tMethod(t.tok, i)
}

func (t rtype) MethodByName(name string) (Method, bool) {
	panic("reflect: reflect.Type.MethodByName: not in M8 scope (unreachable: desugared)")
}

func (t rtype) Elem() Type {
	k := t.Kind()
	switch k {
	case Array, Chan, Map, Pointer, Slice:
		return rtype{tok: __reflect_tElem(t.tok)}
	}
	panic("reflect: Elem of invalid type " + t.String())
}

func (t rtype) Key() Type {
	if t.Kind() != Map {
		panic("reflect: Key of non-map type " + t.String())
	}
	return rtype{tok: __reflect_tKey(t.tok)}
}

func (t rtype) In(i int) Type {
	if t.Kind() != Func {
		panic("reflect: In of non-func type " + t.String())
	}
	if i < 0 || i >= t.NumIn() {
		panic("reflect: In index out of range")
	}
	return rtype{tok: __reflect_tIn(t.tok, i)}
}

func (t rtype) Out(i int) Type {
	if t.Kind() != Func {
		panic("reflect: Out of non-func type " + t.String())
	}
	if i < 0 || i >= t.NumOut() {
		panic("reflect: Out index out of range")
	}
	return rtype{tok: __reflect_tOut(t.tok, i)}
}

func (t rtype) Implements(u Type) bool {
	if u == nil {
		panic("reflect: nil type passed to Type.Implements")
	}
	if u.Kind() != Interface {
		panic("reflect: non-interface type passed to Type.Implements")
	}
	t.__mustBeValid("Implements")
	return __reflect_tImplements(t.tok, u.(rtype).tok)
}

func (t rtype) AssignableTo(u Type) bool {
	if u == nil {
		panic("reflect: nil type passed to Type.AssignableTo")
	}
	t.__mustBeValid("AssignableTo")
	return __reflect_tAssignableTo(t.tok, u.(rtype).tok)
}

func (t rtype) ConvertibleTo(u Type) bool {
	if u == nil {
		panic("reflect: nil type passed to Type.ConvertibleTo")
	}
	t.__mustBeValid("ConvertibleTo")
	return __reflect_tConvertibleTo(t.tok, u.(rtype).tok)
}

func (t rtype) OverflowInt(x int64) bool {
	k := t.Kind()
	switch k {
	case Int, Int8, Int16, Int32, Int64:
		bits := __reflect_tBits(t.tok)
		if bits == 64 {
			return false
		}
		min := -(int64(1) << (uint(bits) - 1))
		max := (int64(1) << (uint(bits) - 1)) - 1
		return x < min || x > max
	}
	panic("reflect: OverflowInt of non-int type " + t.String())
}

func (t rtype) OverflowUint(x uint64) bool {
	k := t.Kind()
	switch k {
	case Uint, Uint8, Uint16, Uint32, Uint64, Uintptr:
		bits := __reflect_tBits(t.tok)
		if bits == 64 {
			return false
		}
		max := (uint64(1) << uint(bits)) - 1
		return x > max
	}
	panic("reflect: OverflowUint of non-uint type " + t.String())
}

func (t rtype) OverflowFloat(x float64) bool {
	k := t.Kind()
	switch k {
	case Float32, Float64:
		if k == Float64 {
			return false
		}
		// float32 overflow: |x| > MaxFloat32 (inf never overflows a float).
		if x != x {
			return false
		}
		const maxFloat32 = 3.40282346638528859811704183484516925440e+38
		return x > maxFloat32 || x < -maxFloat32
	}
	panic("reflect: OverflowFloat of non-float type " + t.String())
}

func (t rtype) OverflowComplex(x complex128) bool {
	k := t.Kind()
	switch k {
	case Complex64, Complex128:
		if k == Complex128 {
			return false
		}
		return t.OverflowFloat(real(x)) || t.OverflowFloat(imag(x))
	}
	panic("reflect: OverflowComplex of non-complex type " + t.String())
}

// Iter-family fail-fasts (re-test: iter package bring-up).
func (t rtype) Methods() iter.Seq[Method] {
	panic("reflect: Type.Methods: not in M8 scope (needs iter; re-test when the iter package lands)")
}

func (t rtype) Fields() iter.Seq[StructField] {
	panic("reflect: Type.Fields: not in M8 scope (needs iter; re-test when the iter package lands)")
}

func (t rtype) Ins() iter.Seq[Type] {
	panic("reflect: Type.Ins: not in M8 scope (needs iter; re-test when the iter package lands)")
}

func (t rtype) Outs() iter.Seq[Type] {
	panic("reflect: Type.Outs: not in M8 scope (needs iter; re-test when the iter package lands)")
}

func (t rtype) CanSeq() bool {
	panic("reflect: Type.CanSeq: not in M8 scope (needs iter; re-test when the iter package lands)")
}

func (t rtype) CanSeq2() bool {
	panic("reflect: Type.CanSeq2: not in M8 scope (needs iter; re-test when the iter package lands)")
}

// ------------------------------------------------------------------
// Value: internal helpers.
// ------------------------------------------------------------------

// __mustBeExported panics like gc's flag.mustBeExported, with the caller
// method name passed explicitly (no runtime.Callers in synthetic scope).
func (v Value) __mustBeExported(method string) {
	if v.typ == nil {
		panic(&ValueError{method, Invalid})
	}
	if v.flag&flagRO != 0 {
		panic("reflect: " + method + " using value obtained using unexported field")
	}
}

// __mustBeAssignable panics like gc's flag.mustBeAssignable.
func (v Value) __mustBeAssignable(method string) {
	if v.typ == nil {
		panic(&ValueError{method, Invalid})
	}
	if v.flag&flagRO != 0 {
		panic("reflect: " + method + " using value obtained using unexported field")
	}
	if v.flag&flagAddr == 0 {
		panic("reflect: " + method + " using unaddressable value")
	}
}

// __mustBeKind takes a slice, not variadic args: the frontend cannot
// pack arguments for variadic calls (WS D), so intra-package callers
// pass an explicit slice literal.
func (v Value) __mustBeKind(method string, kinds []Kind) {
	k := v.Kind()
	for _, want := range kinds {
		if k == want {
			return
		}
	}
	panic(&ValueError{method, k})
}

// ------------------------------------------------------------------
// Value: construction and basic queries.
// ------------------------------------------------------------------

// TypeOf returns the reflection Type of the value in the interface i.
func TypeOf(i any) Type {
	if i == nil {
		return nil
	}
	return rtype{tok: __reflect_typeof(i)}
}

// TypeFor returns the reflection Type of T.
// M8 scope: fail-fast (needs generics instantiation in synthetic scope).
func TypeFor[T any]() Type {
	panic("reflect: TypeFor: not in M8 scope (needs generics in synthetic scope; re-test when they land)")
}

// ValueOf returns a new Value initialized to the concrete value stored
// in the interface i. ValueOf(nil) returns the zero Value.
func ValueOf(i any) Value {
	return __reflect_valueof(i)
}

// New returns a Value representing a pointer to a new zero value for
// the specified type. That is, the returned Value's Type is PointerTo(typ).
func New(typ Type) Value {
	if typ == nil {
		panic("reflect: New(nil)")
	}
	return __reflect_new(typ.(rtype).tok)
}

// NewAt returns a Value representing a pointer to a value of the
// specified type, using p as that pointer.
func NewAt(typ Type, p unsafe.Pointer) Value {
	if typ == nil {
		panic("reflect: New(nil)")
	}
	return __reflect_newat(typ.(rtype).tok, p)
}

// Zero returns a Value representing the zero value for the specified type.
func Zero(typ Type) Value {
	if typ == nil {
		panic("reflect: Zero(nil)")
	}
	return New(typ).Elem()
}

// Indirect returns the value that v points to.
func Indirect(v Value) Value {
	if v.Kind() != Pointer {
		return v
	}
	return v.Elem()
}

// TypeAssert returns the concrete value in v as type T.
// M8 scope: fail-fast (needs generics in synthetic scope).
func TypeAssert[T any](v Value) (T, bool) {
	panic("reflect: TypeAssert: not in M8 scope (needs generics in synthetic scope; re-test when they land)")
}

func (v Value) Kind() Kind {
	if v.typ == nil {
		return Invalid
	}
	return __reflect_tKind(v.typ)
}

func (v Value) Type() Type {
	if v.typ == nil {
		return nil
	}
	return rtype{tok: v.typ}
}

func (v Value) IsValid() bool {
	return v.typ != nil
}

func (v Value) CanAddr() bool {
	return v.flag&flagAddr != 0
}

func (v Value) CanSet() bool {
	return v.flag&flagAddr != 0 && v.flag&flagRO == 0
}

func (v Value) CanInterface() bool {
	if v.typ == nil {
		return false
	}
	return v.flag&flagRO == 0
}

// Interface returns v's current value as any. It panics if v was
// obtained by accessing unexported struct fields.
func (v Value) Interface() (i any) {
	if v.typ == nil {
		panic(&ValueError{"reflect.Value.Interface", Invalid})
	}
	v.__mustBeExported("reflect.Value.Interface")
	return __reflect_interface(v)
}

// Addr returns a pointer value representing the address of v.
// It panics if CanAddr() returns false.
func (v Value) Addr() Value {
	if v.typ == nil {
		panic(&ValueError{"reflect.Value.Addr", Invalid})
	}
	if v.flag&flagAddr == 0 {
		panic("reflect.Value.Addr of unaddressable value")
	}
	return __reflect_newat(__reflect_tPtrTo(v.typ), v.ptr)
}

// ------------------------------------------------------------------
// Value: navigation.
// ------------------------------------------------------------------

// Elem returns the value that the interface v contains or that the
// pointer v points to. It returns the zero Value if v is nil.
func (v Value) Elem() Value {
	k := v.Kind()
	switch k {
	case Interface, Pointer:
		return __reflect_elem(v)
	}
	panic(&ValueError{"reflect.Value.Elem", k})
}

// Field returns the i'th field of the struct v.
func (v Value) Field(i int) Value {
	if v.typ == nil {
		panic(&ValueError{"reflect.Value.Field", Invalid})
	}
	if v.Kind() != Struct {
		panic(&ValueError{"reflect.Value.Field", v.Kind()})
	}
	if i < 0 || i >= v.NumField() {
		panic("reflect: Field index out of range")
	}
	return __reflect_field(v, i)
}

// FieldByIndex returns the nested field corresponding to index.
func (v Value) FieldByIndex(index []int) Value {
	if len(index) == 1 {
		return v.Field(index[0])
	}
	v.__mustBeKind("reflect.Value.FieldByIndex", []Kind{Struct})
	f := v
	for _, i := range index {
		if f.Kind() == Pointer {
			f = f.Elem()
		}
		f = f.Field(i)
	}
	return f
}

// FieldByIndexErr returns the nested field corresponding to index,
// returning an error instead of panicking on nil-pointer indirection.
func (v Value) FieldByIndexErr(index []int) (Value, error) {
	panic("reflect: reflect.Value.FieldByIndexErr: not in M8 scope (unreachable: desugared)")
}

// FieldByName returns the struct field with the given name.
func (v Value) FieldByName(name string) Value {
	v.__mustBeKind("reflect.Value.FieldByName", []Kind{Struct})
	for i := 0; i < v.NumField(); i++ {
		if f := v.Type().Field(i); f.Name == name {
			return v.Field(i)
		}
	}
	return Value{}
}

// FieldByNameFunc returns the first struct field whose name satisfies match.
func (v Value) FieldByNameFunc(match func(string) bool) Value {
	v.__mustBeKind("reflect.Value.FieldByNameFunc", []Kind{Struct})
	for i := 0; i < v.NumField(); i++ {
		if f := v.Type().Field(i); match(f.Name) {
			return v.Field(i)
		}
	}
	return Value{}
}

// Index returns v's i'th element (array, slice, string).
func (v Value) Index(i int) Value {
	k := v.Kind()
	switch k {
	case Array, Slice, String:
		n := v.Len()
		if i < 0 || i >= n {
			switch k {
			case Array:
				panic("reflect: array index out of range")
			case Slice:
				panic("reflect: slice index out of range")
			default:
				panic("reflect: string index out of range")
			}
		}
		return __reflect_index(v, i)
	}
	panic(&ValueError{"reflect.Value.Index", k})
}

// NumField returns the number of fields in the struct v.
func (v Value) NumField() int {
	v.__mustBeKind("reflect.Value.NumField", []Kind{Struct})
	return v.Type().NumField()
}

// Len returns v's length (array, chan, map, slice, string, or pointer
// to array).
func (v Value) Len() int {
	k := v.Kind()
	switch k {
	case Array, Chan, Map, Slice, String, Pointer:
		return __reflect_len(v)
	}
	panic(&ValueError{"reflect.Value.Len", k})
}

// Cap returns v's capacity (array, slice, chan, or pointer to array).
func (v Value) Cap() int {
	k := v.Kind()
	switch k {
	case Array, Slice, Chan, Pointer:
		return __reflect_cap(v)
	}
	panic(&ValueError{"reflect.Value.Cap", k})
}

// IsNil reports whether v is nil (chan, func, interface, map, pointer,
// slice, unsafe pointer).
func (v Value) IsNil() bool {
	k := v.Kind()
	switch k {
	case Chan, Func, Interface, Map, Pointer, Slice, UnsafePointer:
		return __reflect_isnil(v)
	}
	panic(&ValueError{"reflect.Value.IsNil", k})
}

// IsZero reports whether v is the zero value for its type.
func (v Value) IsZero() bool {
	if v.typ == nil {
		panic(&ValueError{"reflect.Value.IsZero", Invalid})
	}
	return __reflect_iszero(v)
}

// ------------------------------------------------------------------
// Value: scalar getters.
// ------------------------------------------------------------------

func (v Value) CanInt() bool {
	switch v.Kind() {
	case Int, Int8, Int16, Int32, Int64:
		return true
	}
	return false
}

func (v Value) Int() int64 {
	k := v.Kind()
	switch k {
	case Int, Int8, Int16, Int32, Int64:
		return __reflect_getint(v)
	}
	panic(&ValueError{"reflect.Value.Int", k})
}

func (v Value) CanUint() bool {
	switch v.Kind() {
	case Uint, Uint8, Uint16, Uint32, Uint64, Uintptr:
		return true
	}
	return false
}

func (v Value) Uint() uint64 {
	k := v.Kind()
	switch k {
	case Uint, Uint8, Uint16, Uint32, Uint64, Uintptr:
		return __reflect_getuint(v)
	}
	panic(&ValueError{"reflect.Value.Uint", k})
}

func (v Value) CanFloat() bool {
	switch v.Kind() {
	case Float32, Float64:
		return true
	}
	return false
}

func (v Value) Float() float64 {
	k := v.Kind()
	switch k {
	case Float32, Float64:
		return __reflect_getfloat(v)
	}
	panic(&ValueError{"reflect.Value.Float", k})
}

func (v Value) CanComplex() bool {
	switch v.Kind() {
	case Complex64, Complex128:
		return true
	}
	return false
}

func (v Value) Complex() complex128 {
	k := v.Kind()
	switch k {
	case Complex64, Complex128:
		return __reflect_getcomplex(v)
	}
	panic(&ValueError{"reflect.Value.Complex", k})
}

func (v Value) Bool() bool {
	k := v.Kind()
	if k != Bool {
		panic(&ValueError{"reflect.Value.Bool", k})
	}
	return __reflect_getbool(v)
}

func (v Value) String() string {
	k := v.Kind()
	if k != String {
		// gc: String() on a String-kind value returns the string; on
		// other kinds it formats like "<T Value>". Mirror it.
		return "<" + v.Type().String() + " Value>"
	}
	return __reflect_getstring(v)
}

// Bytes returns v's underlying value (a []byte slice).
func (v Value) Bytes() []byte {
	k := v.Kind()
	if k == Slice && v.Type().Elem().Kind() == Uint8 {
		return __reflect_getbytes(v)
	}
	if k == Array && v.Type().Elem().Kind() == Uint8 {
		if v.flag&flagAddr == 0 {
			panic("reflect.Value.Bytes of unaddressable byte array")
		}
		return __reflect_getbytes(v)
	}
	if k == Slice {
		panic("reflect.Value.Bytes of non-byte slice")
	}
	if k == Array {
		panic("reflect.Value.Bytes of non-byte array")
	}
	panic(&ValueError{"reflect.Value.Bytes", k})
}

func (v Value) OverflowInt(x int64) bool {
	if v.typ == nil {
		panic(&ValueError{"reflect.Value.OverflowInt", Invalid})
	}
	return v.Type().OverflowInt(x)
}

func (v Value) OverflowUint(x uint64) bool {
	if v.typ == nil {
		panic(&ValueError{"reflect.Value.OverflowUint", Invalid})
	}
	return v.Type().OverflowUint(x)
}

func (v Value) OverflowFloat(x float64) bool {
	if v.typ == nil {
		panic(&ValueError{"reflect.Value.OverflowFloat", Invalid})
	}
	return v.Type().OverflowFloat(x)
}

func (v Value) OverflowComplex(x complex128) bool {
	if v.typ == nil {
		panic(&ValueError{"reflect.Value.OverflowComplex", Invalid})
	}
	return v.Type().OverflowComplex(x)
}

// ------------------------------------------------------------------
// Value: setters.
// ------------------------------------------------------------------

// Set assigns x to the value v.
func (v Value) Set(x Value) {
	v.__mustBeAssignable("reflect.Value.Set")
	x.__mustBeExported("reflect.Value.Set")
	if !x.Type().AssignableTo(v.Type()) {
		panic("reflect.Value.Set: value of type " + x.Type().String() + " is not assignable to type " + v.Type().String())
	}
	__reflect_set(v, x)
}

// SetZero sets v to the zero value for its type.
func (v Value) SetZero() {
	v.__mustBeAssignable("reflect.Value.SetZero")
	__reflect_setzero(v)
}

func (v Value) SetInt(x int64) {
	v.__mustBeAssignable("reflect.Value.SetInt")
	switch v.Kind() {
	case Int, Int8, Int16, Int32, Int64:
		__reflect_setint(v, x)
		return
	}
	panic(&ValueError{"reflect.Value.SetInt", v.Kind()})
}

func (v Value) SetUint(x uint64) {
	v.__mustBeAssignable("reflect.Value.SetUint")
	switch v.Kind() {
	case Uint, Uint8, Uint16, Uint32, Uint64, Uintptr:
		__reflect_setuint(v, x)
		return
	}
	panic(&ValueError{"reflect.Value.SetUint", v.Kind()})
}

func (v Value) SetFloat(x float64) {
	v.__mustBeAssignable("reflect.Value.SetFloat")
	switch v.Kind() {
	case Float32, Float64:
		__reflect_setfloat(v, x)
		return
	}
	panic(&ValueError{"reflect.Value.SetFloat", v.Kind()})
}

func (v Value) SetComplex(x complex128) {
	v.__mustBeAssignable("reflect.Value.SetComplex")
	switch v.Kind() {
	case Complex64, Complex128:
		__reflect_setcomplex(v, x)
		return
	}
	panic(&ValueError{"reflect.Value.SetComplex", v.Kind()})
}

func (v Value) SetBool(x bool) {
	v.__mustBeAssignable("reflect.Value.SetBool")
	if v.Kind() != Bool {
		panic(&ValueError{"reflect.Value.SetBool", v.Kind()})
	}
	__reflect_setbool(v, x)
}

func (v Value) SetString(x string) {
	v.__mustBeAssignable("reflect.Value.SetString")
	if v.Kind() != String {
		panic(&ValueError{"reflect.Value.SetString", v.Kind()})
	}
	__reflect_setstring(v, x)
}

// SetBytes sets v's underlying value to x (a []byte).
func (v Value) SetBytes(x []byte) {
	v.__mustBeAssignable("reflect.Value.SetBytes")
	if v.Kind() != Slice || v.Type().Elem().Kind() != Uint8 {
		panic("reflect.Value.SetBytes of non-byte slice")
	}
	__reflect_setbytes(v, x)
}

// SetPointer sets the unsafe.Pointer value v to x.
func (v Value) SetPointer(x unsafe.Pointer) {
	v.__mustBeAssignable("reflect.Value.SetPointer")
	if v.Kind() != UnsafePointer {
		panic(&ValueError{"reflect.Value.SetPointer", v.Kind()})
	}
	__reflect_set(v, ValueOf(x))
}

// SetMapIndex sets the element of v associated with key to elem.
func (v Value) SetMapIndex(key, elem Value) {
	v.__mustBeAssignable("reflect.Value.SetMapIndex")
	if v.Kind() != Map {
		panic(&ValueError{"reflect.Value.SetMapIndex", v.Kind()})
	}
	__reflect_setmapindex(v, key, elem)
}

// ------------------------------------------------------------------
// Value: Call, methods, MakeFunc.
// ------------------------------------------------------------------

// Call calls the function v with the input arguments in.
func (v Value) Call(in []Value) []Value {
	v.__mustBeExported("reflect.Value.Call")
	if v.Kind() != Func {
		panic(&ValueError{"reflect.Value.Call", v.Kind()})
	}
	return __reflect_call(v, in)
}

// CallSlice calls the variadic function v with the input arguments in,
// assigning the slice in[len(in)-1] to v's final variadic argument.
func (v Value) CallSlice(in []Value) []Value {
	v.__mustBeExported("reflect.Value.CallSlice")
	if v.Kind() != Func {
		panic(&ValueError{"reflect.Value.CallSlice", v.Kind()})
	}
	return __reflect_callslice(v, in)
}

// NumMethod returns the number of methods in the value's method set.
func (v Value) NumMethod() int {
	if v.typ == nil {
		panic(&ValueError{"reflect.Value.NumMethod", Invalid})
	}
	return v.Type().NumMethod()
}

// Method returns a function value corresponding to v's i'th method.
func (v Value) Method(i int) Value {
	if v.typ == nil {
		panic(&ValueError{"reflect.Value.Method", Invalid})
	}
	if i < 0 || i >= v.NumMethod() {
		panic("reflect: Method index out of range")
	}
	if v.Kind() == Interface && v.IsNil() {
		panic("reflect: Method on nil interface value")
	}
	return __reflect_methodval(v, i)
}

// MethodByName returns a function value corresponding to v's method
// with the given name.
func (v Value) MethodByName(name string) Value {
	if v.typ == nil {
		panic(&ValueError{"reflect.Value.MethodByName", Invalid})
	}
	m, ok := v.Type().MethodByName(name)
	if !ok {
		return Value{}
	}
	return v.Method(m.Index)
}

// MakeFunc creates a new function with the type typ that, when called,
// calls fn. The fn is called with a slice of reflect Values for args.
func MakeFunc(typ Type, fn func(args []Value) (results []Value)) Value {
	if typ == nil {
		panic("reflect: MakeFunc(nil)")
	}
	if typ.Kind() != Func {
		panic("reflect: MakeFunc of non-func type " + typ.String())
	}
	return __reflect_makefunc(typ.(rtype).tok, ValueOf(fn))
}

// ------------------------------------------------------------------
// Value: channels.
// ------------------------------------------------------------------

// MakeChan creates a new channel with the specified type and buffer size.
func MakeChan(typ Type, buffer int) Value {
	if typ == nil {
		panic("reflect: MakeChan(nil)")
	}
	if typ.Kind() != Chan {
		panic("reflect: MakeChan of non-chan type " + typ.String())
	}
	if buffer < 0 {
		panic("reflect.MakeChan: negative buffer size")
	}
	return __reflect_makechan(typ.(rtype).tok, buffer)
}

// Send sends x on the channel v.
func (v Value) Send(x Value) {
	v.__mustBeExported("reflect.Value.Send")
	if v.Kind() != Chan {
		panic(&ValueError{"reflect.Value.Send", v.Kind()})
	}
	if v.Type().ChanDir() == RecvDir {
		panic("reflect: send on recv-only channel")
	}
	__reflect_send(v, x)
}

// Recv receives a value from the channel v.
func (v Value) Recv() (x Value, ok bool) {
	v.__mustBeExported("reflect.Value.Recv")
	if v.Kind() != Chan {
		panic(&ValueError{"reflect.Value.Recv", v.Kind()})
	}
	if v.Type().ChanDir() == SendDir {
		panic("reflect: recv on send-only channel")
	}
	p := __reflect_recv(v)
	return p.x, p.ok
}

// TrySend attempts to send x on the channel v but will not block.
func (v Value) TrySend(x Value) bool {
	v.__mustBeExported("reflect.Value.TrySend")
	if v.Kind() != Chan {
		panic(&ValueError{"reflect.Value.TrySend", v.Kind()})
	}
	if v.Type().ChanDir() == RecvDir {
		panic("reflect: send on recv-only channel")
	}
	return __reflect_trysend(v, x)
}

// TryRecv attempts to receive a value from the channel v but will not block.
func (v Value) TryRecv() (x Value, ok bool) {
	panic("reflect: reflect.Value.TryRecv: not in M8 scope (unreachable: desugared)")
}

// Close closes the channel v.
func (v Value) Close() {
	v.__mustBeExported("reflect.Value.Close")
	if v.Kind() != Chan {
		panic(&ValueError{"reflect.Value.Close", v.Kind()})
	}
	if v.Type().ChanDir() == RecvDir {
		panic("reflect: close of receive-only channel")
	}
	__reflect_close(v)
}

// Select executes a select operation described by the list of cases.
func Select(cases []SelectCase) (chosen int, recv Value, recvOK bool) {
	panic("reflect: reflect.Select: not in M8 scope (unreachable: desugared)")
}

// ------------------------------------------------------------------
// Value: slices, maps, conversion.
// ------------------------------------------------------------------

// MakeSlice creates a new zero-initialized slice value for the
// specified slice type, with the specified length and capacity.
func MakeSlice(typ Type, len, cap int) Value {
	if typ == nil {
		panic("reflect: MakeSlice(nil)")
	}
	if typ.Kind() != Slice {
		panic("reflect: MakeSlice of non-slice type " + typ.String())
	}
	if len < 0 || cap < 0 || len > cap {
		panic("reflect.MakeSlice: len out of range")
	}
	return __reflect_makeslice(typ.(rtype).tok, len, cap)
}

// MakeMap creates a new map with the specified type.
func MakeMap(typ Type) Value {
	return MakeMapWithSize(typ, 0)
}

// MakeMapWithSize creates a new map with the specified type and initial space.
func MakeMapWithSize(typ Type, n int) Value {
	if typ == nil {
		panic("reflect: MakeMapWithSize(nil)")
	}
	if typ.Kind() != Map {
		panic("reflect: MakeMapWithSize of non-map type " + typ.String())
	}
	if n < 0 {
		panic("reflect.MakeMapWithSize: negative size")
	}
	return __reflect_makemap(typ.(rtype).tok)
}

// MapIndex returns the value associated with key in the map v.
func (v Value) MapIndex(key Value) Value {
	if v.Kind() != Map {
		panic(&ValueError{"reflect.Value.MapIndex", v.Kind()})
	}
	return __reflect_mapindex(v, key)
}

// MapKeys returns a slice containing all the keys of the map v.
func (v Value) MapKeys() []Value {
	v.__mustBeExported("reflect.Value.MapKeys")
	if v.Kind() != Map {
		panic(&ValueError{"reflect.Value.MapKeys", v.Kind()})
	}
	it := v.MapRange()
	keys := make([]Value, 0, v.Len())
	for it.Next() {
		keys = append(keys, it.Key())
	}
	return keys
}

// MapRange returns a range iterator for the map v.
func (v Value) MapRange() *MapIter {
	if v.Kind() != Map {
		panic(&ValueError{"reflect.Value.MapRange", v.Kind()})
	}
	return &MapIter{h: __reflect_mapiterinit(v)}
}

// Slice returns a slice of v.
func (v Value) Slice(i, j int) Value {
	v.__mustBeKind("reflect.Value.Slice", []Kind{Array, Slice, String})
	n := v.Len()
	var capv int
	if v.Kind() == Slice || v.Kind() == Array {
		capv = v.Cap()
	} else {
		capv = n
	}
	if i < 0 || j < i || j > capv {
		if v.Kind() == String {
			panic("reflect.Value.Slice: string slice index out of bounds")
		}
		panic("reflect.Value.Slice: slice index out of bounds")
	}
	if v.Kind() == Array && v.flag&flagAddr == 0 {
		panic("reflect.Value.Slice: slice of unaddressable array")
	}
	return __reflect_slice(v, i, j)
}

// Slice3 is the 3-index form of Slice.
func (v Value) Slice3(i, j, k int) Value {
	v.__mustBeKind("reflect.Value.Slice3", []Kind{Array, Slice})
	n := v.Len()
	capv := v.Cap()
	if i < 0 || j < i || k < j || k > capv {
		panic("reflect.Value.Slice3: slice index out of bounds")
	}
	_ = n
	if v.Kind() == Array && v.flag&flagAddr == 0 {
		panic("reflect.Value.Slice3: slice of unaddressable array")
	}
	return __reflect_slice3(v, i, j, k)
}

// SliceAt returns a Value representing a slice with the specified type,
// pointer, and length.
func SliceAt(typ Type, p unsafe.Pointer, n int) Value {
	panic("reflect: SliceAt: not in M8 scope (needs runtime type synthesis for SliceOf(typ); re-test when it lands)")
}

// Grow increases the slice's capacity, if necessary, to guarantee space
// for another n elements.
func (v Value) Grow(n int) {
	v.__mustBeAssignable("reflect.Value.Grow")
	if v.Kind() != Slice {
		panic(&ValueError{"reflect.Value.Grow", v.Kind()})
	}
	__reflect_grow(v, n)
}

// SetLen sets v's length to n.
func (v Value) SetLen(n int) {
	v.__mustBeAssignable("reflect.Value.SetLen")
	if v.Kind() != Slice {
		panic(&ValueError{"reflect.Value.SetLen", v.Kind()})
	}
	__reflect_setlen(v, n)
}

// SetCap sets v's capacity to n.
func (v Value) SetCap(n int) {
	v.__mustBeAssignable("reflect.Value.SetCap")
	if v.Kind() != Slice {
		panic(&ValueError{"reflect.Value.SetCap", v.Kind()})
	}
	__reflect_setcap(v, n)
}

// Clear clears the map v or zeroes the slice v's elements.
func (v Value) Clear() {
	v.__mustBeAssignable("reflect.Value.Clear")
	switch v.Kind() {
	case Map, Slice:
		__reflect_clear(v)
		return
	}
	panic(&ValueError{"reflect.Value.Clear", v.Kind()})
}

// Convert returns the value v converted to type t.
func (v Value) Convert(t Type) Value {
	if t == nil {
		panic("reflect: Convert(nil)")
	}
	v.__mustBeExported("reflect.Value.Convert")
	if !v.Type().ConvertibleTo(t) {
		panic("reflect.Value.Convert: value of type " + v.Type().String() + " cannot be converted to type " + t.String())
	}
	return __reflect_convert(v, t.(rtype).tok)
}

// CanConvert reports whether the value v can be converted to type t.
func (v Value) CanConvert(t Type) bool {
	if t == nil {
		panic("reflect: CanConvert(nil)")
	}
	if v.typ == nil {
		return false
	}
	return v.Type().ConvertibleTo(t)
}

// Comparable reports whether the value v is comparable.
func (v Value) Comparable() bool {
	if v.typ == nil {
		panic(&ValueError{"reflect.Value.Comparable", Invalid})
	}
	return v.Type().Comparable()
}

// Equal reports whether v is equal to u.
// For two invalid values, Equal reports true.
func (v Value) Equal(u Value) bool {
	if v.Kind() == Interface {
		v = v.Elem()
	}
	if u.Kind() == Interface {
		u = u.Elem()
	}
	if !v.IsValid() || !u.IsValid() {
		return v.IsValid() == u.IsValid()
	}
	if v.Kind() != u.Kind() || v.typ != u.typ {
		return false
	}
	switch v.Kind() {
	case Bool:
		return v.Bool() == u.Bool()
	case Int, Int8, Int16, Int32, Int64:
		return v.Int() == u.Int()
	case Uint, Uint8, Uint16, Uint32, Uint64, Uintptr:
		return v.Uint() == u.Uint()
	case Float32, Float64:
		return v.Float() == u.Float()
	case Complex64, Complex128:
		return v.Complex() == u.Complex()
	case String:
		return v.String() == u.String()
	case Chan, Pointer, UnsafePointer:
		return v.UnsafePointer() == u.UnsafePointer()
	case Array:
		if v.Len() == 0 {
			if !v.Type().Elem().Comparable() {
				break
			}
			return true
		}
		for i := 0; i < v.Len(); i++ {
			if !v.Index(i).Equal(u.Index(i)) {
				return false
			}
		}
		return true
	case Struct:
		for i := 0; i < v.NumField(); i++ {
			if !v.Field(i).Equal(u.Field(i)) {
				return false
			}
		}
		return true
	case Func, Map, Slice:
	}
	panic("reflect.Value.Equal: values of type " + v.Type().String() + " are not comparable")
}

// ------------------------------------------------------------------
// Value: fail-fasts.
// ------------------------------------------------------------------

// Pointer returns v's value as a uintptr (address escape — D5 trap).
func (v Value) Pointer() uintptr {
	panic("reflect: reflect.Value.Pointer: not in M8 scope (uintptr escape traps per D5/GG-0015; no re-test planned)")
}

// UnsafeAddr returns a pointer to v's data (address escape — D5 trap).
func (v Value) UnsafeAddr() uintptr {
	panic("reflect: reflect.Value.UnsafeAddr: not in M8 scope (uintptr escape traps per D5/GG-0015; no re-test planned)")
}

// InterfaceData returns the interface's data pointer (D5 trap).
func (v Value) InterfaceData() [2]uintptr {
	panic("reflect: reflect.Value.InterfaceData: not in M8 scope (uintptr escape traps per D5/GG-0015; no re-test planned)")
}

// UnsafePointer returns v's value as an unsafe.Pointer.
func (v Value) UnsafePointer() unsafe.Pointer {
	if v.typ == nil {
		panic(&ValueError{"reflect.Value.UnsafePointer", Invalid})
	}
	return v.ptr
}

// Iter-family fail-fasts (re-test: iter package bring-up).
func (v Value) Seq() iter.Seq[Value] {
	panic("reflect: Value.Seq: not in M8 scope (needs iter; re-test when the iter package lands)")
}

func (v Value) Seq2() iter.Seq2[int, Value] {
	panic("reflect: Value.Seq2: not in M8 scope (needs iter; re-test when the iter package lands)")
}

func (v Value) Methods() iter.Seq2[Method, Value] {
	panic("reflect: Value.Methods: not in M8 scope (needs iter; re-test when the iter package lands)")
}

func (v Value) Fields() iter.Seq2[StructField, Value] {
	panic("reflect: Value.Fields: not in M8 scope (needs iter; re-test when the iter package lands)")
}

// ------------------------------------------------------------------
// Top-level functions.
// ------------------------------------------------------------------

// Append appends the values x to a slice s and returns the resulting slice.
func Append(s Value, x ...Value) Value {
	s.__mustBeKind("reflect.Append", []Kind{Slice})
	n := len(x)
	if n == 0 {
		return s
	}
	et := s.Type().Elem()
	for i, v := range x {
		if !v.IsValid() {
			panic("reflect.Append: value " + reflectItoa(i) + " is zero Value")
		}
		if !v.Type().AssignableTo(et) {
			panic("reflect.Append: value of type " + v.Type().String() + " is not assignable to type " + et.String())
		}
	}
	return __reflect_append(s, x)
}

// AppendSlice appends a slice t to a slice s and returns the resulting slice.
func AppendSlice(s, t Value) Value {
	s.__mustBeKind("reflect.AppendSlice", []Kind{Slice})
	t.__mustBeKind("reflect.AppendSlice", []Kind{Slice})
	if !t.Type().AssignableTo(s.Type()) {
		panic("reflect.AppendSlice: " + t.Type().String() + " is not assignable to " + s.Type().String())
	}
	n := t.Len()
	xs := make([]Value, n)
	for i := 0; i < n; i++ {
		xs[i] = t.Index(i)
	}
	return __reflect_append(s, xs)
}

// Copy copies the contents of src into dst until either dst has been
// filled or src has been exhausted. It returns the number of elements copied.
func Copy(dst, src Value) int {
	dk := dst.Kind()
	if dk != Array && dk != Slice {
		panic(&ValueError{"reflect.Copy", dk})
	}
	if dk == Array {
		dst.__mustBeAssignable("reflect.Copy")
	}
	dst.__mustBeExported("reflect.Copy")
	sk := src.Kind()
	stringCopy := false
	if sk != Array && sk != Slice {
		if sk == String && dst.Type().Elem().Kind() == Uint8 {
			stringCopy = true
		} else {
			panic(&ValueError{"reflect.Copy", sk})
		}
	}
	src.__mustBeExported("reflect.Copy")
	if !stringCopy {
		de := dst.Type().Elem()
		se := src.Type().Elem()
		if de.(rtype).tok != se.(rtype).tok {
			panic("reflect.Copy: " + de.String() + " != " + se.String())
		}
	}
	n := dst.Len()
	if sl := src.Len(); sl < n {
		n = sl
	}
	if stringCopy {
		str := src.String()
		for i := 0; i < n; i++ {
			dst.Index(i).SetUint(uint64(str[i]))
		}
	} else {
		for i := 0; i < n; i++ {
			dst.Index(i).Set(src.Index(i))
		}
	}
	return n
}

// ------------------------------------------------------------------
// DeepEqual: adapted from gc's deepequal.go. Cycle detection uses a
// []visit slice with linear scan (v1 struct map keys are identity, so a
// map[visit]bool would never hit); visit keys are {a1, a2, typ} as
// reflect-produced handles (stable by interning) plus the type token.
// ------------------------------------------------------------------

type reflectVisit struct {
	a1  unsafe.Pointer
	a2  unsafe.Pointer
	typ unsafe.Pointer
}

// DeepEqual reports whether x and y are "deeply equal".
func DeepEqual(x, y any) bool {
	if x == nil || y == nil {
		return x == y
	}
	v1 := ValueOf(x)
	v2 := ValueOf(y)
	if v1.typ != v2.typ {
		return false
	}
	return __reflectDeepEqual(v1, v2, nil)
}

func __reflectDeepEqual(v1, v2 Value, visited []reflectVisit) bool {
	if !v1.IsValid() || !v2.IsValid() {
		return v1.IsValid() == v2.IsValid()
	}
	if v1.typ != v2.typ {
		return false
	}
	// Cycle detection: for any possible reference cycle, hard() is true
	// for at least one type in the cycle. Nil values cannot be cyclic and
	// are excluded (gc-exact). The visit key uses the interned handles,
	// which are stable per object, so no address canonicalization is
	// needed (D5 uintptr ordering traps make gc's unsafe.Pointer ordering
	// inexpressible; correctness is unaffected).
	k := v1.Kind()
	hard := k == Map || k == Slice || k == Pointer || k == Interface
	if hard && !v1.IsNil() && !v2.IsNil() {
		a1 := v1.UnsafePointer()
		a2 := v2.UnsafePointer()
		for _, vv := range visited {
			if vv.a1 == a1 && vv.a2 == a2 && vv.typ == v1.typ {
				return true
			}
		}
		visited = append(visited, reflectVisit{a1: a1, a2: a2, typ: v1.typ})
	}
	switch k {
	case Array:
		for i := 0; i < v1.Len(); i++ {
			if !__reflectDeepEqual(v1.Index(i), v2.Index(i), visited) {
				return false
			}
		}
		return true
	case Slice:
		if v1.IsNil() != v2.IsNil() {
			return false
		}
		if v1.Len() != v2.Len() {
			return false
		}
		if v1.UnsafePointer() == v2.UnsafePointer() {
			return true
		}
		for i := 0; i < v1.Len(); i++ {
			if !__reflectDeepEqual(v1.Index(i), v2.Index(i), visited) {
				return false
			}
		}
		return true
	case Interface:
		if v1.IsNil() || v2.IsNil() {
			return v1.IsNil() == v2.IsNil()
		}
		return __reflectDeepEqual(v1.Elem(), v2.Elem(), visited)
	case Pointer:
		if v1.UnsafePointer() == v2.UnsafePointer() {
			return true
		}
		return __reflectDeepEqual(v1.Elem(), v2.Elem(), visited)
	case Struct:
		for i := 0; i < v1.NumField(); i++ {
			if !__reflectDeepEqual(v1.Field(i), v2.Field(i), visited) {
				return false
			}
		}
		return true
	case Map:
		if v1.IsNil() != v2.IsNil() {
			return false
		}
		if v1.Len() != v2.Len() {
			return false
		}
		if v1.UnsafePointer() == v2.UnsafePointer() {
			return true
		}
		for _, k := range v1.MapKeys() {
			val1 := v1.MapIndex(k)
			val2 := v2.MapIndex(k)
			if !val1.IsValid() || !val2.IsValid() {
				return false
			}
			if !__reflectDeepEqual(val1, val2, visited) {
				return false
			}
		}
		return true
	case Func:
		return v1.IsNil() && v2.IsNil()
	case Bool:
		return v1.Bool() == v2.Bool()
	case Float32, Float64:
		return v1.Float() == v2.Float()
	case Complex64, Complex128:
		return v1.Complex() == v2.Complex()
	default:
		// valueInterface(v, false): raw unbox, bypassing flagRO like gc.
		return __reflect_rawinterface(v1) == __reflect_rawinterface(v2)
	}
}

// ------------------------------------------------------------------
// Swapper: adapted from gc's swapper.go. Raw element access bypasses
// flagRO like gc's unsafe bypass.
// ------------------------------------------------------------------

// Swapper returns a function that swaps the elements in the provided
// slice. The swap bypasses flagRO at the raw-element level, exactly like
// gc's typedmemmove-based implementation.
func Swapper(slice any) func(i, j int) {
	v := ValueOf(slice)
	if v.Kind() != Slice {
		panic(&ValueError{Method: "Swapper", Kind: v.Kind()})
	}
	switch v.Len() {
	case 0:
		return func(i, j int) { panic("reflect: slice index out of range") }
	case 1:
		return func(i, j int) {
			if i != 0 || j != 0 {
				panic("reflect: slice index out of range")
			}
		}
	}
	return func(i, j int) {
		n := v.Len()
		if i < 0 || i >= n || j < 0 || j >= n {
			panic("reflect: slice index out of range")
		}
		__reflect_swap(v, i, j)
	}
}

// VisibleFields returns all the visible fields in a struct type t,
// including fields inside anonymous struct members.
func VisibleFields(t Type) []StructField {
	if t == nil {
		panic("reflect: VisibleFields(nil)")
	}
	if t.Kind() != Struct {
		panic("reflect: VisibleFields of non-struct type")
	}
	rt := t.(rtype)
	// Adaptation of gc's visiblefields.go: breadth-first walk over the
	// embedded field tree, keeping the shallowest path to each name and
	// discarding ambiguous (multiply-defined) names.
	type fieldScan struct {
		typ   rtype
		index []int
	}
	var fields []StructField
	// seen maps field name -> index into fields, plus ambiguity marks.
	type seenEntry struct {
		idx int
		amb bool
	}
	seen := map[string]*seenEntry{}
	// current level and next level.
	current := []fieldScan{{typ: rt}}
	next := []fieldScan{}
	// visited anonymously-embedded struct types (cycle guard).
	visited := map[unsafe.Pointer]bool{rt.tok: true}
	for len(current) > 0 {
		next = next[:0]
		for _, scan := range current {
			nf := scan.typ.NumField()
			for i := 0; i < nf; i++ {
				f := __reflect_tField(scan.typ.tok, i)
				isUnexported := f.PkgPath != ""
				// Build the index path.
				idx := make([]int, len(scan.index)+1)
				copy(idx, scan.index)
				idx[len(scan.index)] = i
				// Embedded structs queue for the next level.
				// P1-2: gc dereferences embedded pointers before the
				// struct-kind check (visiblefields.go:94-101); a struct
				// embedding *T must promote T's fields.
				ft := f.Type
				if f.Anonymous && ft.Kind() == Pointer {
					ft = ft.Elem()
				}
				if f.Anonymous && ft.Kind() == Struct {
					ftok := ft.(rtype).tok
					if !visited[ftok] {
						visited[ftok] = true
						next = append(next, fieldScan{typ: ft.(rtype), index: idx})
					}
				}
				// Record the field at this depth unless shadowed.
				if e, ok := seen[f.Name]; ok {
					if !e.amb {
						// A field at the same or shallower depth already
						// recorded: mark ambiguous if same depth.
						e.amb = true
						fields[e.idx] = StructField{}
					}
					continue
				}
				f.Index = idx
				_ = isUnexported
				seen[f.Name] = &seenEntry{idx: len(fields)}
				fields = append(fields, f)
			}
		}
		current, next = next, current
	}
	// Drop ambiguous (zeroed) entries.
	out := fields[:0]
	for _, f := range fields {
		if f.Name != "" {
			out = append(out, f)
		}
	}
	return out
}

// ------------------------------------------------------------------
// Small helpers.
// ------------------------------------------------------------------

// reflectItoa formats n in decimal (tiny local itoa; avoids depending
// on strconv for the few internal uses).
func reflectItoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// __reflect_taglookup parses a struct tag (gc-exact algorithm,
// adapted from StructTag.Lookup; strconv.Unquote handles escapes).
func __reflect_taglookup(tag StructTag, key string) reflectTagPair {
	s := string(tag)
	for s != "" {
		i := 0
		for i < len(s) && s[i] == ' ' {
			i++
		}
		s = s[i:]
		if s == "" {
			break
		}
		i = 0
		for i < len(s) && s[i] > ' ' && s[i] != ':' && s[i] != '"' && s[i] != 0x7f {
			i++
		}
		if i == 0 || i+1 >= len(s) || s[i] != ':' || s[i+1] != '"' {
			break
		}
		name := string(s[:i])
		s = s[i+1:]
		i = 1
		for i < len(s) && s[i] != '"' {
			if s[i] == '\\' {
				i++
			}
			i++
		}
		if i >= len(s) {
			break
		}
		qvalue := string(s[:i+1])
		s = s[i+1:]
		if key == name {
			value, err := strconv.Unquote(qvalue)
			if err != nil {
				break
			}
			return reflectTagPair{value, true}
		}
	}
	return reflectTagPair{"", false}
}

// ------------------------------------------------------------------
// Type constructors: fail-fast (need runtime type synthesis).
// ------------------------------------------------------------------

// ArrayOf returns the array type containing n elements of type elem.
func ArrayOf(n int, elem Type) Type {
	panic("reflect: ArrayOf: not in M8 scope (needs runtime type synthesis; re-test when it lands)")
}

// SliceOf returns the slice type with element type elem.
func SliceOf(elem Type) Type {
	panic("reflect: SliceOf: not in M8 scope (needs runtime type synthesis; re-test when it lands)")
}

// MapOf returns the map type with key type k and element type elem.
func MapOf(key, elem Type) Type {
	panic("reflect: MapOf: not in M8 scope (needs runtime type synthesis; re-test when it lands)")
}

// ChanOf returns a channel type with the given direction and element type.
func ChanOf(dir ChanDir, elem Type) Type {
	panic("reflect: ChanOf: not in M8 scope (needs runtime type synthesis; re-test when it lands)")
}

// FuncOf returns a function type with the given argument and result types.
func FuncOf(in, out []Type, variadic bool) Type {
	panic("reflect: FuncOf: not in M8 scope (needs runtime type synthesis; re-test when it lands)")
}

// PtrTo returns the pointer type with element t.
func PtrTo(t Type) Type {
	panic("reflect: PtrTo: not in M8 scope (needs runtime type synthesis; re-test when it lands)")
}

// PointerTo is the old name for PtrTo.
func PointerTo(t Type) Type {
	panic("reflect: PointerTo: not in M8 scope (needs runtime type synthesis; re-test when it lands)")
}

// StructOf returns the struct type containing fields.
func StructOf(fields []StructField) Type {
	panic("reflect: StructOf: not in M8 scope (needs runtime type synthesis; re-test when it lands)")
}
`

// P3-M8 (wave 2): synthetic internal/reflectlite package. Pure adapter
// over the synthetic reflect package (TypeOf/ValueOf/Swapper only).
const syntheticReflectliteSource = `package reflectlite

import "reflect"

// (Aliases "type Type = reflect.Type" / "type Value = reflect.Value"
// removed in 3d46d9e. The old comment claimed "exporter does not support
// alias kind" — false: the exporter has emitted "case *types.Alias"
// since the initial commit, and NodeFactory resolves aliases. What was
// actually missing was the *Java* consumer: ReflectTypes.java had no
// alias case in shell/keyOf/buildInfo and threw
// "reflect: unsupported type kind alias" while building the type table
// for any export containing the aliases. 3d46d9e removed the aliases
// as the expedient fix AND added the Java alias support, so the
// removal is now belt-and-braces. Re-test trigger: restore the two
// alias declarations above and run the reflect corpus differentially —
// the Java alias path now exists, so restoration is expected to work;
// re-add only with that differential green.)
// (see above)

func TypeOf(i any) reflect.Type { return reflect.TypeOf(i) }

func ValueOf(i any) reflect.Value { return reflect.ValueOf(i) }

func Swapper(slice any) func(i, j int) { return reflect.Swapper(slice) }
`

// P3-M8 (wave 2): minimal synthetic iter package. Only the two generic
// sequence type declarations, so the exact gc reflect signatures that
// mention iter.Seq/iter.Seq2 type-check. The iter methods are fail-fast
// (re-test: iter package bring-up).
const syntheticIterSource = `package iter

type Seq[V any] func(yield func(V) bool)

type Seq2[K, V any] func(yield func(K, V) bool)
`

// reflectIntrinsicNames is the set of __reflect_* names intercepted at
// call sites into the "reflectop" wire node. Real-Go helpers
// (__reflect_taglookup, __reflectSelect, __reflectTMethodByName,
// __reflectTFieldByName, __reflectTFieldByNameFunc,
// __reflectFieldByIndexErr, __reflectTryRecv, reflectItoa) are NOT here;
// they are emitted as ordinary synthetic functions.
var reflectIntrinsicNames = map[string]bool{
	"__reflect_tKind": true, "__reflect_tString": true, "__reflect_tName": true,
	"__reflect_tPkgPath": true, "__reflect_tSize": true, "__reflect_tAlign": true,
	"__reflect_tFieldAlign": true, "__reflect_tBits": true, "__reflect_tChanDir": true,
	"__reflect_tLen": true, "__reflect_tNumField": true, "__reflect_tNumMethod": true,
	"__reflect_tNumIn": true, "__reflect_tNumOut": true, "__reflect_tIsVariadic": true,
	"__reflect_tComparable": true, "__reflect_tField": true, "__reflect_tMethod": true,
	"__reflect_tElem": true, "__reflect_tKey": true, "__reflect_tIn": true,
	"__reflect_tOut": true, "__reflect_tImplements": true, "__reflect_tAssignableTo": true,
	"__reflect_tConvertibleTo": true, "__reflect_tPtrTo": true,
	"__reflect_typeof": true, "__reflect_valueof": true, "__reflect_new": true,
	"__reflect_newat": true, "__reflect_field": true, "__reflect_index": true,
	"__reflect_elem": true, "__reflect_len": true, "__reflect_cap": true,
	"__reflect_isnil": true, "__reflect_iszero": true,
	"__reflect_getint": true, "__reflect_getuint": true, "__reflect_getfloat": true,
	"__reflect_getcomplex": true, "__reflect_getbool": true, "__reflect_getstring": true,
	"__reflect_getbytes": true,
	"__reflect_setint":   true, "__reflect_setuint": true, "__reflect_setfloat": true,
	"__reflect_setcomplex": true, "__reflect_setbool": true, "__reflect_setstring": true,
	"__reflect_setbytes": true, "__reflect_set": true, "__reflect_setzero": true,
	"__reflect_setmapindex": true, "__reflect_mapindex": true,
	"__reflect_interface": true, "__reflect_rawinterface": true, "__reflect_convert": true,
	"__reflect_slice": true, "__reflect_slice3": true,
	"__reflect_grow": true, "__reflect_setlen": true, "__reflect_setcap": true,
	"__reflect_clear": true,
	"__reflect_call":  true, "__reflect_callslice": true, "__reflect_methodval": true,
	"__reflect_makefunc": true,
	"__reflect_append":   true, "__reflect_makeslice": true, "__reflect_makemap": true,
	"__reflect_makechan": true, "__reflect_sliceat": true,
	"__reflect_send": true, "__reflect_recv": true, "__reflect_trysend": true,
	"__reflect_close": true, "__reflect_swap": true,
	"__reflect_selectimpl":  true,
	"__reflect_mapiterinit": true, "__reflect_mapiternext": true,
	"__reflect_mapiterkey": true, "__reflect_mapiterval": true,
	"__reflect_mapiterreset": true,
}

// isReflectIntrinsicFunc reports whether fn is a synthetic reflect
// intrinsic (package-level __reflect_* in the synthetic reflect package).
func isReflectIntrinsicFunc(fn *types.Func) bool {
	return fn.Pkg() != nil && fn.Pkg().Path() == "reflect" && reflectIntrinsicNames[fn.Name()]
}

// exportReflectIntrinsic lowers a call to a synthetic reflect intrinsic
// to the "reflectop" wire node: {"op","args","type"}. The Java
// ReflectOpNode dispatches on op; the result type id lets Java box the
// result per the call-site type. Intercepted in exportFuncCall before
// the funcByObj lookup (staged patch 02).
func (e *exporter) exportReflectIntrinsic(c *ast.CallExpr, name string) (*Node, error) {
	args, err := e.exportCallArgs(c)
	if err != nil {
		return nil, err
	}
	typ, err := e.callType(c)
	if err != nil {
		return nil, err
	}
	op := strings.TrimPrefix(name, "__reflect_")
	return e.node("reflectop", c, map[string]any{
		"op":   op,
		"args": args,
		"type": typ,
	}), nil
}
