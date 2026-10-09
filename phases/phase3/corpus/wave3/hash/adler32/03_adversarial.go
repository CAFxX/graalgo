// hash/adler32 03: adversarial — nmax (5552) chunking boundaries,
// large inputs, all-0xff/all-0x00 blocks, Sum idempotence.
package main

import (
	"bytes"
	"fmt"
	"hash/adler32"
)

func main() {
	// nmax boundary: 5551/5552/5553 bytes must agree with Checksum.
	for _, n := range []int{5551, 5552, 5553, 11104, 11105} {
		data := bytes.Repeat([]byte{0xAB}, n)
		h := adler32.New()
		// write in two halves to cross chunk boundaries mid-stream
		h.Write(data[:n/2])
		h.Write(data[n/2:])
		fmt.Println(n, h.Sum32() == adler32.Checksum(data)) // true each
	}

	// 1MB of zeros vs Checksum.
	big := bytes.Repeat([]byte{0}, 1<<20)
	h := adler32.New()
	const step = 65536
	for i := 0; i < len(big); i += step {
		h.Write(big[i : i+step])
	}
	fmt.Println(h.Sum32() == adler32.Checksum(big)) // true

	// All-0xff block.
	ff := bytes.Repeat([]byte{0xFF}, 1000)
	fmt.Printf("%08x\n", adler32.Checksum(ff))

	// Sum does not mutate: repeated Sum calls agree.
	h2 := adler32.New()
	h2.Write([]byte("idempotent"))
	s1 := fmt.Sprintf("%x", h2.Sum(nil))
	s2 := fmt.Sprintf("%x", h2.Sum(nil))
	fmt.Println(s1 == s2) // true

	// Interleaved Reset/Write cycles.
	h3 := adler32.New()
	for i := 0; i < 5; i++ {
		h3.Write([]byte("cycle"))
		h3.Reset()
	}
	h3.Write([]byte("cycle"))
	fmt.Println(h3.Sum32() == adler32.Checksum([]byte("cycle"))) // true
}
