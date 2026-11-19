// Unsafe idioms of the standard library, as local replicas and through the
// real sync/atomic: atomic.Value's efaceWords, reflect-style interface header
// reads, strings.Builder's unsafe.String, sync.Pool's indexLocal, noescape,
// the slice-header and string-header word views.
package main

import (
	"sync/atomic"
	"unsafe"
)

// ---- reflect / internal/abi: the two words of an interface value.
type emptyInterface struct {
	typ  unsafe.Pointer
	data unsafe.Pointer
}

type point struct{ x, y int }

func typeOf(i any) unsafe.Pointer { return (*emptyInterface)(unsafe.Pointer(&i)).typ }

func dataOf(i any) unsafe.Pointer { return (*emptyInterface)(unsafe.Pointer(&i)).data }

// ---- strings.Builder.
type builder struct {
	addr *builder
	buf  []byte
}

func (b *builder) WriteString(s string) { b.buf = append(b.buf, s...) }
func (b *builder) String() string       { return unsafe.String(unsafe.SliceData(b.buf), len(b.buf)) }

//go:nosplit
func noescape(p unsafe.Pointer) unsafe.Pointer {
	x := uintptr(p)
	return unsafe.Pointer(x ^ 0)
}

// ---- sync.Pool: per-P array indexed through uintptr arithmetic.
type poolLocal struct {
	private any
	shared  [2]uint64
	pad     [100]byte
}

func indexLocal(l unsafe.Pointer, i int) *poolLocal {
	lp := unsafe.Pointer(uintptr(l) + uintptr(i)*unsafe.Sizeof(poolLocal{}))
	return (*poolLocal)(lp)
}

// ---- reflect.SliceHeader / StringHeader shaped views.
type sliceHeader struct {
	data unsafe.Pointer
	len  int
	cap  int
}

type stringHeader struct {
	data unsafe.Pointer
	len  int
}

type config struct {
	name string
	n    int
}

func main() {
	// Interface headers: type words are equal for equal dynamic types, nil for nil.
	var e any
	println(typeOf(e) == nil, dataOf(e) == nil)
	p := &point{3, 4}
	println(typeOf(1) == typeOf(2), typeOf(1) == typeOf("x"), typeOf(p) == typeOf(&point{}), typeOf(1) != nil)
	println(dataOf(p) == unsafe.Pointer(p), (*point)(dataOf(p)).y, *(*int)(dataOf(42)), *(*string)(dataOf("str")))
	println((*point)(dataOf(point{5, 6})).x, *(*float64)(dataOf(2.5)), *(*bool)(dataOf(true)), *(*uint8)(dataOf(uint8(200))))
	var err error
	println((*emptyInterface)(unsafe.Pointer(&err)).typ == nil)

	// Writing a header word by word builds the interface value (atomic.Value's Load).
	var out any
	w := (*emptyInterface)(unsafe.Pointer(&out))
	w.typ = typeOf(77)
	w.data = dataOf(77)
	println(out.(int), out == any(77))
	w.typ = typeOf(p)
	w.data = unsafe.Pointer(p)
	println(out.(*point) == p, out.(*point).x)
	src := any(config{"cfg", 9})
	w.typ = typeOf(src)
	w.data = dataOf(src)
	println(out.(config).name, out.(config).n, out == src)

	// The real atomic.Value: Store, Load, Swap, CompareAndSwap.
	var v atomic.Value
	println(v.Load() == nil)
	v.Store(config{"a", 1})
	c := v.Load().(config)
	println(c.name, c.n)
	v.Store(config{"b", 2})
	old := v.Swap(config{"c", 3})
	println(old.(config).name, v.Load().(config).name)
	println(v.CompareAndSwap(config{"x", 0}, config{"y", 0}), v.CompareAndSwap(config{"c", 3}, config{"d", 4}))
	println(v.Load().(config).name, v.Load().(config).n)
	var vp atomic.Value
	vp.Store(p)
	println(vp.Load().(*point) == p)
	var vi atomic.Value
	vi.Store(1 << 40)
	vi.Store(-5)
	println(vi.Load().(int))
	func() {
		defer func() { println(recover().(string)) }()
		vi.Store("inconsistent")
	}()
	func() {
		defer func() { println(recover().(string)) }()
		vi.Store(nil)
	}()

	// strings.Builder.
	var sb builder
	sb.WriteString("built ")
	sb.WriteString("without ")
	sb.WriteString("a copy")
	s := sb.String()
	println(s, len(s), unsafe.StringData(s) == unsafe.SliceData(sb.buf))
	np := (*point)(noescape(unsafe.Pointer(p)))
	println(np == p, np.x)

	// sync.Pool indexing.
	locals := make([]poolLocal, 4)
	base := unsafe.Pointer(&locals[0])
	for i := range locals {
		l := indexLocal(base, i)
		l.private = i * 11
		l.shared[1] = uint64(i)
		l.pad[99] = byte(i)
	}
	println(locals[3].private.(int), locals[2].shared[1], locals[1].pad[99], indexLocal(base, 2) == &locals[2])

	// Slice and string headers through word views.
	xs := []int{1, 2, 3, 4, 5}
	sh := (*sliceHeader)(unsafe.Pointer(&xs))
	println(sh.len, sh.cap, sh.data == unsafe.Pointer(&xs[0]), *(*int)(sh.data))
	sh.len = 2
	println(len(xs), cap(xs))
	sh.len, sh.cap = 4, 4
	println(len(xs), cap(xs), xs[3])
	sh.data = unsafe.Pointer(&xs[1])
	sh.len, sh.cap = 3, 3
	println(xs[0], xs[2], len(xs))
	str := "header"
	st := (*stringHeader)(unsafe.Pointer(&str))
	println(st.len, *(*byte)(st.data), st.data == unsafe.Pointer(unsafe.StringData(str)))
	st.len = 4
	println(str)
	var built string
	bh := (*stringHeader)(unsafe.Pointer(&built))
	bytes := []byte("assembled")
	bh.data = unsafe.Pointer(&bytes[0])
	bh.len = len(bytes)
	println(built, len(built))

	// atomic operations through reinterpreted pointers.
	var flag int32 = -1
	println(atomic.LoadUint32((*uint32)(unsafe.Pointer(&flag))), atomic.AddUint32((*uint32)(unsafe.Pointer(&flag)), 2), flag)
	var word uint64 = 1 << 63
	println(atomic.AddInt64((*int64)(unsafe.Pointer(&word)), -1) > 0, word)
	var ptr *point
	println(atomic.CompareAndSwapPointer((*unsafe.Pointer)(unsafe.Pointer(&ptr)), nil, unsafe.Pointer(p)), ptr == p)
	println((*point)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&ptr)))).y)
}
