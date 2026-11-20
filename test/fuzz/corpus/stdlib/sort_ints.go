// Targets package sort on integer kinds of every width: sort.Sort over
// custom Interface types, sort.Ints, sort.Slice, IsSorted, Search and
// SearchInts on sorted/reverse/all-equal/organ-pipe/10k pseudo-random
// inputs; prints FNV checksums of the results plus a few samples.
package main

import "sort"

var h uint64 = 14695981039346656037

func mix(v uint64) {
	for i := 0; i < 8; i++ {
		h ^= v & 0xff
		h *= 1099511628211
		v >>= 8
	}
}

type lcg struct{ s uint64 }

func (l *lcg) next() uint64 {
	l.s = l.s*6364136223846793005 + 1442695040888963407
	return l.s >> 17
}

type i8s []int8

func (a i8s) Len() int           { return len(a) }
func (a i8s) Less(i, j int) bool { return a[i] < a[j] }
func (a i8s) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }

type u16s []uint16

func (a u16s) Len() int           { return len(a) }
func (a u16s) Less(i, j int) bool { return a[i] < a[j] }
func (a u16s) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }

type i32s []int32

func (a i32s) Len() int           { return len(a) }
func (a i32s) Less(i, j int) bool { return a[i] < a[j] }
func (a i32s) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }

type u64s []uint64

func (a u64s) Len() int           { return len(a) }
func (a u64s) Less(i, j int) bool { return a[i] < a[j] }
func (a u64s) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }

func shapes(n int, seed uint64) map[string][]int {
	r := &lcg{seed}
	m := map[string][]int{}
	var rnd, asc, desc, eq, pipe, saw, few []int
	for i := 0; i < n; i++ {
		rnd = append(rnd, int(int64(r.next())-1<<45))
		asc = append(asc, i*3-n)
		desc = append(desc, n-i*2)
		eq = append(eq, 42)
		if i < n/2 {
			pipe = append(pipe, i)
		} else {
			pipe = append(pipe, n-i)
		}
		saw = append(saw, i%17)
		few = append(few, int(r.next()%3)-1)
	}
	m["rnd"], m["asc"], m["desc"], m["eq"] = rnd, asc, desc, eq
	m["pipe"], m["saw"], m["few"] = pipe, saw, few
	return m
}

var order = []string{"rnd", "asc", "desc", "eq", "pipe", "saw", "few"}

func main() {
	for _, n := range []int{0, 1, 2, 5, 12, 13, 50, 257, 10000} {
		sh := shapes(n, uint64(n)*7+1)
		for _, k := range order {
			src := sh[k]
			a := append([]int(nil), src...)
			sort.Ints(a)
			h = 14695981039346656037
			for _, v := range a {
				mix(uint64(v))
			}
			ha := h
			b := append([]int(nil), src...)
			sort.Slice(b, func(i, j int) bool { return b[i] > b[j] })
			h = 14695981039346656037
			for _, v := range b {
				mix(uint64(v))
			}
			c8 := make(i8s, n)
			c16 := make(u16s, n)
			c32 := make(i32s, n)
			c64 := make(u64s, n)
			for i, v := range src {
				c8[i] = int8(v)
				c16[i] = uint16(v)
				c32[i] = int32(v)
				c64[i] = uint64(v)
			}
			sort.Sort(c8)
			sort.Sort(sort.Reverse(c16))
			sort.Sort(c32)
			sort.Stable(c64)
			for i := 0; i < n; i++ {
				mix(uint64(c8[i]))
				mix(uint64(c16[i]))
				mix(uint64(c32[i]))
				mix(c64[i])
			}
			ok := sort.IntsAreSorted(a) && sort.IsSorted(c8) && sort.IsSorted(sort.Reverse(c16)) && sort.IsSorted(c32) && sort.IsSorted(c64)
			srt := sort.IntsAreSorted(src)
			if n <= 13 || k == "rnd" {
				println(n, k, ha, h, ok, srt)
			} else {
				println(n, k, h, ok, srt)
			}
			if n > 0 {
				probes := []int{a[0] - 1, a[0], a[n/2], a[n-1], a[n-1] + 1, 0, 42}
				for _, p := range probes {
					i1 := sort.SearchInts(a, p)
					i2 := sort.Search(n, func(i int) bool { return a[i] > p })
					i3, found := sort.Find(n, func(i int) int {
						if p < a[i] {
							return -1
						} else if p > a[i] {
							return 1
						}
						return 0
					})
					print(i1, ",", i2, ",", i3, ",", found, " ")
				}
				println()
			}
		}
	}
	// small sample prints
	s := []int{5, -3, 9, 0, -3, 7, 2, 2, 100, -100}
	sort.Ints(s)
	for _, v := range s {
		print(v, " ")
	}
	println()
	sort.Sort(sort.Reverse(sort.IntSlice(s)))
	for _, v := range s {
		print(v, " ")
	}
	println()
	println(sort.SearchInts(nil, 3), sort.Search(0, func(int) bool { return true }), sort.Search(10, func(i int) bool { return false }))
	x8 := i8s{127, -128, 0, -1, 1, 127, -128}
	sort.Sort(x8)
	for _, v := range x8 {
		print(v, " ")
	}
	println()
	x64 := u64s{^uint64(0), 0, 1 << 63, 1<<63 - 1, 5}
	sort.Sort(x64)
	for _, v := range x64 {
		print(v, " ")
	}
	println()
}
