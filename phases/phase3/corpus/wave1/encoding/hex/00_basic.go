package main

import (
	"encoding/hex"
	"fmt"
)

func main() {
	src := []byte("hello")

	// Encode/Decode round-trip.
	s := hex.EncodeToString(src)
	fmt.Println(s)
	d, err := hex.DecodeString(s)
	fmt.Println(string(d), err == nil)

	// Encode into a buffer.
	dst := make([]byte, hex.EncodedLen(len(src)))
	n := hex.Encode(dst, src)
	fmt.Println(n, string(dst))

	// Decode into a buffer (multi-result package func desugar).
	dbuf := make([]byte, hex.DecodedLen(len(dst)))
	n2, err := hex.Decode(dbuf, dst)
	fmt.Println(n2, err == nil, string(dbuf[:n2]))

	// Append variants.
	a := hex.AppendEncode(nil, src)
	fmt.Println(string(a))
	ad, err := hex.AppendDecode(nil, a)
	fmt.Println(string(ad), err == nil)

	// Dump.
	fmt.Print(hex.Dump([]byte("hello world, this is a hex dump test")))

	// Length helpers.
	fmt.Println(hex.EncodedLen(0), hex.DecodedLen(0))
	fmt.Println(hex.EncodedLen(3), hex.DecodedLen(6))
}
