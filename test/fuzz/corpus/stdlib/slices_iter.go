// Targets range-over-func iterators in slices and maps: All, Values,
// Backward, Chunk (incl. n<=0 panic, early break), Collect, AppendSeq,
// Sorted, SortedFunc, SortedStableFunc, maps.Keys/Values/All feeding
// slices.Sorted, and hand-written iter.Seq/Seq2 adapters with early exit.
package main

import (
	"cmp"
	"iter"
	"maps"
	"slices"
	"strings"
)

func try(name string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(interface{ Error() string }); ok {
				println(name, "panic:", e.Error())
			} else if s, ok := r.(string); ok {
				println(name, "panic:", s)
			} else {
				println(name, "panic: other")
			}
		}
	}()
	f()
}

func filter[T any](s iter.Seq[T], keep func(T) bool) iter.Seq[T] {
	return func(yield func(T) bool) {
		for v := range s {
			if keep(v) && !yield(v) {
				return
			}
		}
	}
}

func mapSeq[T, U any](s iter.Seq[T], f func(T) U) iter.Seq[U] {
	return func(yield func(U) bool) {
		for v := range s {
			if !yield(f(v)) {
				return
			}
		}
	}
}

func take[T any](s iter.Seq[T], n int) iter.Seq[T] {
	return func(yield func(T) bool) {
		if n <= 0 {
			return
		}
		i := 0
		for v := range s {
			if !yield(v) {
				return
			}
			i++
			if i == n {
				return
			}
		}
	}
}

func naturals() iter.Seq[int] {
	return func(yield func(int) bool) {
		for i := 0; ; i++ {
			if !yield(i) {
				return
			}
		}
	}
}

func main() {
	s := []string{"d", "a", "c", "b", "e"}
	for i, v := range slices.All(s) {
		print(i, v, " ")
	}
	println()
	for i, v := range slices.Backward(s) {
		print(i, v, " ")
		if i == 2 {
			break
		}
	}
	println()
	for v := range slices.Values(s) {
		print(v)
	}
	println()
	println(strings.Join(slices.Sorted(slices.Values(s)), ""))
	println(strings.Join(slices.SortedFunc(slices.Values(s), func(a, b string) int { return strings.Compare(b, a) }), ""))
	words := []string{"bb", "a", "ccc", "dd", "e", "fff", "g"}
	println(strings.Join(slices.SortedStableFunc(slices.Values(words), func(a, b string) int { return cmp.Compare(len(a), len(b)) }), ","))
	col := slices.Collect(slices.Values([]int{}))
	println("collect-empty-nil", col == nil)
	ap := slices.AppendSeq([]int{100}, take(naturals(), 4))
	for _, v := range ap {
		print(v, " ")
	}
	println()

	nums := slices.Collect(take(naturals(), 23))
	for ch := range slices.Chunk(nums, 5) {
		print("[")
		for _, v := range ch {
			print(v, " ")
		}
		print("] len", len(ch), " cap", cap(ch), " ")
	}
	println()
	n := 0
	for ch := range slices.Chunk(nums, 4) {
		n += len(ch)
		if n >= 12 {
			break
		}
	}
	println("chunk-break", n)
	for range slices.Chunk([]int(nil), 3) {
		println("never")
	}
	try("chunk0", func() {
		for range slices.Chunk(nums, 0) {
		}
	})
	try("chunk-neg", func() { slices.Chunk(nums, -2) })

	sq := mapSeq(filter(naturals(), func(i int) bool { return i%3 == 0 }), func(i int) int { return i * i })
	var h uint64 = 14695981039346656037
	for v := range take(sq, 1000) {
		h = (h ^ uint64(v)) * 1099511628211
	}
	println("pipeline", h)
	strs := slices.Collect(mapSeq(take(naturals(), 6), func(i int) string { return strings.Repeat("x", i) }))
	println(strings.Join(strs, "|"))

	m := map[string]int{}
	for i := 0; i < 200; i++ {
		m[string(rune('A'+i%26))+string(rune('a'+i%7))] += i
	}
	keys := slices.Sorted(maps.Keys(m))
	vals := slices.Sorted(maps.Values(m))
	println(len(keys), keys[0], keys[len(keys)-1], vals[0], vals[len(vals)-1])
	h = 14695981039346656037
	for _, k := range keys {
		h = (h ^ uint64(m[k])) * 1099511628211
	}
	println("mapsum", h)
	m2 := maps.Collect(slices.All([]string{"x", "y", "z"}))
	println(len(m2), m2[0], m2[2])
	m3 := maps.Collect(maps.All(m))
	println(maps.Equal(m, m3))
	cnt := 0
	for k, v := range maps.All(m) {
		_, _ = k, v
		cnt++
		if cnt == 5 {
			break
		}
	}
	println("early", cnt)
	// pull-free zip of two Seqs via Collect
	a := slices.Collect(take(naturals(), 5))
	b := slices.Collect(slices.Values([]string{"v", "w", "x", "y", "z"}))
	for i := range a {
		print(a[i], b[i], " ")
	}
	println()
	// iterate Backward while modifying
	mod := []int{1, 2, 3, 4}
	for i, v := range slices.Backward(mod) {
		if i > 0 {
			mod[i-1] += v
		}
	}
	println(mod[0], mod[1], mod[2], mod[3])
}
