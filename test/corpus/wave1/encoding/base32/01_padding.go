package main

import (
	"encoding/base32"
	"fmt"
)

func main() {
	src := []byte("padding test!!")

	// WithPadding: custom padding character.
	padded := base32.StdEncoding.WithPadding('*')
	s := padded.EncodeToString(src)
	fmt.Println(s)
	d, err := padded.DecodeString(s)
	fmt.Println(string(d), err == nil)

	// NoPadding.
	raw := base32.StdEncoding.WithPadding(base32.NoPadding)
	s2 := raw.EncodeToString(src)
	fmt.Println(s2)
	d2, err := raw.DecodeString(s2)
	fmt.Println(string(d2), err == nil)

	// WithPadding on a custom encoding (method chain on *Encoding).
	custom := base32.NewEncoding("0123456789abcdefghijklmnopqrstuv").WithPadding(base32.NoPadding)
	fmt.Println(custom.EncodeToString(src))

	// EncodedLen/DecodedLen edge cases.
	fmt.Println(base32.StdEncoding.EncodedLen(0), base32.StdEncoding.DecodedLen(0))
	fmt.Println(base32.StdEncoding.EncodedLen(1), base32.StdEncoding.DecodedLen(8))
}
