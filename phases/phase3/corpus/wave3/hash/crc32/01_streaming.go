// hash/crc32 01: streaming — Update chaining equivalence, chunking
// invariance across polynomials, Reset, Sum BE layout.
package main

import (
	"fmt"
	"hash/crc32"
)

// hash32 is the Sum32 half of hash.Hash32, named (not anonymous) so the
// type assertion goes through the well-tested named-interface path.
type hash32 interface {
	Sum32() uint32
}

func main() {
	data := []byte("The quick brown fox jumps over the lazy dog")
	tabs := map[string]*crc32.Table{
		"ieee":   crc32.IEEETable,
		"cast":   crc32.MakeTable(crc32.Castagnoli),
		"koop":   crc32.MakeTable(crc32.Koopman),
		"custom": crc32.MakeTable(0x1EDC6F41),
	}
	names := []string{"ieee", "cast", "koop", "custom"}
	for _, name := range names {
		tab := tabs[name]
		// Update chaining == one-shot Checksum.
		var crc uint32
		for i := 0; i < len(data); i += 7 {
			end := i + 7
			if end > len(data) {
				end = len(data)
			}
			crc = crc32.Update(crc, tab, data[i:end])
		}
		fmt.Println(name, crc == crc32.Checksum(data, tab)) // true x4

		// Digest Write chunking == Checksum.
		h := crc32.New(tab)
		for _, b := range data {
			h.Write([]byte{b})
		}
		s32 := h.(hash32).Sum32()
		fmt.Println(name, s32 == crc32.Checksum(data, tab)) // true x4

		// Sum lays out big-endian and does not mutate.
		s := h.Sum([]byte{0xAA})
		be := uint32(s[1])<<24 | uint32(s[2])<<16 | uint32(s[3])<<8 | uint32(s[4])
		fmt.Println(name, len(s) == 5, s[0] == 0xAA, be == s32) // true x4
	}

	// Reset restores the zero state.
	h := crc32.NewIEEE()
	h.Write([]byte("junk"))
	h.Reset()
	h.Write([]byte("123456789"))
	fmt.Printf("%08x\n", h.(hash32).Sum32()) // cbf43926

	// NewIEEE Sum of empty is zero; Write returns len(p), nil.
	e := crc32.NewIEEE()
	n, err := e.Write(nil)
	fmt.Println(n == 0, err == nil, e.(hash32).Sum32() == 0) // true true true
}
