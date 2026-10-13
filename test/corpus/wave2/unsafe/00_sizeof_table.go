package main

import (
	"fmt"
	"unsafe"
)

// 00: Sizeof/Alignof table — frontend constant-fold intrinsics.
// Covers padding structs, arrays, strings, slices, maps, chans, funcs,
// pointers, interfaces; const-position and array-length (pre-pass)
// occurrences alongside runtime folds. Deterministic: integers only.
type pad struct {
	a int8
	b int64
	c int8
}

const cSize = unsafe.Sizeof(int(0))

func main() {
	fmt.Println(unsafe.Sizeof(int(0)))            // 8
	fmt.Println(unsafe.Alignof(int(0)))           // 8
	fmt.Println(unsafe.Sizeof(pad{}))            // 24
	fmt.Println(unsafe.Alignof(pad{}))            // 8
	fmt.Println(unsafe.Sizeof([3]int64{}))       // 24
	fmt.Println(unsafe.Sizeof(""))                // 16
	fmt.Println(unsafe.Sizeof([]byte(nil)))      // 24
	fmt.Println(unsafe.Sizeof(map[string]int(nil))) // 8
	fmt.Println(unsafe.Sizeof((chan int)(nil)))   // 8
	fmt.Println(unsafe.Sizeof((func())(nil)))     // 8
	fmt.Println(unsafe.Sizeof((*int)(nil)))      // 8
	fmt.Println(unsafe.Sizeof(any(nil)))          // 16
	fmt.Println(unsafe.Alignof(struct {
		x int8
		y string
	}{})) // 8
	fmt.Println(cSize) // 8 — const position (pre-pass rewrite)
	var arr [unsafe.Sizeof(int64(0))]int
	fmt.Println(len(arr)) // 8 — array length (pre-pass rewrite)
}
