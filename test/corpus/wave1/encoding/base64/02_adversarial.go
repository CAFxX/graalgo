package main

import (
	"encoding/base64"
	"fmt"
)

func main() {
	// Empty input.
	fmt.Println(base64.StdEncoding.EncodeToString(nil))
	d, err := base64.StdEncoding.DecodeString("")
	fmt.Println(len(d), err == nil)

	// Corrupt input: bad character.
	_, err = base64.StdEncoding.DecodeString("!!!!")
	fmt.Println(err != nil)

	// CorruptInputError value.
	var cie base64.CorruptInputError = 5
	fmt.Println(cie.Error())

	// Wrong padding length.
	_, err = base64.StdEncoding.DecodeString("abc")
	fmt.Println(err != nil)

	// New line in input is rejected.
	_, err = base64.StdEncoding.DecodeString("aGVs\nbG8=")
	fmt.Println(err != nil)

	// Decode into an exactly-sized buffer.
	dst := make([]byte, 5)
	n, err := base64.StdEncoding.Decode(dst, []byte("aGVsbG8="))
	fmt.Println(n, err == nil, string(dst[:n]))
}
