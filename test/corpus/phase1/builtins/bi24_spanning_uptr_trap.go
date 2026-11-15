package main

// bi24: spanning unsafe.Pointer read — *(*unsafe.Pointer)(unsafe.Pointer(&b[0]))
// over []byte. Under gc this is defined behavior: the 8 bytes of b read
// little-endian as a pointer word (578437695752307201). Under graalgo the
// byte[] carrier holds one byte per element and has no flat memory behind
// it, so the read must TRAP (UnsafeEscapeException, exit 1) instead of
// returning the single byte as the word (1). DELIBERATE divergence from gc:
// the test testBi24SpanningUptrTrap asserts the trap, not parity.
// (fix-redmaster-r27: review F1 on 24d87358 — the r26 DerefNode guard
// covered int/float only, so this shape silently printed 1.)
import (
	"fmt"
	"unsafe"
)

func main() {
	b := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	p := *(*unsafe.Pointer)(unsafe.Pointer(&b[0]))
	fmt.Println("uptr-word:", uintptr(p))
}
