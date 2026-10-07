// subtle.XORBytes vs gc.
package main

import (
	"bytes"
	"crypto/subtle"
	"fmt"
	"os"
)

func main() {
	failed := false
	a := bytes.Repeat([]byte{0x0f}, 64)
	b := bytes.Repeat([]byte{0xf0}, 64)
	dst := make([]byte, 64)
	n := subtle.XORBytes(dst, a, b)
	if n != 64 {
		fmt.Println("FAIL n")
		failed = true
	}
	for i := range dst {
		if dst[i] != 0xff {
			fmt.Println("FAIL xor")
			failed = true
			break
		}
	}
	// Panic on short dst
	func() {
		defer func() {
			if recover() == nil {
				fmt.Println("FAIL no-panic-short")
				failed = true
			}
		}()
		subtle.XORBytes(make([]byte, 4), a, b)
	}()
	if failed {
		os.Exit(1)
	}
	fmt.Println("PASS")
}
