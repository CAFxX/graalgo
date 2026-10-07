package main

import (
	"encoding/ascii85"
	"fmt"
)

func main() {
	// Corrupt input: character out of range.
	dst := make([]byte, 100)
	_, _, err := ascii85.Decode(dst, []byte("v!!!!"), true)
	fmt.Println(err != nil)

	// CorruptInputError value.
	var cie ascii85.CorruptInputError = 2
	fmt.Println(cie.Error())

	// 'z' is valid (zero 4-byte group).
	enc := make([]byte, ascii85.MaxEncodedLen(4))
	n := ascii85.Encode(enc, []byte{0, 0, 0, 0})
	fmt.Println(n, string(enc[:n]))
	dec := make([]byte, 4)
	nd, ns, err := ascii85.Decode(dec, enc[:n], true)
	fmt.Println(nd, ns, err == nil)

	// 'y' is valid (space 4-byte group).
	n2 := ascii85.Encode(enc, []byte{' ', ' ', ' ', ' '})
	fmt.Println(n2, string(enc[:n2]))

	// Decode without flush: trailing partial group ignored.
	nd3, ns3, err3 := ascii85.Decode(dec, []byte("9jqo^"), false)
	fmt.Println(nd3, ns3, err3 == nil)

	// Overflow: 5 chars decoding to > 0xffffffff.
	_, _, err = ascii85.Decode(dst, []byte("s8W-!"), true)
	fmt.Println(err == nil)
	_, _, err = ascii85.Decode(dst, []byte("s8W-\""), true)
	fmt.Println(err != nil)
}
