// hash/crc32 00: published vectors for IEEE/Castagnoli/Koopman, table
// identity, Size.
package main

import (
	"fmt"
	"hash/crc32"
)

func main() {
	d := []byte("123456789")
	fmt.Printf("%08x\n", crc32.ChecksumIEEE(d)) // cbf43926
	fmt.Printf("%08x\n", crc32.Checksum(d, crc32.MakeTable(crc32.IEEE)))
	fmt.Printf("%08x\n", crc32.Checksum(d, crc32.MakeTable(crc32.Castagnoli))) // e3069283
	fmt.Printf("%08x\n", crc32.Checksum(d, crc32.MakeTable(crc32.Koopman))) // 2d3dd0ae
	fmt.Printf("%08x\n", crc32.Checksum(nil, crc32.IEEETable))   // 00000000
	fmt.Printf("%08x\n", crc32.ChecksumIEEE([]byte("a")))        // e8b7be43
	// MakeTable(IEEE) returns the exported IEEETable (pointer identity).
	fmt.Println(crc32.MakeTable(crc32.IEEE) == crc32.IEEETable) // true
	// Custom polynomial: differential value (no published literal).
	fmt.Printf("%08x\n", crc32.Checksum(d, crc32.MakeTable(0x3D65B9AF)))
	fmt.Println(crc32.Size) // 4
	h := crc32.NewIEEE()
	fmt.Println(h.Size(), h.BlockSize()) // 4 1
}
