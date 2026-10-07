package main

import (
	"fmt"
	"unsafe"
)

func main() {
	// nil ptr, nonzero len
	func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Println("slice-nil-len5 panics:", r)
			}
		}()
		s := unsafe.Slice((*int)(nil), 5)
		fmt.Println("slice-nil-len5 ok:", s)
	}()
	func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Println("string-nil-len5 panics:", r)
			}
		}()
		s := unsafe.String(nil, 5)
		fmt.Println("string-nil-len5 ok:", s)
	}()
	// nil ptr, zero len
	s := unsafe.Slice((*int)(nil), 0)
	fmt.Println("slice-nil-len0:", s == nil)
	// unsafe.Pointer(nil) legality
	var p unsafe.Pointer = unsafe.Pointer(nil)
	fmt.Println("ptr-nil:", p == nil)
	// Sizeof operand not evaluated
	fmt.Println("sizeof-int:", unsafe.Sizeof(int(0)))
	const c = unsafe.Sizeof(struct {
		a int8
		b int64
	}{})
	fmt.Println("const-sizeof:", c)
	var arr [c]int
	fmt.Println("arraylen:", len(arr))
}
