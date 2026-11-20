// Targets range semantics: range over arrays evaluates a copy (mutations
// during the loop invisible), range over slices sees element mutations but
// fixed length, range over strings by rune with invalid UTF-8, range over
// maps with deletes during iteration (count only), range over int (typed,
// zero, negative), and per-iteration loop vars with goroutine-free closures.
package main

import (
	"sort"
	"strconv"
	"unicode/utf8"
)

type grid [3][2]int

//go:noinline
func neg() int { return -3 }

func main() {
	arr := [4]int{1, 2, 3, 4}
	for i, v := range arr {
		arr[3] = 100 // copy already taken
		if i == 3 {
			println("arr v", v, arr[3])
		}
	}
	pa := &arr
	arr[3] = 4
	for i, v := range pa {
		pa[3] = 200 // pointer: sees mutation
		if i == 3 {
			println("ptr v", v)
		}
	}

	sl := []int{1, 2, 3}
	n := 0
	for i, v := range sl {
		if i == 0 {
			sl[2] = 30
			sl = append(sl, 4, 5) // length fixed at start
		}
		n += v
	}
	println("slice sum", n, len(sl))

	// range over string with invalid bytes
	s := "a\xffé\xe2\x82z€"
	for i, r := range s {
		print(i, ":", r, " ")
	}
	println()
	println(len(s), utf8.RuneCountInString(s), utf8.ValidString(s))
	bs := []byte(s)
	rs := []rune(s)
	println(len(bs), len(rs), rs[1] == utf8.RuneError, string(rs[2]), string(rs) == s)
	for i := range "日本" {
		print(i, " ")
	}
	println()

	// map: delete during iteration is safe; count results deterministically
	m := map[int]int{}
	for i := range 20 {
		m[i] = i * i
	}
	visited := 0
	for k := range m {
		if k%2 == 0 {
			delete(m, k+1)
		}
		visited++
	}
	println(len(m) >= 10, visited >= 10, visited <= 20)
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	println(len(keys) == len(m), keys[0])
	clear(m)
	println(len(m))

	// range over int forms
	type myInt int16
	var total myInt
	for i := range myInt(300) {
		total += i
	}
	println(total)
	cnt := 0
	for range neg() {
		cnt++
	}
	for range 0 {
		cnt++
	}
	println("neg/zero iterations", cnt)
	var u8 uint8 = 255
	var last uint8
	iters := 0
	for i := range u8 {
		last = i
		iters++
	}
	println(last, iters)

	// range over nested arrays copies
	var g grid
	for i, row := range g {
		row[0] = i + 1 // copy
		g[i][1] = i * 10
	}
	println(g[0][0], g[1][1], g[2][1])
	for i := range g {
		for j := range g[i] {
			g[i][j]++
		}
	}
	println(g[0][0], g[2][1])

	// per-iteration semantics with modified loop variable and continue
	var fs []func() string
	for i := 0; i < 6; i++ {
		if i%2 == 0 {
			continue
		}
		fs = append(fs, func() string { return strconv.Itoa(i) })
		i++
	}
	for _, f := range fs {
		print(f(), " ")
	}
	println()

	// range with assignment to existing vars (not per-iteration)
	var ki, kv int
	ptrs := []*int{}
	for ki, kv = range []int{7, 8, 9} {
		ptrs = append(ptrs, &kv)
	}
	println(ki, kv, *ptrs[0], ptrs[0] == ptrs[2])

	// range over channel-free func-returned slice evaluated once
	calls := 0
	get := func() []int { calls++; return []int{1, 2, 3} }
	for range get() {
	}
	println(calls)

	// range over array pointer nil with only index is fine
	var np *[5]int
	c := 0
	for i := range np {
		c += i
	}
	println(c)
	// labeled range over string with break
	out := ""
scan:
	for _, r := range "abc-def" {
		switch r {
		case '-':
			break scan
		}
		out += string(r - 32)
	}
	println(out)
}
