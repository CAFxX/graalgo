package main

// bi23: spanning carrier read — *(*int64)(unsafe.Pointer(&b[0])) over []byte.
// Under gc this is defined behavior: the 8 bytes of b read little-endian
// as int64 (578437695752307201). Under graalgo the byte[] carrier holds one
// byte per element and has no flat memory behind it, so the read must TRAP
// (UnsafeEscapeException, exit 1) instead of returning the single byte
// zero-extended (1). DELIBERATE divergence from gc: the test
// testBi23SpanningCarrierTrap asserts the trap, not parity.
import (
	"fmt"
	"unsafe"
)

func main() {
	b := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	fmt.Println(*(*int64)(unsafe.Pointer(&b[0])))
}
