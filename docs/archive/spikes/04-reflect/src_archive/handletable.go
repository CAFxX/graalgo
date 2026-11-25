// Package handletable emulates unsafe.Pointer as an opaque handle.
//
// Design under test: unsafe.Pointer is NOT a machine address but a handle
// (object id, offset). All of reflect's unsafe idioms must have a viable
// formulation in this model, with raw-address mode reserved for foreign /
// not-in-heap memory and a small set of dedicated intrinsics (call frames,
// code-pointer tokens, module offset resolution) for the rest.
//
// Language choice: Go, for the same reason as the layout core — the
// semantics under test are language-agnostic (handle arithmetic, bounds
// checks, word access); Go lets the tests run under the gc oracle directly.
// A production Truffle port replaces Object.data ([]byte) with the real
// managed object; the handle algebra is unchanged.
package handletable

import "fmt"

// Handle is an opaque unsafe.Pointer surrogate.
// Encoding: high 32 bits = object index (0 reserved for nil),
// low 32 bits = byte offset within the object.
type Handle uint64

const (
	nilHandle Handle = 0
	wordSize  = 8
)

// Object is one addressable allocation in the emulated heap.
type Object struct {
	data []byte // in production: the managed Truffle object
	name string // for diagnostics
}

// Table is the handle table.
type Table struct {
	objs []Object // objs[0] is unused; index 0 == nil handle
}

func NewTable() *Table { return &Table{objs: []Object{{}}} }

func split(h Handle) (idx uint32, off uint64) {
	return uint32(h >> 32), uint64(h & 0xffffffff)
}

func join(idx uint32, off uint64) Handle {
	if off > 0xffffffff {
		panic(fmt.Sprintf("handletable: offset %d exceeds 32-bit handle range", off))
	}
	return Handle(uint64(idx)<<32 | off)
}

// Alloc models address-of a Go variable: unsafe.Pointer(&x). (AD)
func (t *Table) Alloc(size uintptr, name string) Handle {
	t.objs = append(t.objs, Object{data: make([]byte, size), name: name})
	return join(uint32(len(t.objs)-1), 0)
}

// Zero is the canonical zero object: unsafe.Pointer(&zeroVal[0]). (ZV)
func (t *Table) Zero() Handle {
	if len(t.objs) == 1 || t.objs[1].name != "zeroVal" {
		t.objs = append(t.objs, Object{data: make([]byte, 4096), name: "zeroVal"})
	}
	return join(1, 0)
}

func (t *Table) check(h Handle, access uintptr) (idx uint32, off uint64) {
	idx, off = split(h)
	if idx == 0 || int(idx) >= len(t.objs) {
		panic(fmt.Sprintf("handletable: nil/dangling handle %x", uint64(h)))
	}
	if off+uint64(access) > uint64(len(t.objs[idx].data)) {
		panic(fmt.Sprintf("handletable: out-of-bounds access off=%d len=%d on %q",
			off, access, t.objs[idx].name))
	}
	return idx, off
}

// Add is reflect's add(p, x): pointer arithmetic within one object. (PA)
// The whySafe contract becomes a bounds check.
func (t *Table) Add(h Handle, x uintptr) Handle {
	idx, off := split(h)
	if idx == 0 || int(idx) >= len(t.objs) {
		panic("handletable: Add on nil/dangling handle")
	}
	if off+uint64(x) > uint64(len(t.objs[idx].data)) {
		panic(fmt.Sprintf("handletable: Add(%d) out of bounds on %q", x, t.objs[idx].name))
	}
	return join(idx, off+uint64(x))
}

// IsAligned8 answers the isZero idiom `uintptr(ptr)%8 != 0` from handle
// metadata, without materializing an address. (PA)
func (t *Table) IsAligned8(h Handle) bool {
	_, off := split(h)
	return off%8 == 0
}

// ReadWord / WriteWord model *(*unsafe.Pointer)(p) and (*unsafe.Pointer)(p) = v. (RW)
func (t *Table) ReadWord(h Handle) uintptr {
	idx, off := t.check(h, wordSize)
	d := t.objs[idx].data[off : off+wordSize]
	return uintptr(d[0]) | uintptr(d[1])<<8 | uintptr(d[2])<<16 | uintptr(d[3])<<24 |
		uintptr(d[4])<<32 | uintptr(d[5])<<40 | uintptr(d[6])<<48 | uintptr(d[7])<<56
}

func (t *Table) WriteWord(h Handle, v uintptr) {
	idx, off := t.check(h, wordSize)
	d := t.objs[idx].data[off : off+wordSize]
	for i := 0; i < 8; i++ {
		d[i] = byte(v >> (8 * i))
	}
}

// ReadBytes / WriteBytes model memmove-style raw copies. (RW)
func (t *Table) ReadBytes(h Handle, n uintptr) []byte {
	idx, off := t.check(h, n)
	out := make([]byte, n)
	copy(out, t.objs[idx].data[off:off+uint64(n)])
	return out
}

func (t *Table) WriteBytes(h Handle, b []byte) {
	idx, off := t.check(h, uintptr(len(b)))
	copy(t.objs[idx].data[off:], b)
}

// SliceFrom models unsafe.Slice((*byte)(p), n): a bounds-checked re-slice. (RW)
func (t *Table) SliceFrom(h Handle, n uintptr) Handle {
	t.check(h, n) // bounds check only; same object, same offset
	return h
}

// Eface is an interface value: two words (type, data). (IF)
type Eface struct{ Type, Data Handle }

// PackEface models *(*any)(unsafe.Pointer(&EmptyInterface{typ, data})). (IF)
func (t *Table) PackEface(typ, data Handle) Handle {
	h := t.Alloc(2*wordSize, "eface")
	t.WriteWord(h, uintptr(typ))
	t.WriteWord(t.Add(h, wordSize), uintptr(data))
	return h
}

// EfaceType models prototype extraction: *(**ptrType)(unsafe.Pointer(&i)). (IF)
func (t *Table) EfaceType(h Handle) Handle { return Handle(t.ReadWord(h)) }
func (t *Table) EfaceData(h Handle) Handle { return Handle(t.ReadWord(t.Add(h, wordSize))) }

// SetEfaceData models (*abi.CommonInterface)(unsafe.Pointer(&out)).Data = d. (IF)
func (t *Table) SetEfaceData(h, d Handle) { t.WriteWord(t.Add(h, wordSize), uintptr(d)) }

// SliceHeader is a 3-word slice header object. (SH)
func (t *Table) SliceHeader(data Handle, length, cap_ uintptr) Handle {
	h := t.Alloc(3*wordSize, "sliceheader")
	t.WriteWord(h, uintptr(data))
	t.WriteWord(t.Add(h, wordSize), length)
	t.WriteWord(t.Add(h, 2*wordSize), cap_)
	return h
}
func (t *Table) SliceData(h Handle) Handle   { return Handle(t.ReadWord(h)) }
func (t *Table) SliceLen(h Handle) uintptr  { return t.ReadWord(t.Add(h, wordSize)) }

// Frame models a call frame for callReflect/callMethod word traffic. (CF)
func (t *Table) Frame(nwords uintptr) Handle { return t.Alloc(nwords*wordSize, "frame") }

// FuncTable assigns synthetic code pointers (tokens) to functions,
// replacing machine PCs which do not exist on Truffle. (FP)
type FuncTable struct {
	names []string
	byTok map[uintptr]int
}

func NewFuncTable() *FuncTable { return &FuncTable{byTok: map[uintptr]int{}} }

// Register returns a stable synthetic code pointer token.
func (f *FuncTable) Register(name string) uintptr {
	tok := uintptr(0x1000 + len(f.names)*0x10)
	f.names = append(f.names, name)
	f.byTok[tok] = len(f.names) - 1
	return tok
}
func (f *FuncTable) Name(tok uintptr) string { return f.names[f.byTok[tok]] }

// TokenTable maps handles <-> opaque uintptr tokens for the
// Pointer()/UnsafeAddr() user-facing surface. Tokens are NOT addresses;
// they round-trip but do not support arithmetic. (Pointer<->uintptr)
type TokenTable struct {
	tab   *Table
	toTok map[Handle]uintptr
	toH   map[uintptr]Handle
	next  uintptr
}

func NewTokenTable(tab *Table) *TokenTable {
	return &TokenTable{tab: tab, toTok: map[Handle]uintptr{}, toH: map[uintptr]Handle{}, next: 0x10000}
}

// TokenOf models uintptr(ptr): materialize an opaque token, never an address.
func (tt *TokenTable) TokenOf(h Handle) uintptr {
	if h == nilHandle {
		return 0
	}
	if tok, ok := tt.toTok[h]; ok {
		return tok
	}
	tok := tt.next
	tt.next += 0x100
	tt.toTok[h] = tok
	tt.toH[tok] = h
	return tok
}

// HandleOf models NewAt-style uintptr->Pointer: token round-trip.
func (tt *TokenTable) HandleOf(tok uintptr) (Handle, bool) {
	if tok == 0 {
		return nilHandle, true
	}
	h, ok := tt.toH[tok]
	return h, ok
}

// RaceNote models race.ReadPC(ptr, callpc, fn): the race runtime is
// handle-aware, so it accepts the handle directly. (RC)
func RaceNote(h Handle, callPC uintptr, fnName string) string {
	return fmt.Sprintf("race: obj=%x pc=%x fn=%s", uint64(h), callPC, fnName)
}
