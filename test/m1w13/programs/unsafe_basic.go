// The unsafe patterns real standard-library code uses, on the pointer model
// of SPEC §3.4 / GG-0062 (ADR-0015): reinterpreting casts, byte views of
// scalars, unsafe.String/Slice/StringData/SliceData, unsafe.Add, uintptr
// round trips, Sizeof/Alignof/Offsetof under type parameters.
package main

import "unsafe"

type pair struct {
	a uint32
	b uint16
	c uint8
	d bool
	e float64
}

type hdr struct {
	tag [4]byte
	n   uint64
	p   *int
}

func float32bits(f float32) uint32     { return *(*uint32)(unsafe.Pointer(&f)) }
func float32frombits(b uint32) float32 { return *(*float32)(unsafe.Pointer(&b)) }
func float64bits(f float64) uint64     { return *(*uint64)(unsafe.Pointer(&f)) }
func float64frombits(b uint64) float64 { return *(*float64)(unsafe.Pointer(&b)) }

func sizeOf[T any]() uintptr {
	var v T
	return unsafe.Sizeof(v)
}

func alignOf[T any]() uintptr {
	var v T
	return unsafe.Alignof(v)
}

type box[T any] struct {
	pad uint8
	v   T
	end bool
}

func offsets[T any]() (uintptr, uintptr, uintptr) {
	var b box[T]
	return unsafe.Offsetof(b.v), unsafe.Offsetof(b.end), unsafe.Sizeof(b)
}

func main() {
	// math.Float32bits-style casts, both directions.
	println(float32bits(1.5), float32bits(-0.0), float64bits(1.5), float64bits(-2.25))
	println(float32frombits(0x40490fdb), float64frombits(0x400921fb54442d18), float32frombits(0x7f800000))

	// *(*uint64)(unsafe.Pointer(&b[0])): the bytes of a slice as wider scalars, loads and stores.
	b := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	println(*(*uint64)(unsafe.Pointer(&b[0])), *(*uint32)(unsafe.Pointer(&b[4])), *(*uint16)(unsafe.Pointer(&b[1])))
	println(*(*int16)(unsafe.Pointer(&b[10])), *(*int8)(unsafe.Pointer(&b[3])))
	*(*uint32)(unsafe.Pointer(&b[2])) = 0xDDCCBBAA
	*(*uint16)(unsafe.Pointer(&b[8])) = 0xFFEE
	for _, c := range b {
		print(c, " ")
	}
	println()

	// The bytes of a wide scalar, and of a struct with padding.
	x := uint64(0x1122334455667788)
	xb := (*[8]byte)(unsafe.Pointer(&x))
	println(xb[0], xb[7], len(xb))
	xb[1] = 0xFF
	println(x == 0x112233445566ff88)
	i32 := int32(-2)
	println(*(*uint32)(unsafe.Pointer(&i32)), *(*uint16)(unsafe.Pointer(&i32)), *(*int8)(unsafe.Pointer(&i32)))
	*(*uint16)(unsafe.Add(unsafe.Pointer(&i32), 2)) = 0
	println(i32)
	pr := pair{0xAABBCCDD, 0x1122, 0x33, true, 2.5}
	base := unsafe.Pointer(&pr)
	println(*(*uint32)(base), *(*uint16)(unsafe.Add(base, 4)), *(*uint8)(unsafe.Add(base, 6)), *(*bool)(unsafe.Add(base, 7)))
	println(*(*uint64)(base), *(*uint64)(unsafe.Add(base, 8)) == float64bits(2.5))
	*(*uint64)(base) = 0x0100000200000003
	println(pr.a, pr.b, pr.c, pr.d)
	*(*float64)(unsafe.Add(base, unsafe.Offsetof(pr.e))) = -1
	println(pr.e)

	// Field pointers through unsafe.Add are the typed pointers again.
	pb := (*uint16)(unsafe.Add(base, unsafe.Offsetof(pr.b)))
	println(pb == &pr.b, unsafe.Pointer(pb) == unsafe.Pointer(&pr.b), unsafe.Add(base, 4) == unsafe.Pointer(&pr.b))
	*pb = 7
	println(pr.b)

	// unsafe.String / StringData / Slice / SliceData share storage.
	buf := []byte("hello, world")
	s := unsafe.String(&buf[0], len(buf))
	sub := unsafe.String(&buf[7], 5)
	println(s, sub, len(s), unsafe.StringData(s) == &buf[0], unsafe.StringData(sub) == &buf[7])
	buf[0] = 'J'
	println(s, unsafe.SliceData(buf) == &buf[0], unsafe.String(unsafe.SliceData(buf), 5))
	str := "immutable"
	view := unsafe.Slice(unsafe.StringData(str), len(str))
	println(len(view), cap(view), view[0], view[8], string(view[2:5]), unsafe.StringData(str) == &view[0])
	println(unsafe.String(nil, 0) == "", len(unsafe.Slice((*int)(nil), 0)), unsafe.Slice((*int)(nil), 0) == nil)
	ints := []int32{10, 20, 30, 40, 50}
	tail := unsafe.Slice(&ints[2], 3)
	tail[0] = 33
	println(len(tail), cap(tail), ints[2], tail[2], unsafe.SliceData(tail) == &ints[2])
	arr := [4]uint16{1, 2, 3, 4}
	as := unsafe.Slice(&arr[1], 3)
	as[2] = 44
	println(arr[3], len(as))

	// Sizeof/Alignof/Offsetof of types that depend on a type parameter.
	println(sizeOf[pair](), alignOf[pair](), sizeOf[hdr](), alignOf[hdr](), sizeOf[[3]pair](), sizeOf[struct{}]())
	println(sizeOf[string](), sizeOf[[]int](), sizeOf[any](), sizeOf[complex64](), alignOf[complex64](), alignOf[[0]int64]())
	println(offsets[uint8]())
	println(offsets[int64]())
	println(offsets[pair]())
	println(offsets[struct{}]())
	println(offsets[[3]uint16]())

	// uintptr round trips inside an object.
	h := hdr{[4]byte{9, 8, 7, 6}, 42, nil}
	u := uintptr(unsafe.Pointer(&h))
	println(*(*uint64)(unsafe.Pointer(u + unsafe.Offsetof(h.n))), *(*byte)(unsafe.Pointer(u + 3)))
	println(unsafe.Pointer(u) == unsafe.Pointer(&h), unsafe.Pointer(u+8) == unsafe.Pointer(&h.n), uintptr(unsafe.Pointer(&h.n))-u)
	println(uintptr(unsafe.Pointer(nil)), unsafe.Pointer(uintptr(0)) == nil)
	end := unsafe.Pointer(uintptr(unsafe.Pointer(&b[0])) + uintptr(len(b)) - 1)
	println(*(*byte)(end))
	q := (*int)(nil)
	h.p = (*int)(unsafe.Pointer(&h.n))
	println(*h.p, h.p != q, *(**int)(unsafe.Add(unsafe.Pointer(&h), 16)) == h.p)
}
