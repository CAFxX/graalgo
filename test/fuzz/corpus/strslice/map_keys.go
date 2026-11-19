// Targets map key semantics: NaN keys (len grows, lookup fails, delete
// no-op, clear removes), +0/-0 the same key, struct/array/interface/string
// keys, interface keys with unhashable dynamic types panicking, pointer keys.
package main

import (
	"math"
	"slices"
	"strconv"
)

func protect(name string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(interface{ Error() string }); ok {
				println(name, "panic:", e.Error())
			} else if s, ok := r.(string); ok {
				println(name, "panic str:", s)
			}
		}
	}()
	f()
}

type key struct {
	a int
	s string
	f float64
}

type arrKey [3]int16

type ifKey interface{ M() int }

type impl struct{ v int }

func (i impl) M() int { return i.v }

type sliceImpl []int

func (s sliceImpl) M() int { return len(s) }

//go:noinline
func nan() float64 { return math.NaN() }

//go:noinline
func negZero() float64 { return math.Copysign(0, -1) }

func main() {
	m := map[float64]int{}
	for i := 0; i < 5; i++ {
		m[nan()] = i
	}
	m[1.5] = 100
	println("nanlen", len(m))
	v, ok := m[nan()]
	println("nanlookup", v, ok)
	delete(m, nan())
	println("nandelete", len(m))
	sum := 0
	nanCount := 0
	for k, v := range m {
		if k != k {
			nanCount++
		}
		sum += v
	}
	println("nanrange", nanCount, sum)
	clear(m)
	println("nanclear", len(m))
	// +0 / -0
	z := map[float64]string{}
	z[0.0] = "pos"
	z[negZero()] = "neg"
	println("zero", len(z), z[0], z[negZero()])
	for k := range z {
		println("zerokey sign", math.Signbit(k))
	}
	// float32 and complex keys
	f32 := map[float32]int{float32(negZero()): 1}
	f32[0] = 2
	println("f32", len(f32), f32[0])
	cm := map[complex128]int{}
	cm[complex(0, negZero())] = 1
	cm[0] = 2
	cm[complex(nan(), 0)] = 3
	cm[complex(nan(), 0)] = 4
	println("complex", len(cm), cm[0])
	// struct keys
	sk := map[key]int{}
	sk[key{1, "a", 0}] = 1
	sk[key{1, "a", negZero()}] = 2
	sk[key{1, "a", nan()}] = 3
	sk[key{1, "a", nan()}] = 4
	sk[key{2, "a\xff", 1}] = 5
	println("structkeys", len(sk), sk[key{1, "a", 0}], sk[key{2, "a\xff", 1}])
	// array keys
	ak := map[arrKey]string{}
	ak[arrKey{1, 2, 3}] = "x"
	ak[arrKey{1, 2, 3}] += "y"
	ak[arrKey{3, 2, 1}] = "z"
	println("arrkeys", len(ak), ak[arrKey{1, 2, 3}], ak[arrKey{}] == "")
	// interface keys
	im := map[any]int{}
	im[1] = 1
	im[int64(1)] = 2
	im[uint8(1)] = 3
	im[1.0] = 4
	im["1"] = 5
	im[key{a: 1}] = 6
	im[arrKey{1}] = 7
	im[true] = 8
	im[nil] = 9
	im[impl{1}] = 10
	println("ifkeys", len(im), im[1], im[int64(1)], im[1.0], im[nil], im[impl{1}], im[impl{2}])
	var nilIf ifKey
	im[nilIf] = 11
	println("nilif", len(im), im[nil])
	// unhashable dynamic types
	protect("slicekey", func() { im[[]int{1}] = 1 })
	protect("mapkey", func() { im[map[int]int{}] = 1 })
	protect("funckey", func() { im[func() {}] = 1 })
	protect("lookup-slice", func() { _ = im[[]byte("x")] })
	protect("delete-slice", func() { delete(im, []string{}) })
	protect("struct-with-slice", func() {
		type bad struct {
			a int
			b []int
		}
		im[bad{}] = 1
	})
	protect("arr-of-if", func() {
		im[[2]any{1, []int{}}] = 1
	})
	protect("named-slice-if", func() {
		mm := map[ifKey]int{}
		mm[sliceImpl{1, 2}] = 1
	})
	ik := map[ifKey]int{impl{3}: 3}
	println("ifkey2", ik[impl{3}], len(ik))
	// comparing interface keys holding NaN
	im2 := map[any]int{}
	im2[nan()] = 1
	im2[nan()] = 2
	println("nanif", len(im2))
	// string keys with invalid utf8 and nul bytes
	sm := map[string]int{"": 0, "\x00": 1, "\xff": 2, "\xef\xbf\xbd": 3, "a\x00b": 4, "a": 5}
	keys := make([]string, 0, len(sm))
	for k := range sm {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		println("strkey", strconv.Quote(k), sm[k])
	}
	_, ok = sm["�"]
	println("fffd-present", ok)
	// pointer keys: distinct allocations are distinct keys
	type node struct{ v int }
	pm := map[*node]int{}
	ptrs := make([]*node, 10)
	for i := range ptrs {
		ptrs[i] = &node{i}
		pm[ptrs[i]] = i * i
	}
	pm[nil] = -1
	total := 0
	for i, p := range ptrs {
		total += pm[p] * (i + 1)
	}
	println("ptrkeys", len(pm), total, pm[nil], pm[&node{3}])
	// channel keys
	ch1, ch2 := make(chan int), make(chan int)
	chm := map[chan int]int{ch1: 1, ch2: 2}
	chm[ch1] += 10
	println("chankeys", len(chm), chm[ch1], chm[ch2])
	// bool / rune / int8 extremes
	bm := map[int8]int{}
	for i := -128; i < 128; i++ {
		bm[int8(i)] += i
	}
	println("int8keys", len(bm), bm[-128], bm[127])
	um := map[uint64]int{math.MaxUint64: 1, 0: 2, 1 << 63: 3}
	println("u64", um[math.MaxUint64], um[1<<63], len(um))
}
