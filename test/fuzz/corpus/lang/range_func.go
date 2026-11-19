// Targets Go 1.23 range-over-func: iter.Seq/Seq2, early break, continue,
// return from inside the loop body, defer inside the body (runs at function
// exit), panics propagating through the iterator, nested iterators, labeled
// break across nested iterator loops, and slices/maps iterator helpers.
package main

import (
	"iter"
	"maps"
	"slices"
	"strconv"
)

var log string

func count(n int) iter.Seq[int] {
	return func(yield func(int) bool) {
		defer func() { log += "c" + strconv.Itoa(n) + "done;" }()
		for i := 0; i < n; i++ {
			if !yield(i) {
				log += "stop@" + strconv.Itoa(i) + ";"
				return
			}
		}
	}
}

func pairs(s []string) iter.Seq2[int, string] {
	return func(yield func(int, string) bool) {
		for i, v := range s {
			if !yield(i, v) {
				return
			}
		}
	}
}

func fibs() iter.Seq[int] {
	return func(yield func(int) bool) {
		a, b := 0, 1
		for {
			if !yield(a) {
				return
			}
			a, b = b, a+b
		}
	}
}

func filter[T any](s iter.Seq[T], f func(T) bool) iter.Seq[T] {
	return func(yield func(T) bool) {
		for v := range s {
			if f(v) && !yield(v) {
				return
			}
		}
	}
}

func find(n, target int) (idx int) {
	for i := range count(n) {
		if i == target {
			return i * 10
		}
	}
	return -1
}

func deferInBody() (out string) {
	for i := range count(3) {
		defer func() { out += "d" + strconv.Itoa(i) }()
	}
	out = "body;"
	return
}

func panicThrough() (msg string) {
	defer func() {
		if r := recover(); r != nil {
			msg = "recovered:" + r.(string) + " log=" + log
		}
	}()
	for i := range count(5) {
		if i == 2 {
			panic("boom" + strconv.Itoa(i))
		}
	}
	return "nopanic"
}

func panicInIter() (msg string) {
	defer func() {
		if r := recover(); r != nil {
			msg = "recovered:" + r.(interface{ Error() string }).Error()
		}
	}()
	bad := func(yield func(int) bool) {
		var a []int
		yield(1)
		yield(a[3])
	}
	sum := 0
	for v := range bad {
		sum += v
	}
	return strconv.Itoa(sum)
}

func main() {
	log = ""
	s := 0
	for i := range count(5) {
		s += i
	}
	println(s, log)

	log = ""
	for i := range count(10) {
		if i == 3 {
			break
		}
		if i == 1 {
			continue
		}
		print(i, " ")
	}
	println(log)

	log = ""
	println(find(8, 4), find(3, 9), log)

	for i, v := range pairs([]string{"a", "b", "c"}) {
		print(i, v, ";")
	}
	println()

	n := 0
	for f := range fibs() {
		if f > 1000 {
			break
		}
		n += f
	}
	println(n)

	evens := filter(fibs(), func(v int) bool { return v%2 == 0 })
	got := []int{}
	for v := range evens {
		got = append(got, v)
		if len(got) == 6 {
			break
		}
	}
	for _, v := range got {
		print(v, " ")
	}
	println()

	log = ""
	println(deferInBody(), log)

	log = ""
	println(panicThrough())
	println(panicInIter())

	// nested iterators with labeled break/continue
	log = ""
	out := ""
outer:
	for i := range count(4) {
		for j := range count(4) {
			if j > i {
				continue outer
			}
			if i == 3 && j == 1 {
				break outer
			}
			out += strconv.Itoa(i) + strconv.Itoa(j) + " "
		}
	}
	println(out)
	println(log)

	// per-iteration variables in range-over-func captured by closures
	var fs []func() int
	for i := range count(3) {
		fs = append(fs, func() int { return i * i })
	}
	println(fs[0](), fs[1](), fs[2]())

	// stdlib iterators
	m := map[string]int{"z": 26, "a": 1, "m": 13}
	keys := slices.Sorted(maps.Keys(m))
	for _, k := range keys {
		print(k, "=", m[k], " ")
	}
	println()
	vals := slices.Collect(maps.Values(m))
	slices.Sort(vals)
	println(len(vals), vals[0], vals[2])
	for i, v := range slices.Backward([]int{1, 2, 3}) {
		print(i, ":", v, " ")
	}
	println()
	for c := range slices.Chunk([]int{1, 2, 3, 4, 5}, 2) {
		print(len(c), " ")
	}
	println()
	col := slices.Collect(filter(slices.Values([]int{5, 8, 2, 9, 4}), func(v int) bool { return v > 4 }))
	println(len(col), col[0], col[1], col[2])

	// range over int with typed bound
	type small int8
	var cnt small
	for k := range small(5) {
		cnt += k
	}
	println(cnt)
	for range 3 {
		print("x")
	}
	println()
}
