// 55_slice_bounds: slice/array/string indexing and slicing at their
// bounds, including the runtime-error panics (recovered and printed) for
// index, slice and 3-index slice violations, plus append/copy/len/cap
// arithmetic and aliasing.
package main

import "fmt"

func try(name string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println(name, "panic:", r)
		}
	}()
	f()
	fmt.Println(name, "ok")
}

func main() {
	s := []int{0, 1, 2, 3, 4}
	arr := [4]string{"a", "b", "c", "d"}
	str := "hello"
	i, j, k := 5, 2, 7
	try("idx-last", func() { fmt.Println(s[len(s)-1]) })
	try("idx-len", func() { fmt.Println(s[i]) })
	try("idx-neg", func() { n := -1; fmt.Println(s[n]) })
	try("arr-idx", func() { fmt.Println(arr[i-1]) })
	try("str-idx", func() { fmt.Println(str[i]) })
	try("slice-full", func() { fmt.Println(s[0:len(s)], s[len(s):]) })
	try("slice-hi", func() { fmt.Println(s[:i+1]) })
	try("slice-inv", func() { fmt.Println(s[3:j]) })
	try("slice-cap", func() { t := s[:2]; fmt.Println(t[:4], len(t), cap(t)) })
	try("slice3", func() { fmt.Println(s[1:3:4], len(s[1:3:4]), cap(s[1:3:4])) })
	try("slice3-bad", func() { fmt.Println(s[1:3:k]) })
	try("str-slice", func() { fmt.Println(str[1:4], str[i:]) })
	try("str-slice-bad", func() { fmt.Println(str[2:k]) })
	try("nil", func() { var n []int; fmt.Println(len(n), cap(n), n == nil, n[:0] == nil) })
	a := make([]int, 2, 3)
	b := append(a, 10)
	c := append(a, 20)
	fmt.Println("alias", a, b, c, len(b), cap(b))
	d := append(b, 30)
	d[0] = 99
	fmt.Println("grow", b, d, len(d))
	n := copy(s[1:], s)
	fmt.Println("copy", n, s)
	n = copy(s, s[:0])
	m := make([]byte, 3)
	n2 := copy(m, "hello")
	fmt.Println("copy2", n, n2, string(m))
	var grid [3][3]int
	for r := range grid {
		for c := range grid[r] {
			grid[r][c] = r*3 + c
		}
	}
	g2 := grid
	g2[1][1] = -1
	fmt.Println("arrays", grid, g2, grid == g2)
	try("zero-len-make", func() { x := make([]int, 0); fmt.Println(x[0]) })
}
