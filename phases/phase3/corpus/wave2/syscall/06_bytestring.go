package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

// sc06: ByteSliceFromString / BytePtrFromString validation rules and
// the NUL-terminated StringByteSlice shape.
func main() {
	b, err := syscall.ByteSliceFromString("abc")
	fmt.Println(err == nil, len(b) == 4, b[3] == 0, string(b[:3]) == "abc")
	_, err = syscall.ByteSliceFromString("a\x00b")
	fmt.Println(err == syscall.EINVAL)
	p, err := syscall.BytePtrFromString("xy")
	// *p is not expressible in frontend/v1 (WS D: StarExpr in expression
	// position is a scope error); read back through unsafe.Slice instead —
	// same data-integrity check on the returned pointer.
	fmt.Println(err == nil, unsafe.Slice(p, 2)[0] == 'x')
	_, err = syscall.BytePtrFromString("\x00")
	fmt.Println(err == syscall.EINVAL)
	s := syscall.StringByteSlice("zz")
	fmt.Println(len(s) == 3, s[2] == 0, string(s[:2]) == "zz")
}
