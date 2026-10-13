package main

import (
	"encoding/base32"
	"fmt"
)

func main() {
	src := []byte("hello world")

	// Encode/Decode round-trip via strings.
	s := base32.StdEncoding.EncodeToString(src)
	fmt.Println(s)
	d, err := base32.StdEncoding.DecodeString(s)
	fmt.Println(string(d), err == nil)

	// HexEncoding.
	fmt.Println(base32.HexEncoding.EncodeToString(src))

	// Consts.
	fmt.Println(base32.StdPadding, base32.NoPadding)

	// Encode into a buffer.
	dst := make([]byte, base32.StdEncoding.EncodedLen(len(src)))
	base32.StdEncoding.Encode(dst, src)
	fmt.Println(string(dst))

	// Decode into a buffer (multi-result method desugar).
	dbuf := make([]byte, base32.StdEncoding.DecodedLen(len(dst)))
	n, err := base32.StdEncoding.Decode(dbuf, dst)
	fmt.Println(n, err == nil, string(dbuf[:n]))

	// Append variants.
	a := base32.StdEncoding.AppendEncode(nil, src)
	fmt.Println(string(a))
	ad, err := base32.StdEncoding.AppendDecode(nil, a)
	fmt.Println(string(ad), err == nil)

	// NewEncoding with a custom alphabet.
	custom := base32.NewEncoding("abcdefghijklmnopqrstuvwxyz012345")
	fmt.Println(custom.EncodeToString(src))
}
