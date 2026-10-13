package main

import (
	"fmt"
	"unsafe"
)

// 10: Pointer identity (P3-M10 regression test).
// Distinct allocations compare unequal; a pointer keeps its
// unsafe.Pointer handle across *p = v writes; *T -> Pointer -> *T
// round trips preserve the referent (reads and writes); &arr[i]
// element pointers are distinct per index and stable per element.
func main() {
	// Distinct new(int) allocations: different pointers.
	a := new(int)
	b := new(int)
	pa := unsafe.Pointer(a)
	pb := unsafe.Pointer(b)
	fmt.Println(pa == pb)                    // false
	fmt.Println(pa == unsafe.Pointer(a))     // true — interning

	// Handle stability across writes.
	*a = 42
	fmt.Println(pa == unsafe.Pointer(a))     // true
	fmt.Println(*a)                          // 42

	// Round trip: (*T)(p) reads and writes the same storage.
	back := (*int)(pa)
	fmt.Println(unsafe.Pointer(back) == pa)  // true
	fmt.Println(*back)                       // 42
	*back = 7
	fmt.Println(*a)                          // 7 — write through the round trip

	// &x variables: distinct vars, distinct handles.
	x, y := 1, 1
	px, py := unsafe.Pointer(&x), unsafe.Pointer(&y)
	fmt.Println(px == py)                    // false
	fmt.Println(px == unsafe.Pointer(&x))    // true

	// Element pointers: distinct per index, stable per element.
	arr := []int{10, 20, 30}
	e0 := unsafe.Pointer(&arr[0])
	e1 := unsafe.Pointer(&arr[1])
	fmt.Println(e0 == e1)                     // false
	fmt.Println(e0 == unsafe.Pointer(&arr[0])) // true
	ep := (*int)(e0)
	fmt.Println(*ep)                          // 10
	*ep = 11
	fmt.Println(arr[0])                       // 11 — write reaches the array

	// new(map) is a non-nil pointer to a nil map.
	mp := unsafe.Pointer(new(map[string]int))
	fmt.Println(mp == nil)                    // false

	// Nil stays nil.
	var np *int
	fmt.Println(unsafe.Pointer(np) == nil)    // true
}
