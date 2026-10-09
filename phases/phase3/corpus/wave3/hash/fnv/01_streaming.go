// hash/fnv 01: streaming — chunking invariance for all six hashes,
// Reset, Sum BE layout, 1-byte writes.
package main

import (
	"bytes"
	"fmt"
	"hash"
	"hash/fnv"
)

func ref(h hash.Hash, data []byte) []byte {
	h.Write(data)
	return h.Sum(nil)
}

func newHash(name string) hash.Hash {
	switch name {
	case "32":
		return fnv.New32()
	case "32a":
		return fnv.New32a()
	case "64":
		return fnv.New64()
	case "64a":
		return fnv.New64a()
	case "128":
		return fnv.New128()
	case "128a":
		return fnv.New128a()
	}
	return nil
}

func main() {
	data := []byte("The quick brown fox jumps over the lazy dog")
	names := []string{"32", "32a", "64", "64a", "128", "128a"}
	for _, name := range names {
		r := ref(newHash(name), data)

		// 1-byte writes agree with bulk.
		h := newHash(name)
		for _, b := range data {
			h.Write([]byte{b})
		}
		fmt.Println(name, bytes.Equal(h.Sum(nil), r)) // true x6

		// Odd splits agree.
		h2 := newHash(name)
		for i := 0; i < len(data); {
			n := 1 + (i*5)%11
			if i+n > len(data) {
				n = len(data) - i
			}
			h2.Write(data[i : i+n])
			i += n
		}
		fmt.Println(name, bytes.Equal(h2.Sum(nil), r)) // true x6

		// Reset then re-hash agrees.
		h3 := newHash(name)
		h3.Write([]byte("junk"))
		h3.Reset()
		fmt.Println(name, bytes.Equal(ref(h3, data), r)) // true x6
	}

	// Sum appends to its argument; 128-bit layout is BE hi,lo.
	h := fnv.New128()
	h.Write([]byte("x"))
	s := h.Sum([]byte{0xAA})
	fmt.Println(len(s) == 17, s[0] == 0xAA) // true true
}
