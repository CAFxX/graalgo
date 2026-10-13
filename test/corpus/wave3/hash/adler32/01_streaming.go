// hash/adler32 01: streaming — chunking invariance (1-byte writes vs
// bulk vs odd splits), Reset, Sum/Sum32, Sum append semantics.
package main

import (
	"fmt"
	"hash/adler32"
)

func main() {
	data := []byte("The quick brown fox jumps over the lazy dog")

	// Reference: single Write.
	ref := adler32.New()
	ref.Write(data)
	fmt.Printf("%08x\n", ref.Sum32())

	// 1-byte-at-a-time writes agree.
	h1 := adler32.New()
	for _, b := range data {
		h1.Write([]byte{b})
	}
	fmt.Println(h1.Sum32() == ref.Sum32()) // true

	// Odd splits agree.
	h2 := adler32.New()
	for i := 0; i < len(data); {
		n := 1 + (i*7)%13
		if i+n > len(data) {
			n = len(data) - i
		}
		h2.Write(data[i : i+n])
		i += n
	}
	fmt.Println(h2.Sum32() == ref.Sum32()) // true

	// Sum appends big-endian to its argument without mutating state.
	s := ref.Sum([]byte("prefix:"))
	fmt.Printf("%x\n", s) // 7072656669783a + 8 hex digits of the checksum
	s2 := ref.Sum32()
	fmt.Println(s[7] == byte(s2>>24) && s[10] == byte(s2)) // true

	// Reset restores the initial state.
	ref.Reset()
	fmt.Printf("%08x\n", ref.Sum32()) // 00000001
	ref.Write([]byte("a"))
	fmt.Printf("%08x\n", ref.Sum32()) // 00620062

	// Write returns len(p), nil.
	n, err := ref.Write(data)
	fmt.Println(n == len(data), err == nil) // true true

	// Empty write is a no-op on the checksum.
	before := ref.Sum32()
	ref.Write(nil)
	fmt.Println(ref.Sum32() == before) // true
}
