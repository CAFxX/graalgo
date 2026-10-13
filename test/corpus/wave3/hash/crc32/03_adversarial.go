// hash/crc32 03: adversarial — 64KB/1MB inputs (slicing-by-8 threshold
// in gc; the synthetic uses the simple path — values must still agree),
// Update from nonzero crc, Reset cycles.
package main

import (
	"bytes"
	"fmt"
	"hash/crc32"
)

type hash32 interface {
	Sum32() uint32
}

func main() {
	// Sizes around gc's slicing-by-8 fast-path thresholds: the synthetic
	// always uses the simple update, values must match regardless.
	for _, n := range []int{63, 64, 65, 2047, 2048, 2049, 65536} {
		data := bytes.Repeat([]byte{0x5A}, n)
		h := crc32.NewIEEE()
		h.Write(data)
		fmt.Println(n, h.(hash32).Sum32() == crc32.ChecksumIEEE(data)) // true each
	}

	// Update from a nonzero starting crc chains correctly.
	data := []byte("abcdef")
	c1 := crc32.Update(0, crc32.IEEETable, data[:3])
	c2 := crc32.Update(c1, crc32.IEEETable, data[3:])
	fmt.Println(c2 == crc32.ChecksumIEEE(data)) // true

	// Castagnoli bulk vs one-shot.
	big := bytes.Repeat([]byte("x"), 100000)
	tab := crc32.MakeTable(crc32.Castagnoli)
	h := crc32.New(tab)
	for i := 0; i < len(big); i += 999 {
		end := i + 999
		if end > len(big) {
			end = len(big)
		}
		h.Write(big[i:end])
	}
	fmt.Println(h.(hash32).Sum32() == crc32.Checksum(big, tab)) // true

	// Reset cycles with alternating tables.
	h2 := crc32.New(crc32.MakeTable(crc32.Koopman))
	for i := 0; i < 3; i++ {
		h2.Write([]byte("cycle"))
		h2.Reset()
	}
	h2.Write([]byte("cycle"))
	fmt.Println(h2.(hash32).Sum32() == crc32.Checksum([]byte("cycle"), crc32.MakeTable(crc32.Koopman))) // true
}
