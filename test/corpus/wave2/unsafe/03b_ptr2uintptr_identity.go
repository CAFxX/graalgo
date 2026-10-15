package main

import (
	"fmt"
	"unsafe"
)

// 03b: unsafe.Pointer → uintptr yields the handle as an integer identity
// token (PKG-007). D5 has no observable address (GG-0015) — the token is
// identity only, never a raw address, and it never traps. The portable
// properties asserted here hold under both gc and graalgo: nil → 0,
// stability (same referent → same token), distinctness, equality-as-identity
// (uintptr(p) == uintptr(q) iff p == q), and nil is the only zero token.
func main() {
	var nilptr uintptr = uintptr(unsafe.Pointer(nil))
	a := new(int)
	b := new(int)
	pa := unsafe.Pointer(a)
	u1 := uintptr(pa)
	u2 := uintptr(unsafe.Pointer(a)) // same referent, fresh conversion
	ub := uintptr(unsafe.Pointer(b)) // distinct allocation
	q := unsafe.Pointer(a)           // same referent again
	fmt.Println(nilptr == 0)         // nil → 0
	fmt.Println(u1 != 0)             // non-nil token is non-zero
	fmt.Println(u1 == u2)            // stability
	fmt.Println(u1 != ub)            // distinctness
	fmt.Println(nilptr < u1)         // nil is the only zero token
	fmt.Println(pa == q)             // handle == parity (interning)
	fmt.Println(uintptr(pa) == uintptr(q))
}
