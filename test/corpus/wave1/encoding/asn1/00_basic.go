package main

import (
	"encoding/asn1"
	"fmt"
)

func main() {
	// Marshal an integer (multi-result package func desugar).
	b, err := asn1.Marshal(42)
	fmt.Println(err == nil)
	fmt.Println(len(b) > 0)

	// Marshal a string.
	bs, err := asn1.Marshal("hello")
	fmt.Println(err == nil, len(bs) > 0)

	// Marshal a struct.
	type person struct {
		Name string
		Age  int
	}
	bp, err := asn1.Marshal(person{Name: "bob", Age: 30})
	fmt.Println(err == nil, len(bp) > 0)

	// MarshalWithParams (explicit tag).
	bt, err := asn1.MarshalWithParams(5, "tag:2")
	fmt.Println(err == nil, len(bt) > 0)

	// BitString helpers.
	bits := asn1.BitString{Bytes: []byte{0xa0}, BitLength: 3}
	fmt.Println(bits.At(0), bits.At(2))
	fmt.Println(len(bits.RightAlign()) > 0)

	// ObjectIdentifier.
	oid := asn1.ObjectIdentifier{1, 2, 840, 113549}
	fmt.Println(oid.String())
	fmt.Println(oid.Equal(asn1.ObjectIdentifier{1, 2, 840, 113549}))
	fmt.Println(oid.Equal(asn1.ObjectIdentifier{1, 2, 840}))

	// Enumerated and Flag marshal.
	be, err := asn1.Marshal(asn1.Enumerated(3))
	fmt.Println(err == nil, len(be) > 0)
	bf, err := asn1.Marshal(asn1.Flag(true))
	fmt.Println(err == nil, len(bf) > 0)

	// RawValue / RawContent / Null values.
	fmt.Println(asn1.NullRawValue.Tag, len(asn1.NullBytes))
	var rc asn1.RawContent = []byte{1, 2, 3}
	fmt.Println(len(rc))

	// Class/tag consts.
	fmt.Println(asn1.ClassUniversal, asn1.ClassApplication,
		asn1.ClassContextSpecific, asn1.ClassPrivate)
	fmt.Println(asn1.TagInteger, asn1.TagSequence, asn1.TagNull)

	// Error types.
	se := &asn1.StructuralError{Msg: "boom"}
	fmt.Println(se.Error())
	sye := &asn1.SyntaxError{Msg: "bad"}
	fmt.Println(sye.Error())
}
