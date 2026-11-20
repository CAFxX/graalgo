// Targets the slices/sort packages over aliasing-sensitive operations:
// Insert/Delete/Replace (tail zeroing), Compact, Grow/Clip caps, stable vs
// unstable sorting results, BinarySearch, Concat, Repeat, Chunk, Index panics.
package main

import (
	"cmp"
	"slices"
	"sort"
	"strconv"
	"strings"
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

func dump(name string, s []int) {
	print(name, " len=", len(s), " cap=", cap(s), " [")
	for i, v := range s {
		if i > 0 {
			print(" ")
		}
		print(v)
	}
	println("]")
}

type person struct {
	name string
	age  int
}

func main() {
	s := []int{1, 2, 3, 4, 5, 6}
	full := s[:cap(s)]
	d := slices.Delete(s, 1, 3)
	dump("delete", d)
	dump("deletefull", full) // tail zeroed
	s = []int{1, 2, 3}
	ins := slices.Insert(s, 1, 10, 11)
	dump("insert", ins)
	dump("insertorig", s)
	s2 := make([]int, 3, 10)
	copy(s2, []int{7, 8, 9})
	ins2 := slices.Insert(s2, 3, 1, 2)
	dump("insertcap", ins2)
	s2[0] = -1
	println("insertshare", ins2[0])
	protect("insert-oob", func() { slices.Insert([]int{1}, 3, 5) })
	protect("delete-oob", func() { slices.Delete([]int{1, 2}, 1, 3) })
	protect("delete-inv", func() { slices.Delete([]int{1, 2}, 2, 1) })
	rp := []int{1, 2, 3, 4, 5}
	rp2 := slices.Replace(rp, 1, 4, 9)
	dump("replace", rp2)
	dump("replacefull", rp[:5])
	c := []int{1, 1, 2, 2, 2, 3, 1, 1}
	cfull := c[:cap(c)]
	cc := slices.Compact(c)
	dump("compact", cc)
	dump("compactfull", cfull)
	cf := slices.CompactFunc([]int{1, 3, 5, 2, 4, 7}, func(a, b int) bool { return a%2 == b%2 })
	dump("compactfunc", cf)
	g := slices.Grow([]int{1}, 10)
	println("grow", len(g), cap(g) >= 11)
	cl := slices.Clip(make([]int, 2, 9))
	println("clip", len(cl), cap(cl))
	protect("grow-neg", func() { slices.Grow([]int{}, -1) })
	// sorting
	people := []person{{"al", 30}, {"bo", 25}, {"cy", 30}, {"di", 25}, {"ed", 40}, {"fi", 30}, {"gu", 25}}
	st := slices.Clone(people)
	slices.SortStableFunc(st, func(a, b person) int { return cmp.Compare(a.age, b.age) })
	for _, p := range st {
		print(p.name, p.age, " ")
	}
	println()
	st2 := slices.Clone(people)
	sort.SliceStable(st2, func(i, j int) bool { return st2[i].age > st2[j].age })
	for _, p := range st2 {
		print(p.name, p.age, " ")
	}
	println()
	// fully-ordered sort (deterministic regardless of algorithm)
	nums := make([]int, 500)
	for i := range nums {
		nums[i] = (i * 7919) % 503
	}
	slices.Sort(nums)
	println("sorted", slices.IsSorted(nums), nums[0], nums[250], nums[499])
	strs := strings.Fields("pear apple Fig banana \xff apple Éclair éclair 10 9 100")
	sort.Strings(strs)
	println("strsort", strings.Join(strs, "|"))
	slices.SortFunc(strs, func(a, b string) int {
		return cmp.Or(cmp.Compare(len(a), len(b)), strings.Compare(a, b))
	})
	println("lensort", strings.Join(strs, "|"))
	fl := []float64{3, -0.0, 0, 1, -1, 2.5}
	slices.Sort(fl)
	for _, f := range fl {
		print(strconv.FormatFloat(f, 'g', -1, 64), " ")
	}
	println()
	// BinarySearch
	for _, x := range []int{-1, 0, 250, 251, 502, 503} {
		i, ok := slices.BinarySearch(nums, x)
		j := sort.SearchInts(nums, x)
		println("bsearch", x, i, ok, j)
	}
	// Concat, Repeat, Chunk
	cat := slices.Concat([]int{1}, nil, []int{2, 3}, []int{})
	dump("concat", cat)
	println("concatnil", slices.Concat[[]int]() == nil)
	rep := slices.Repeat([]int{1, 2}, 3)
	dump("repeat", rep)
	protect("repeat-neg", func() { slices.Repeat([]int{1}, -1) })
	for ch := range slices.Chunk([]int{1, 2, 3, 4, 5, 6, 7}, 3) {
		dump("chunk", ch)
	}
	protect("chunk-0", func() {
		for range slices.Chunk([]int{1}, 0) {
		}
	})
	// Index/Contains/Equal/Min/Max/Reverse
	println("index", slices.Index(nums, 250), slices.Contains(nums, 503), slices.Equal([]int{}, nil))
	println("minmax", slices.Min(nums), slices.Max(nums))
	protect("min-empty", func() { slices.Min([]int{}) })
	r := []int{1, 2, 3, 4}
	slices.Reverse(r)
	dump("reverse", r)
	println("cmp", slices.Compare([]int{1, 2}, []int{1, 2, 0}), slices.Compare([]int{2}, []int{1, 9}))
	// sorted keys from iterators
	vals := slices.Collect(slices.Values([]string{"c", "a", "b"}))
	println("collect", strings.Join(slices.Sorted(slices.Values(vals)), ""), len(vals))
	bw := slices.Backward([]int{1, 2, 3})
	for i, v := range bw {
		print(i, "=", v, " ")
	}
	println()
	// sort.Sort on a custom type, ensuring Less/Swap through interface
	bs := byLen{"ccc", "a", "bb", "dddd", ""}
	sort.Sort(bs)
	println("bylen", strings.Join(bs, ","), sort.IsSorted(bs))
	idx := sort.Search(100, func(i int) bool { return i*i >= 50 })
	println("search", idx)
}

type byLen []string

func (b byLen) Len() int           { return len(b) }
func (b byLen) Less(i, j int) bool { return len(b[i]) < len(b[j]) }
func (b byLen) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }
