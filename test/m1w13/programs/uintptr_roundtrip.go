// uintptr round trips the node factory lowers without the handle table
// (NodeFactory.pointerLocals, pointerArith): a local that only carries a
// pointer (abi.NoEscape's shape, with ^ 0, | 0, + 0 and plain), pointer
// arithmetic in one expression (+ and -, field and element steps), and the
// neighbours that must keep integer semantics (a uintptr local that is
// printed, compared, incremented or reassigned).
package main

import "unsafe"

type rec struct {
	a int64
	b [4]int32
	s string
	c byte
}

//go:noinline
func noEscape(p unsafe.Pointer) unsafe.Pointer {
	x := uintptr(p)
	return unsafe.Pointer(x ^ 0)
}

//go:noinline
func viaOr(p unsafe.Pointer) unsafe.Pointer {
	x := uintptr(p)
	return unsafe.Pointer(x | 0)
}

//go:noinline
func plain(p unsafe.Pointer) unsafe.Pointer {
	x := uintptr(p)
	y := unsafe.Pointer(x)
	return unsafe.Pointer(uintptr(y) + 0)
}

//go:noinline
func twice(p, q unsafe.Pointer, first bool) unsafe.Pointer {
	x := uintptr(p)
	if !first {
		x := uintptr(q)
		return unsafe.Pointer(x - 0)
	}
	return unsafe.Pointer(x)
}

// An integer local stays an integer: it is compared and printed.
//go:noinline
func observed(p, q unsafe.Pointer) (bool, bool) {
	x := uintptr(p)
	y := uintptr(q)
	return x == y, x != 0
}

// Reassigned and stepped: not a pointer local.
//go:noinline
func stepped(p unsafe.Pointer, n int) int32 {
	x := uintptr(p)
	x += unsafe.Offsetof(rec{}.b)
	x += uintptr(n) * 4
	return *(*int32)(unsafe.Pointer(x))
}

func main() {
	r := &rec{a: 7, b: [4]int32{10, 20, 30, 40}, s: "hi", c: 'z'}
	p := unsafe.Pointer(r)
	println((*rec)(noEscape(p)).a, (*rec)(viaOr(p)).s, (*rec)(plain(p)).c)
	println((*rec)(twice(p, unsafe.Pointer(&r.b[2]), true)).a, *(*int32)(twice(p, unsafe.Pointer(&r.b[2]), false)))
	same, nonzero := observed(p, unsafe.Pointer(r))
	diff, _ := observed(p, unsafe.Pointer(&r.s))
	println(same, nonzero, diff)
	for i := 0; i < 4; i++ {
		println(stepped(p, i))
	}
	// Pointer arithmetic in one expression, both directions.
	e2 := (*int32)(unsafe.Pointer(uintptr(unsafe.Pointer(&r.b[0])) + 2*unsafe.Sizeof(r.b[0])))
	e1 := (*int32)(unsafe.Pointer(uintptr(unsafe.Pointer(e2)) - unsafe.Sizeof(r.b[0])))
	println(*e2, *e1)
	s := (*string)(unsafe.Pointer(uintptr(p) + unsafe.Offsetof(r.s)))
	println(*s, len(*s))
	arr := []uint16{1, 2, 3, 4, 5}
	sum := uint16(0)
	base := unsafe.Pointer(&arr[0])
	for i := 0; i < len(arr); i++ {
		sum += *(*uint16)(unsafe.Pointer(uintptr(base) + uintptr(i)*2))
	}
	println(sum)
	// Hot: the round trip on fresh objects.
	t := 0
	for i := 0; i < 200000; i++ {
		q := &rec{a: int64(i)}
		t += int((*rec)(noEscape(unsafe.Pointer(q))).a & 3)
	}
	println(t)
}
