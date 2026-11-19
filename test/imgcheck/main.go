// Command imgcheck prints every fact of the type descriptors of a corpus of
// types (corpus.go), read the way internal/abi and internal/reflectlite read
// them: through unsafe conversions of an interface's type word to mirror
// structs of internal/abi's descriptor types, name and type offsets resolved
// by the runtime (toolexec-design §3.4, §5.6, W9a; abi-spike §1.4).
//
// test/imgcheck/run.sh runs it twice — built by gc, where the descriptors
// are the compiler's, and through graalgo-tool on the node package, where
// they are the images graalgo/go/types/Descriptors.java builds — and
// compares the two outputs: byte for byte for the scalar header of
// abi.Type, fact for fact for everything else. Under gc, oracle_gc.go also
// checks the walker itself against package reflect.
//
// The mirror structs are go1.27.1's internal/abi; a release that changes
// the layout changes them (the reflect cross-check fails first).
package main

import (
	"sort"
	"unsafe"
)

type abiType struct {
	Size_       uintptr
	PtrBytes    uintptr
	Hash        uint32
	TFlag       uint8
	Align_      uint8
	FieldAlign_ uint8
	Kind_       uint8
	Equal       func(unsafe.Pointer, unsafe.Pointer) bool
	GCData      *byte
	Str         int32
	PtrToThis   int32
}

const (
	tflagUncommon       = 1 << 0
	tflagExtraStar      = 1 << 1
	tflagNamed          = 1 << 2
	tflagRegularMemory  = 1 << 3
	tflagGCMaskOnDemand = 1 << 4
	tflagDirectIface    = 1 << 5
)

const (
	kArray = 17 + iota
	kChan
	kFunc
	kInterface
	kMap
	kPointer
	kSlice
	kString
	kStruct
	kUnsafePointer
)

type abiName struct{ Bytes *byte }

type uncommonType struct {
	PkgPath int32
	Mcount  uint16
	Xcount  uint16
	Moff    uint32
	_       uint32
}

type abiMethod struct {
	Name int32
	Mtyp int32
	Ifn  int32
	Tfn  int32
}

type imethod struct {
	Name int32
	Typ  int32
}

type arrayType struct {
	abiType
	Elem  *abiType
	Slice *abiType
	Len   uintptr
}

type chanType struct {
	abiType
	Elem *abiType
	Dir  int
}

type funcType struct {
	abiType
	InCount  uint16
	OutCount uint16
}

type interfaceType struct {
	abiType
	PkgPath abiName
	Methods []imethod
}

type mapType struct {
	abiType
	Key        *abiType
	Elem       *abiType
	Group      *abiType
	Hasher     func(unsafe.Pointer, uintptr) uintptr
	GroupSize  uintptr
	KeysOff    uintptr
	KeyStride  uintptr
	ElemsOff   uintptr
	ElemStride uintptr
	ElemOff    uintptr
	Flags      uint32
}

type ptrType struct {
	abiType
	Elem *abiType
}

type sliceType struct {
	abiType
	Elem *abiType
}

type structField struct {
	Name   abiName
	Typ    *abiType
	Offset uintptr
}

type structType struct {
	abiType
	PkgPath abiName
	Fields  []structField
}

type emptyInterface struct {
	Type *abiType
	Data unsafe.Pointer
}

// The runtime's resolvers, as internal/reflectlite declares them.

//go:linkname resolveNameOff internal/reflectlite.resolveNameOff
func resolveNameOff(ptrInModule unsafe.Pointer, off int32) unsafe.Pointer

//go:linkname resolveTypeOff internal/reflectlite.resolveTypeOff
func resolveTypeOff(rtype unsafe.Pointer, off int32) unsafe.Pointer

func add(p unsafe.Pointer, x uintptr) unsafe.Pointer { return unsafe.Pointer(uintptr(p) + x) }

func (n abiName) data(off int) *byte { return (*byte)(add(unsafe.Pointer(n.Bytes), uintptr(off))) }

func (n abiName) readVarint(off int) (int, int) {
	v := 0
	for i := 0; ; i++ {
		x := *n.data(off + i)
		v += int(x&0x7f) << (7 * i)
		if x&0x80 == 0 {
			return i + 1, v
		}
	}
}

func (n abiName) name() string {
	if n.Bytes == nil {
		return ""
	}
	i, l := n.readVarint(1)
	return unsafe.String(n.data(1+i), l)
}

func (n abiName) tag() string {
	if *n.data(0)&2 == 0 {
		return ""
	}
	i, l := n.readVarint(1)
	i2, l2 := n.readVarint(1 + i + l)
	return unsafe.String(n.data(1+i+l+i2), l2)
}

// pkgPath is reflectlite's: the package qualifying an unexported name that
// does not belong to its type's package.
func (n abiName) pkgPath() string {
	if n.Bytes == nil || *n.data(0)&(1<<2) == 0 {
		return ""
	}
	i, l := n.readVarint(1)
	off := 1 + i + l
	if *n.data(0)&2 != 0 {
		i2, l2 := n.readVarint(off)
		off += i2 + l2
	}
	// reflectlite copies the four bytes into an int32 through a []byte over
	// the variable — a byte view of a word, which the node package does not
	// give (docs/arch/limitations.md); the offset is little-endian.
	var nameOff int32
	for i := 0; i < 4; i++ {
		nameOff |= int32(*n.data(off + i)) << (8 * i)
	}
	return abiName{(*byte)(resolveTypeOff(unsafe.Pointer(n.Bytes), nameOff))}.name()
}

func (t *abiType) nameOff(off int32) abiName {
	return abiName{(*byte)(resolveNameOff(unsafe.Pointer(t), off))}
}

func (t *abiType) typeOff(off int32) *abiType {
	return (*abiType)(resolveTypeOff(unsafe.Pointer(t), off))
}

func (t *abiType) str() string {
	s := t.nameOff(t.Str).name()
	if t.TFlag&tflagExtraStar != 0 {
		return s[1:]
	}
	return s
}

func kindSize(t *abiType) uintptr {
	switch t.Kind_ {
	case kArray:
		return unsafe.Sizeof(arrayType{})
	case kChan:
		return unsafe.Sizeof(chanType{})
	case kFunc:
		return unsafe.Sizeof(funcType{})
	case kInterface:
		return unsafe.Sizeof(interfaceType{})
	case kMap:
		return unsafe.Sizeof(mapType{})
	case kPointer:
		return unsafe.Sizeof(ptrType{})
	case kSlice:
		return unsafe.Sizeof(sliceType{})
	case kStruct:
		return unsafe.Sizeof(structType{})
	}
	return unsafe.Sizeof(abiType{})
}

func (t *abiType) uncommon() *uncommonType {
	if t.TFlag&tflagUncommon == 0 {
		return nil
	}
	return (*uncommonType)(add(unsafe.Pointer(t), kindSize(t)))
}

func (u *uncommonType) methods() []abiMethod {
	if u.Mcount == 0 {
		return nil
	}
	return (*[1 << 16]abiMethod)(add(unsafe.Pointer(u), uintptr(u.Moff)))[:u.Mcount:u.Mcount]
}

func (t *funcType) params() []*abiType {
	n := int(t.InCount) + int(t.OutCount&(1<<15-1))
	if n == 0 {
		return nil
	}
	uadd := unsafe.Sizeof(*t)
	if t.TFlag&tflagUncommon != 0 {
		uadd += unsafe.Sizeof(uncommonType{})
	}
	return (*[1 << 17]*abiType)(add(unsafe.Pointer(t), uadd))[:n:n]
}

func typeOf(a any) *abiType {
	return (*emptyInterface)(unsafe.Pointer(&a)).Type
}

func dataOf(a any) unsafe.Pointer {
	return (*emptyInterface)(unsafe.Pointer(&a)).Data
}

const hexDigits = "0123456789abcdef"

func hex(b []byte) string {
	out := make([]byte, 0, 2*len(b))
	for _, x := range b {
		out = append(out, hexDigits[x>>4], hexDigits[x&15])
	}
	return string(out)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func b2s(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// walker visits the closure of the corpus's descriptors once each.
type walker struct {
	seen  map[*abiType]int
	queue []*abiType
	lines []string
	types int
	facts int
}

func (w *walker) ref(t *abiType) string {
	if t == nil {
		return "<nil>"
	}
	if _, ok := w.seen[t]; !ok {
		w.seen[t] = len(w.seen)
		w.queue = append(w.queue, t)
	}
	return "{" + t.str() + "}"
}

func (w *walker) fact(s string) {
	w.lines = append(w.lines, "  "+s)
	w.facts++
}

// describe prints every field of t's descriptor. Link-dependent words are
// left out, each for the reason given where it is skipped.
func (w *walker) describe(t *abiType) {
	w.types++
	w.lines = append(w.lines, "type "+t.str())
	// The scalar header of abi.Type, byte for byte: Size_, PtrBytes, Hash,
	// TFlag, Align_, FieldAlign_, Kind_. Read one byte at a time: a []byte
	// over words is one of the node package's documented restrictions.
	var header [24]byte
	for i := range header {
		header[i] = *(*byte)(add(unsafe.Pointer(t), uintptr(i)))
	}
	w.fact("header " + hex(header[:]))
	w.fact("size=" + itoa(int(t.Size_)) + " ptrbytes=" + itoa(int(t.PtrBytes)) + " hash=" + itoa(int(t.Hash)) +
		" tflag=" + itoa(int(t.TFlag)) + " align=" + itoa(int(t.Align_)) + " fieldalign=" + itoa(int(t.FieldAlign_)) +
		" kind=" + itoa(int(t.Kind_)) + " equal=" + b2s(t.Equal != nil))
	name := t.nameOff(t.Str)
	w.fact("str=" + name.name() + " exported=" + b2s(*name.data(0)&1 != 0))
	if t.TFlag&tflagGCMaskOnDemand == 0 {
		// One bit per pointer-sized word of the pointer prefix.
		n := (t.PtrBytes/8 + 7) / 8
		w.fact("gcmask " + hex(unsafe.Slice(t.GCData, int(n))))
	}
	if t.TFlag&tflagNamed != 0 {
		// gc emits *T for every defined type; for another type the pointer
		// type is in the binary only when something else needs it.
		pt := t.typeOff(t.PtrToThis)
		w.fact("ptrtothis=" + w.ref(pt) + " elem-is-self=" + b2s(pt != nil && (*ptrType)(unsafe.Pointer(pt)).Elem == t))
	} else if t.PtrToThis != 0 {
		if pt := t.typeOff(t.PtrToThis); (*ptrType)(unsafe.Pointer(pt)).Elem != t {
			w.fact("ptrtothis of an unnamed type is not the pointer to it")
		}
	}
	switch t.Kind_ {
	case kArray:
		a := (*arrayType)(unsafe.Pointer(t))
		w.fact("array elem=" + w.ref(a.Elem) + " slice=" + w.ref(a.Slice) + " len=" + itoa(int(a.Len)))
	case kChan:
		c := (*chanType)(unsafe.Pointer(t))
		w.fact("chan elem=" + w.ref(c.Elem) + " dir=" + itoa(c.Dir))
	case kPointer:
		w.fact("pointer elem=" + w.ref((*ptrType)(unsafe.Pointer(t)).Elem))
	case kSlice:
		w.fact("slice elem=" + w.ref((*sliceType)(unsafe.Pointer(t)).Elem))
	case kFunc:
		f := (*funcType)(unsafe.Pointer(t))
		w.fact("func in=" + itoa(int(f.InCount)) + " out=" + itoa(int(f.OutCount&(1<<15-1))) + " variadic=" + b2s(f.OutCount&(1<<15) != 0))
		for i, p := range f.params() {
			w.fact("param " + itoa(i) + " " + w.ref(p))
		}
	case kInterface:
		it := (*interfaceType)(unsafe.Pointer(t))
		w.fact("interface pkgpath=" + it.PkgPath.name() + " methods=" + itoa(len(it.Methods)) + " cap=" + itoa(cap(it.Methods)))
		for i := range it.Methods {
			m := &it.Methods[i]
			n := t.nameOff(m.Name)
			w.fact("imethod " + itoa(i) + " " + n.name() + " exported=" + b2s(*n.data(0)&1 != 0) + " pkg=" + n.pkgPath() + " typ=" + w.ref(t.typeOff(m.Typ)))
		}
	case kMap:
		m := (*mapType)(unsafe.Pointer(t))
		// Hasher is code: called, on a zero key, it must be a function of
		// the key and the seed (its values are the runtime's own).
		hasher := "<nil>"
		if m.Hasher != nil && m.Flags&2 == 0 {
			var zero [1024]uint64
			k := unsafe.Pointer(&zero)
			hasher = b2s(m.Key.Size_ > 8192 || m.Hasher(k, 1) == m.Hasher(k, 1) && m.Hasher(k, 1) != m.Hasher(k, 2))
		} else if m.Hasher != nil {
			hasher = "set"
		}
		w.fact("map key=" + w.ref(m.Key) + " elem=" + w.ref(m.Elem) + " hasher=" + hasher + " groupsize=" + itoa(int(m.GroupSize)) +
			" keysoff=" + itoa(int(m.KeysOff)) + " keystride=" + itoa(int(m.KeyStride)) + " elemsoff=" + itoa(int(m.ElemsOff)) +
			" elemstride=" + itoa(int(m.ElemStride)) + " elemoff=" + itoa(int(m.ElemOff)) + " flags=" + itoa(int(m.Flags)))
		w.group(m.Group)
	case kStruct:
		st := (*structType)(unsafe.Pointer(t))
		w.fact("struct pkgpath=" + st.PkgPath.name() + " fields=" + itoa(len(st.Fields)) + " cap=" + itoa(cap(st.Fields)))
		for i := range st.Fields {
			f := &st.Fields[i]
			w.fact("field " + itoa(i) + " " + f.Name.name() + " offset=" + itoa(int(f.Offset)) + " exported=" + b2s(*f.Name.data(0)&1 != 0) +
				" embedded=" + b2s(*f.Name.data(0)&8 != 0) + " tag=" + f.Name.tag() + " typ=" + w.ref(f.Typ))
		}
	}
	if u := t.uncommon(); u != nil {
		w.fact("uncommon pkgpath=" + t.nameOff(u.PkgPath).name() + " mcount=" + itoa(int(u.Mcount)) + " xcount=" + itoa(int(u.Xcount)) +
			" moff=" + itoa(int(u.Moff)))
		for i, m := range u.methods() {
			n := t.nameOff(m.Name)
			line := "method " + itoa(i) + " " + n.name() + " exported=" + b2s(*n.data(0)&1 != 0) + " pkg=" + n.pkgPath()
			if i < int(u.Xcount) {
				// gc's linker keeps the type of every exported method of a
				// type that reaches an interface once reflect's Method is
				// linked (oracle_gc.go); an unexported method's type is
				// there only if some interface in the binary has the
				// method. Ifn/Tfn are code (W9b).
				line += " mtyp=" + w.ref(t.typeOff(m.Mtyp))
			}
			w.fact(line)
		}
	}
}

// group prints the map group type: a struct no Go source can name, so only
// its own descriptor is described, not queued under a name.
func (w *walker) group(g *abiType) {
	st := (*structType)(unsafe.Pointer(g))
	w.fact("group kind=" + itoa(int(g.Kind_)) + " size=" + itoa(int(g.Size_)) + " ptrbytes=" + itoa(int(g.PtrBytes)) +
		" align=" + itoa(int(g.Align_)) + " tflag=" + itoa(int(g.TFlag)) + " equal=" + b2s(g.Equal != nil) + " fields=" + itoa(len(st.Fields)))
	for i := range st.Fields {
		f := &st.Fields[i]
		w.fact("group field " + itoa(i) + " " + f.Name.name() + " offset=" + itoa(int(f.Offset)) + " size=" + itoa(int(f.Typ.Size_)) +
			" kind=" + itoa(int(f.Typ.Kind_)))
	}
}

func main() {
	w := &walker{seen: map[*abiType]int{}}
	for _, r := range roots {
		w.ref(typeOf(r))
	}
	for len(w.queue) > 0 {
		t := w.queue[0]
		w.queue = w.queue[1:]
		w.describe(t)
	}
	// Equality functions, called: a value equals itself (not NaN), and a
	// pair of distinct values does not.
	for i, p := range equalPairs {
		t := typeOf(p[0])
		eq := "<nil>"
		if t.Equal != nil && t.TFlag&tflagDirectIface == 0 {
			eq = b2s(t.Equal(dataOf(p[0]), dataOf(p[0]))) + b2s(t.Equal(dataOf(p[0]), dataOf(p[1]))) + b2s(t.Equal(dataOf(p[1]), dataOf(p[1])))
		}
		w.lines = append(w.lines, "equal "+itoa(i)+" "+t.str()+" "+eq)
	}
	// Descriptor identity: the same type reached twice is one descriptor.
	same := typeOf([]int(nil)) == typeOf(make([]int, 3)) && typeOf(sort.IntSlice(nil)) != typeOf([]int(nil)) &&
		(*ptrType)(unsafe.Pointer(typeOf((*error)(nil)))).Elem == (*ptrType)(unsafe.Pointer(typeOf(new(error)))).Elem
	w.lines = append(w.lines, "identity "+b2s(same))
	oracle(w)
	for _, l := range w.lines {
		println(l)
	}
	println("imgcheck:", len(roots), "roots,", w.types, "types,", w.facts, "facts")
}
