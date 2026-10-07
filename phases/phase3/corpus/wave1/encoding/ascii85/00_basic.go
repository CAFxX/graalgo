package main

import (
	"encoding/ascii85"
	"fmt"
)

func main() {
	src := []byte("hello world, ascii85!")

	// MaxEncodedLen.
	fmt.Println(ascii85.MaxEncodedLen(len(src)))

	// Encode.
	dst := make([]byte, ascii85.MaxEncodedLen(len(src)))
	n := ascii85.Encode(dst, src)
	fmt.Println(n, string(dst[:n]))

	// Decode round-trip (3-result package func desugar).
	dec := make([]byte, len(src))
	ndst, nsrc, err := ascii85.Decode(dec, dst[:n], true)
	fmt.Println(ndst, nsrc, err == nil, string(dec[:ndst]))

	// Short source (not a multiple of 4) without flush.
	short := []byte("abc")
	enc := make([]byte, ascii85.MaxEncodedLen(len(short)))
	ne := ascii85.Encode(enc, short)
	fmt.Println(ne, string(enc[:ne]))
	dec2 := make([]byte, len(short))
	nd, ns, err := ascii85.Decode(dec2, enc[:ne], true)
	fmt.Println(nd, ns, err == nil, string(dec2[:nd]))

	// Empty.
	fmt.Println(ascii85.Encode(make([]byte, 10), nil))
	ndst2, nsrc2, err2 := ascii85.Decode(make([]byte, 10), nil, true)
	fmt.Println(ndst2, nsrc2, err2 == nil)
}
