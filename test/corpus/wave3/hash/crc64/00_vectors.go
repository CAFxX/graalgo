// hash/crc64 00: published vectors for ECMA/ISO, table identity, Size.
package main

import (
	"fmt"
	"hash/crc64"
)

func main() {
	d := []byte("123456789")
	ecma := crc64.MakeTable(crc64.ECMA)
	iso := crc64.MakeTable(crc64.ISO)
	fmt.Printf("%016x\n", crc64.Checksum(d, ecma)) // 995dc9bbdf1939fa
	fmt.Printf("%016x\n", crc64.Checksum(d, iso))   // b90956c775a41001
	fmt.Printf("%016x\n", crc64.Checksum(nil, ecma)) // 0000000000000000
	// MakeTable caches the predefined tables (pointer identity).
	fmt.Println(crc64.MakeTable(crc64.ECMA) == ecma) // true
	fmt.Println(crc64.MakeTable(crc64.ISO) == iso)   // true
	// Custom polynomial: differential value (no published literal).
	fmt.Printf("%016x\n", crc64.Checksum(d, crc64.MakeTable(0xAD93D23594C935A9)))
	fmt.Println(crc64.Size) // 8
	h := crc64.New(ecma)
	fmt.Println(h.Size(), h.BlockSize()) // 8 1
}
