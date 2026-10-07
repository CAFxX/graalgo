package main

import (
	"fmt"
	"unsafe"
)

func try(name string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("%s panics: %v\n", name, r)
		}
	}()
	f()
	fmt.Printf("%s ok\n", name)
}

func main() {
	p := unsafe.Pointer(new([4]int))
	n := -1
	try("slice-neglen", func() { _ = unsafe.Slice((*int)(p), n) })
	try("string-neglen", func() { _ = unsafe.String((*byte)(p), n) })
	try("string-nil-len0", func() { s := unsafe.String(nil, 0); fmt.Printf("string-nil-len0=%q\n", s) })
	sd := unsafe.StringData("")
	fmt.Println("stringdata-empty-nonil:", sd != nil)
	sd2 := unsafe.StringData("hi")
	fmt.Println("stringdata-hi-nonil:", sd2 != nil)
	try("add-nil", func() { _ = unsafe.Add(nil, 0) })
	type T struct {
		a int8
		b int64
		c string
	}
	const off = unsafe.Offsetof(T{}.b)
	fmt.Println("const-offsetof:", off)
	var arr2 [off]int
	fmt.Println("arraylen2:", len(arr2))
	fmt.Println("alignof:", unsafe.Alignof(T{}))
	x := new(int)
	p1 := unsafe.Pointer(x)
	p2 := unsafe.Pointer(x)
	fmt.Println("intern-eq:", p1 == p2)
	// SliceData(nil slice)
	var nilbs []byte
	fmt.Println("slicedata-nil:", unsafe.SliceData(nilbs) == nil)
}
