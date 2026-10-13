package main

import (
	"encoding/base64"
	"fmt"
)

func main() {
	src := []byte("hello world")

	// Encode/Decode round-trip via strings.
	s := base64.StdEncoding.EncodeToString(src)
	fmt.Println(s)
	d, err := base64.StdEncoding.DecodeString(s)
	fmt.Println(string(d), err == nil)

	// URLEncoding.
	u := base64.URLEncoding.EncodeToString([]byte("\xfb\xff\xfe"))
	fmt.Println(u)

	// Raw (unpadded) variants.
	fmt.Println(base64.RawStdEncoding.EncodeToString(src))
	fmt.Println(base64.RawURLEncoding.EncodeToString(src))

	// Consts.
	fmt.Println(base64.StdPadding, base64.NoPadding)

	// Encode into a buffer.
	dst := make([]byte, base64.StdEncoding.EncodedLen(len(src)))
	base64.StdEncoding.Encode(dst, src)
	fmt.Println(string(dst))

	// Decode into a buffer (multi-result method desugar).
	dbuf := make([]byte, base64.StdEncoding.DecodedLen(len(dst)))
	n, err := base64.StdEncoding.Decode(dbuf, dst)
	fmt.Println(n, err == nil, string(dbuf[:n]))

	// Append variants.
	a := base64.StdEncoding.AppendEncode(nil, src)
	fmt.Println(string(a))
	ad, err := base64.StdEncoding.AppendDecode(nil, a)
	fmt.Println(string(ad), err == nil)
}
