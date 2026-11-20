package main

// The two internal/reflectlite functions whose upstream bodies need a byte or
// word view of wider scalars (BUG-0037; they run as temporary intrinsics):
// reflectlite.Swapper, reached by sort.Slice/SliceStable over every element
// class — scalars whose slot is not the int8..int64 of their size (float32,
// complex64, uint16, uint32), multi-leaf elements of 2, 4 and 8 bytes,
// pointer-containing and zero-size elements, odd sizes, slices of an array
// inlined in a struct — and reflectlite.pkgPath, reached by errors.As to an
// unnamed interface type with an unexported method.

import (
	"errors"
	"sort"
)

type pair struct{ a, b int32 }
type halves struct{ a, b uint16 }
type fpair struct{ a, b float32 }
type bytes3 [3]byte
type node struct {
	key int
	ptr *int
}
type holder struct {
	tag  string
	vals [6]float32
	tail int
}

type E struct{ code int }

func (e E) Error() string { return "E" }
func (e E) secret() int   { return e.code }

type named interface {
	error
	secret() int
}

type wrapped struct{ err error }

func (w wrapped) Error() string { return "wrapped: " + w.err.Error() }
func (w wrapped) Unwrap() error { return w.err }

func main() {
	f32 := []float32{3.5, -1, 2.25, 0, 1e30, -1e-30}
	sort.Slice(f32, func(i, j int) bool { return f32[i] < f32[j] })
	println(f32[0], f32[1], f32[2], f32[3], f32[4], f32[5])

	u16 := []uint16{65535, 1, 300, 0, 32768}
	sort.Slice(u16, func(i, j int) bool { return u16[i] < u16[j] })
	println(u16[0], u16[1], u16[2], u16[3], u16[4])

	u32 := []uint32{4294967295, 7, 1 << 31, 0}
	sort.Slice(u32, func(i, j int) bool { return u32[i] < u32[j] })
	println(u32[0], u32[1], u32[2], u32[3])

	u64 := []uint64{1 << 63, 2, 0}
	sort.Slice(u64, func(i, j int) bool { return u64[i] < u64[j] })
	println(u64[0], u64[1], u64[2])

	c64 := []complex64{3 + 1i, -1 + 2i, 2 - 3i}
	sort.Slice(c64, func(i, j int) bool { return real(c64[i]) < real(c64[j]) })
	println(real(c64[0]), imag(c64[0]), real(c64[2]), imag(c64[2]))

	ps := []pair{{3, -3}, {1, -1}, {2, -2}, {0, 0}}
	sort.Slice(ps, func(i, j int) bool { return ps[i].a < ps[j].a })
	println(ps[0].a, ps[0].b, ps[1].b, ps[2].b, ps[3].a, ps[3].b)

	hs := []halves{{3, 30}, {1, 10}, {2, 20}}
	sort.SliceStable(hs, func(i, j int) bool { return hs[i].a < hs[j].a })
	println(hs[0].b, hs[1].b, hs[2].b)

	fs := []fpair{{2.5, 1}, {-1, 2}, {0.5, 3}}
	sort.Slice(fs, func(i, j int) bool { return fs[i].a < fs[j].a })
	println(fs[0].b, fs[1].b, fs[2].b)

	arrs := [][2]int32{{9, 1}, {-4, 2}, {5, 3}}
	sort.Slice(arrs, func(i, j int) bool { return arrs[i][0] < arrs[j][0] })
	println(arrs[0][1], arrs[1][1], arrs[2][1])

	b3 := []bytes3{{3, 0, 9}, {1, 2, 3}, {2, 4, 6}}
	sort.Slice(b3, func(i, j int) bool { return b3[i][0] < b3[j][0] })
	println(b3[0][2], b3[1][2], b3[2][2])

	strs := []string{"pear", "fig", "apple"}
	sort.Slice(strs, func(i, j int) bool { return strs[i] < strs[j] })
	println(strs[0], strs[1], strs[2])

	x, y, z := 3, 1, 2
	ptrs := []*int{&x, &y, &z}
	sort.Slice(ptrs, func(i, j int) bool { return *ptrs[i] < *ptrs[j] })
	println(*ptrs[0], *ptrs[1], *ptrs[2], ptrs[0] == &y)

	nodes := []node{{3, &x}, {1, &y}, {2, &z}}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].key < nodes[j].key })
	println(*nodes[0].ptr, *nodes[1].ptr, *nodes[2].ptr)

	errs := []error{E{3}, E{1}, E{2}}
	sort.Slice(errs, func(i, j int) bool { return errs[i].(E).code < errs[j].(E).code })
	println(errs[0].(E).code, errs[1].(E).code, errs[2].(E).code)

	empty := make([]struct{}, 5)
	calls := 0
	sort.Slice(empty, func(i, j int) bool { calls++; return i > j })
	sort.SliceStable(empty, func(i, j int) bool { return false })
	println(len(empty), calls > 0)

	one := []float32{7}
	sort.Slice(one, func(i, j int) bool { return true })
	println(one[0])

	// A slice of an array inlined in a struct: the struct's other fields stay.
	h := holder{tag: "h", vals: [6]float32{6, 5, 4, 3, 2, 1}, tail: 99}
	sub := h.vals[1:5]
	sort.Slice(sub, func(i, j int) bool { return sub[i] < sub[j] })
	println(h.tag, h.vals[0], h.vals[1], h.vals[2], h.vals[3], h.vals[4], h.vals[5], h.tail)

	// errors.As: a defined interface type, then unnamed ones with an
	// unexported method (the name carries main's package path).
	var n named
	println(errors.As(error(E{7}), &n), n != nil && n.secret() == 7)
	var a interface {
		error
		secret() int
	}
	println(errors.As(error(E{8}), &a), a != nil && a.secret() == 8)
	var w interface {
		secret() int
	}
	println(errors.As(wrapped{E{9}}, &w), w != nil && w.secret() == 9)
	var miss interface {
		error
		hidden() bool
	}
	println(errors.As(error(E{1}), &miss), miss == nil)
}
