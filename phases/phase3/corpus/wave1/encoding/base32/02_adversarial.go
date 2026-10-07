package main

import (
	"encoding/base32"
	"fmt"
)

func main() {
	// Empty input.
	fmt.Println(base32.StdEncoding.EncodeToString(nil))
	d, err := base32.StdEncoding.DecodeString("")
	fmt.Println(len(d), err == nil)

	// Corrupt input: bad character.
	_, err = base32.StdEncoding.DecodeString("!!!!!!!!")
	fmt.Println(err != nil)

	// Corrupt input: bad padding position.
	_, err = base32.StdEncoding.DecodeString("MY======")
	fmt.Println(err != nil)

	// CorruptInputError value.
	var cie base32.CorruptInputError = 3
	fmt.Println(cie.Error())

	// Decode into a too-small buffer (short write): the synthetic
	// returns what fits without overrunning.
	dst := make([]byte, 8)
	n, err := base32.StdEncoding.Decode(dst, []byte("JBSWY3DPEE"))
	fmt.Println(n, err == nil)

	// Trailing garbage after valid data.
	_, err = base32.StdEncoding.DecodeString("JBSWY3DPEE!!!")
	fmt.Println(err != nil)
}
