package main

import (
	"fmt"
	"unsafe"
)

// 01: Offsetof — field offsets incl. embedded structs and trailing fields;
// const-position and array-length occurrences (pre-pass); unexported field
// from the same package.
type inner struct {
	a int8
	b int64
}

type outer struct {
	inner
	c int8
	d string
	e int64
}

const cOff = unsafe.Offsetof(outer{}.b)

func main() {
	fmt.Println(unsafe.Offsetof(outer{}.a)) // 0
	fmt.Println(unsafe.Offsetof(outer{}.b)) // 8 (through embedded inner)
	fmt.Println(unsafe.Offsetof(outer{}.c)) // 16
	fmt.Println(unsafe.Offsetof(outer{}.d)) // 24
	fmt.Println(unsafe.Offsetof(outer{}.e)) // 40
	fmt.Println(cOff)                       // 8 — const position
	var arr [unsafe.Offsetof(outer{}.d)]byte
	fmt.Println(len(arr)) // 24 — array length
}
