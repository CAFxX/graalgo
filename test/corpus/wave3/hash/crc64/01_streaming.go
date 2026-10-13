// hash/crc64 01: streaming — Update chaining, chunking invariance for
// ISO/ECMA/custom, Reset, Sum BE layout.
package main

import (
	"fmt"
	"hash/crc64"
)

type hash64 interface {
	Sum64() uint64
}

func main() {
	data := []byte("The quick brown fox jumps over the lazy dog")
	names := []string{"iso", "ecma", "custom"}
	tabs := map[string]*crc64.Table{
		"iso":    crc64.MakeTable(crc64.ISO),
		"ecma":   crc64.MakeTable(crc64.ECMA),
		"custom": crc64.MakeTable(0xAD93D23594C935A9),
	}
	for _, name := range names {
		tab := tabs[name]
		var crc uint64
		for i := 0; i < len(data); i += 7 {
			end := i + 7
			if end > len(data) {
				end = len(data)
			}
			crc = crc64.Update(crc, tab, data[i:end])
		}
		fmt.Println(name, crc == crc64.Checksum(data, tab)) // true x3

		h := crc64.New(tab)
		for _, b := range data {
			h.Write([]byte{b})
		}
		s64 := h.(hash64).Sum64()
		fmt.Println(name, s64 == crc64.Checksum(data, tab)) // true x3

		s := h.Sum([]byte{0xAA})
		be := uint64(s[1])<<56 | uint64(s[2])<<48 | uint64(s[3])<<40 | uint64(s[4])<<32 |
			uint64(s[5])<<24 | uint64(s[6])<<16 | uint64(s[7])<<8 | uint64(s[8])
		fmt.Println(name, len(s) == 9, s[0] == 0xAA, be == s64) // true x3
	}

	h := crc64.New(crc64.MakeTable(crc64.ECMA))
	h.Write([]byte("junk"))
	h.Reset()
	h.Write([]byte("123456789"))
	fmt.Printf("%016x\n", h.(hash64).Sum64()) // 995dc9bbdf1939fa

	e := crc64.New(crc64.MakeTable(crc64.ISO))
	n, err := e.Write(nil)
	fmt.Println(n == 0, err == nil, e.(hash64).Sum64() == 0) // true true true
}
