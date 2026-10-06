package main

import "fmt"

type P struct {
	x int
	y int64
}

func build8(n int) []int8 {
	var s []int8
	for i := 0; i < n; i++ {
		s = append(s, int8(i))
	}
	return s
}

func build64(n int) []int64 {
	var s []int64
	for i := 0; i < n; i++ {
		s = append(s, int64(i)*10000000000)
	}
	return s
}

func buildStr(n int) []string {
	var s []string
	for i := 0; i < n; i++ {
		s = append(s, "s")
	}
	return s
}

func buildP(n int) []P {
	var s []P
	for i := 0; i < n; i++ {
		s = append(s, P{x: i, y: int64(i)})
	}
	return s
}

func main() {
	// Growth across element kinds (slices escape via return: pure
	// growslice formula, deterministic).
	for _, n := range []int{4, 10} {
		b8 := build8(n)
		fmt.Println("int8", n, len(b8), cap(b8))
		b64 := build64(n)
		fmt.Println("int64", n, len(b64), cap(b64), b64[n-1])
		bs := buildStr(n)
		fmt.Println("string", n, len(bs), cap(bs))
		bp := buildP(n)
		fmt.Println("struct", n, len(bp), cap(bp), bp[n-1].x)
	}
}
