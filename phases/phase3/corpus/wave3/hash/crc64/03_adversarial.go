// hash/crc64 03: adversarial — sizes around gc's slicing-by-8 64-byte
// threshold, nonzero-crc Update chaining, 100KB bulk, Reset cycles.
package main

import (
	"bytes"
	"fmt"
	"hash/crc64"
)

type hash64 interface {
	Sum64() uint64
}

func main() {
	// Around gc's 64-byte slicing-by-8 entry threshold: the synthetic
	// always uses the simple update; values must match regardless.
	for _, n := range []int{63, 64, 65, 2047, 2048, 2049} {
		data := bytes.Repeat([]byte{0x5A}, n)
		tab := crc64.MakeTable(crc64.ECMA)
		h := crc64.New(tab)
		h.Write(data)
		fmt.Println(n, h.(hash64).Sum64() == crc64.Checksum(data, tab)) // true each
	}

	data := []byte("abcdef")
	tab := crc64.MakeTable(crc64.ISO)
	c1 := crc64.Update(0, tab, data[:3])
	c2 := crc64.Update(c1, tab, data[3:])
	fmt.Println(c2 == crc64.Checksum(data, tab)) // true

	big := bytes.Repeat([]byte("x"), 100000)
	h := crc64.New(tab)
	for i := 0; i < len(big); i += 999 {
		end := i + 999
		if end > len(big) {
			end = len(big)
		}
		h.Write(big[i:end])
	}
	fmt.Println(h.(hash64).Sum64() == crc64.Checksum(big, tab)) // true

	h2 := crc64.New(crc64.MakeTable(crc64.ECMA))
	for i := 0; i < 3; i++ {
		h2.Write([]byte("cycle"))
		h2.Reset()
	}
	h2.Write([]byte("cycle"))
	fmt.Println(h2.(hash64).Sum64() == crc64.Checksum([]byte("cycle"), crc64.MakeTable(crc64.ECMA))) // true
}
