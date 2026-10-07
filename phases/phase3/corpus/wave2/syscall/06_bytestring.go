package main

import (
	"fmt"
	"syscall"
)

// sc06: ByteSliceFromString / BytePtrFromString validation rules and
// the NUL-terminated StringByteSlice shape.
func main() {
	b, err := syscall.ByteSliceFromString("abc")
	fmt.Println(err == nil, len(b) == 4, b[3] == 0, string(b[:3]) == "abc")
	_, err = syscall.ByteSliceFromString("a\x00b")
	fmt.Println(err == syscall.EINVAL)
	p, err := syscall.BytePtrFromString("xy")
	fmt.Println(err == nil, *p == 'x')
	_, err = syscall.BytePtrFromString("\x00")
	fmt.Println(err == syscall.EINVAL)
	s := syscall.StringByteSlice("zz")
	fmt.Println(len(s) == 3, s[2] == 0, string(s[:2]) == "zz")
}
