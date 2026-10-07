package main

import (
	"encoding/asn1"
	"fmt"
)

func main() {
	// Marshal rejects unsupported types with StructuralError.
	_, err := asn1.Marshal(make(chan int))
	fmt.Println(err != nil)
	if se, ok := err.(*asn1.StructuralError); ok {
		fmt.Println(se.Msg != "")
	}

	// MarshalWithParams with a bad tag number.
	_, err = asn1.MarshalWithParams(1, "tag:notanumber")
	fmt.Println(err != nil)

	// Nested structs and slices.
	type inner struct {
		X int
	}
	type outer struct {
		I inner
		S []int
		B []byte
	}
	b, err := asn1.Marshal(outer{I: inner{X: 7}, S: []int{1, 2}, B: []byte("xy")})
	fmt.Println(err == nil, len(b) > 0)

	// Large integer (multi-byte length).
	big, err := asn1.Marshal([]int{1})
	fmt.Println(err == nil, len(big) > 0)

	// Boolean true/false.
	bt, _ := asn1.Marshal(true)
	bf, _ := asn1.Marshal(false)
	fmt.Println(len(bt) != len(bf) || string(bt) != string(bf))

	// BitString with zero length.
	zb := asn1.BitString{}
	fmt.Println(len(zb.RightAlign()) == 0)

	// ObjectIdentifier String of empty.
	fmt.Println(asn1.ObjectIdentifier{}.String() == "")
}
