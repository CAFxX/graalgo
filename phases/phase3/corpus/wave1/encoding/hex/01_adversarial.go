package main

import (
	"encoding/hex"
	"fmt"
)

func main() {
	// Empty input.
	fmt.Println(hex.EncodeToString(nil))
	d, err := hex.DecodeString("")
	fmt.Println(len(d), err == nil)

	// Odd length -> ErrLength.
	_, err = hex.DecodeString("abc")
	fmt.Println(err == hex.ErrLength)
	fmt.Println(hex.ErrLength.Error())

	// Invalid byte -> InvalidByteError.
	_, err = hex.DecodeString("zz")
	fmt.Println(err != nil)
	if ibe, ok := err.(hex.InvalidByteError); ok {
		fmt.Println(ibe.Error())
	}

	// Invalid byte in the middle.
	_, err = hex.DecodeString("abzzcd")
	fmt.Println(err != nil)

	// Uppercase accepted.
	d2, err := hex.DecodeString("DEADBEEF")
	fmt.Println(len(d2), err == nil)

	// Decode into an exactly-sized buffer.
	dst := make([]byte, 2)
	n, err := hex.Decode(dst, []byte("abcd"))
	fmt.Println(n, err == nil)
}
