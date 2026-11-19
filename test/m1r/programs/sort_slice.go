package main

// sort.Slice, SliceStable and SliceIsSorted from upstream's source: they go
// through internal/reflectlite (ValueOf, Len, Swapper), which reads the
// slice's type descriptor and swaps elements by size class — through typed
// slice views, or unsafe_New and typedmemmove for the general case
// (toolexec W9a).

import "sort"

type person struct {
	name string
	age  int
}

type big struct {
	key  int
	pad  [5]int64
	name string
	tags []string
}

type pair struct {
	a, b int32
}

type wide struct {
	x, y, z float64
}

type named []int

type byLen []string

func (b byLen) Len() int           { return len(b) }
func (b byLen) Less(i, j int) bool { return len(b[i]) < len(b[j]) }
func (b byLen) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }

func lcg(seed *uint32) int {
	*seed = *seed*1664525 + 1013904223
	return int(*seed >> 8)
}

func try(name string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(error); ok {
				println(name, "panic:", e.Error())
			} else if s, ok := r.(string); ok {
				println(name, "panic:", s)
			} else {
				println(name, "panic")
			}
		}
	}()
	f()
}

func main() {
	seed := uint32(7)

	// Elements of every size class of reflectlite.Swapper.
	ints := make([]int, 40)
	for i := range ints {
		ints[i] = lcg(&seed) % 1000
	}
	sort.Slice(ints, func(i, j int) bool { return ints[i] < ints[j] })
	println(sort.SliceIsSorted(ints, func(i, j int) bool { return ints[i] < ints[j] }), ints[0], ints[1], ints[20], ints[39])

	strs := []string{"pear", "fig", "apple", "kiwi", "banana", "date", "cherry"}
	sort.Slice(strs, func(i, j int) bool { return strs[i] < strs[j] })
	for _, s := range strs {
		print(s, " ")
	}
	println()

	i64 := []int64{5, -3, 1 << 40, 0, -1 << 50, 7}
	sort.Slice(i64, func(i, j int) bool { return i64[i] < i64[j] })
	println(i64[0], i64[1], i64[2], i64[3], i64[4], i64[5])

	fl := []float64{2.5, -1.25, 1e300, 0, -1e-300, 3}
	sort.Slice(fl, func(i, j int) bool { return fl[i] < fl[j] })
	println(fl[0], fl[1], fl[2], fl[5])

	i32 := []int32{9, -2, 1 << 30, 0, -7}
	sort.Slice(i32, func(i, j int) bool { return i32[i] < i32[j] })
	println(i32[0], i32[1], i32[2], i32[3], i32[4])

	runes := []rune("gopher sorting")
	sort.Slice(runes, func(i, j int) bool { return runes[i] < runes[j] })
	println(string(runes))

	i16 := []int16{300, -300, 5, 0}
	sort.Slice(i16, func(i, j int) bool { return i16[i] < i16[j] })
	println(i16[0], i16[1], i16[2], i16[3])

	i8 := []int8{5, -128, 127, 0, -1}
	sort.Slice(i8, func(i, j int) bool { return i8[i] < i8[j] })
	println(i8[0], i8[1], i8[2], i8[3], i8[4])

	bs := []byte("the quick brown fox")
	sort.Slice(bs, func(i, j int) bool { return bs[i] < bs[j] })
	println(string(bs))

	bools := []bool{true, false, true, false, false}
	sort.Slice(bools, func(i, j int) bool { return !bools[i] && bools[j] })
	println(bools[0], bools[2], bools[3], bools[4])

	// Pointer-sized elements with pointers: pointers, maps, funcs, channels.
	ptrs := []*person{{"c", 3}, {"a", 1}, {"b", 2}}
	sort.Slice(ptrs, func(i, j int) bool { return ptrs[i].age < ptrs[j].age })
	println(ptrs[0].name, ptrs[1].name, ptrs[2].name)
	maps := []map[string]int{{"n": 3}, {"n": 1}, {"n": 2}}
	sort.Slice(maps, func(i, j int) bool { return maps[i]["n"] < maps[j]["n"] })
	println(maps[0]["n"], maps[1]["n"], maps[2]["n"])
	fns := []func() int{func() int { return 3 }, func() int { return 1 }, func() int { return 2 }}
	sort.Slice(fns, func(i, j int) bool { return fns[i]() < fns[j]() })
	println(fns[0](), fns[1](), fns[2]())

	// The general case: structs with strings and pointers, moved with typedmemmove.
	people := []person{{"Zoe", 31}, {"Al", 25}, {"Bo", 31}, {"Cy", 25}, {"Di", 40}}
	sort.Slice(people, func(i, j int) bool { return people[i].age < people[j].age })
	println(people[0].age, people[1].age, people[2].age, people[3].age, people[4].name)
	sort.SliceStable(people, func(i, j int) bool { return people[i].name < people[j].name })
	sort.SliceStable(people, func(i, j int) bool { return people[i].age < people[j].age })
	for _, p := range people {
		print(p.name, ":", p.age, " ")
	}
	println()

	bigs := make([]big, 25)
	for i := range bigs {
		k := lcg(&seed) % 100
		bigs[i] = big{key: k, pad: [5]int64{int64(k), 2, 3, 4, int64(-k)}, name: "n", tags: []string{"t"}}
	}
	sort.Slice(bigs, func(i, j int) bool { return bigs[i].key < bigs[j].key })
	ok := sort.SliceIsSorted(bigs, func(i, j int) bool { return bigs[i].key < bigs[j].key })
	for _, b := range bigs {
		ok = ok && b.pad[0] == int64(b.key) && b.pad[4] == int64(-b.key) && b.name == "n" && len(b.tags) == 1
	}
	println(ok, bigs[0].key <= bigs[24].key)

	wides := []wide{{3, 1, 2}, {1, 2, 3}, {2, 3, 1}}
	sort.Slice(wides, func(i, j int) bool { return wides[i].x < wides[j].x })
	println(wides[0].y, wides[1].y, wides[2].y)

	ifs := []any{3, "b", 1.5, nil, "a"}
	rank := func(v any) int {
		switch v.(type) {
		case nil:
			return 0
		case int:
			return 1
		case float64:
			return 2
		}
		return 3
	}
	sort.SliceStable(ifs, func(i, j int) bool { return rank(ifs[i]) < rank(ifs[j]) })
	println(ifs[0] == nil, ifs[1].(int), ifs[2].(float64), ifs[3].(string), ifs[4].(string))

	arrs := [][2]string{{"b", "1"}, {"a", "2"}, {"c", "0"}}
	sort.Slice(arrs, func(i, j int) bool { return arrs[i][0] < arrs[j][0] })
	println(arrs[0][1], arrs[1][1], arrs[2][1])

	// Defined slice types, sub-slices (the parent's other elements stay), empty and nil slices.
	n := named{5, 2, 8, 1}
	sort.Slice(n, func(i, j int) bool { return n[i] < n[j] })
	println(n[0], n[3])
	base := []int{9, 8, 7, 6, 5, 4, 3, 2, 1, 0}
	sub := base[3:7]
	sort.Slice(sub, func(i, j int) bool { return sub[i] < sub[j] })
	println(base[0], base[2], base[3], base[6], base[7], base[9])
	var nilSlice []string
	sort.Slice(nilSlice, func(i, j int) bool { return false })
	sort.SliceStable([]int{}, func(i, j int) bool { return false })
	println(sort.SliceIsSorted(nilSlice, func(i, j int) bool { return false }), len(nilSlice))

	// Not a slice: reflectlite's panic.
	try("not a slice", func() { sort.Slice(42, func(i, j int) bool { return false }) })
	try("nil", func() { sort.Slice(nil, func(i, j int) bool { return false }) })
	try("pointer to slice", func() { sort.Slice(&ints, func(i, j int) bool { return false }) })

	// The interface-based API next to it.
	words := byLen{"ccc", "a", "bb", "dddd", ""}
	sort.Sort(words)
	println(len(words[0]), words[1], words[4], sort.IsSorted(words))
	sort.Sort(sort.Reverse(sort.IntSlice(ints)))
	println(ints[0] >= ints[39], sort.SearchInts([]int{1, 3, 5, 7}, 5), sort.SearchStrings(strs, "kiwi"))
	sort.Stable(sort.StringSlice(strs))
	idx, found := sort.Find(len(strs), func(i int) int {
		if strs[i] == "date" {
			return 0
		} else if "date" < strs[i] {
			return -1
		}
		return 1
	})
	println(idx, found)
}
