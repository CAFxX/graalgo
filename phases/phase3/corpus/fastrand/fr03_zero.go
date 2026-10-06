// verifies: GG-0020
package main

import (
	"fmt"
	_ "unsafe"
)

//go:linkname fastrandn runtime.fastrandn
func fastrandn(n uint32) uint32

// P3-M4 remediation (P1-1): fastrandn(0) is defined — gc's randn has no
// positivity check and Lemire's multiply-shift with n=0 yields 0.
func main() {
	fmt.Println("fastrandn(0) =", fastrandn(0))
}
