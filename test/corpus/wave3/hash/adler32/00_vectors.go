// hash/adler32 00: published test vectors (RFC 1950 / Wikipedia).
// Values verified against gc 1.27.1 before pinning.
package main

import (
	"fmt"
	"hash/adler32"
)

func main() {
	fmt.Printf("%08x\n", adler32.Checksum(nil))                  // 00000001
	fmt.Printf("%08x\n", adler32.Checksum([]byte("a")))           // 00620062
	fmt.Printf("%08x\n", adler32.Checksum([]byte("abc")))         // 024d0127
	fmt.Printf("%08x\n", adler32.Checksum([]byte("Wikipedia")))   // 11e60398
	fmt.Printf("%08x\n", adler32.Checksum([]byte("123456789")))   // 091e01de
	fmt.Println(adler32.Size)                                    // 4
	h := adler32.New()
	fmt.Println(h.Size(), h.BlockSize())                         // 4 4
}
