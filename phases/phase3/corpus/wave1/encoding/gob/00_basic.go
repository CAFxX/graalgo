package main

import (
	"encoding/gob"
	"fmt"
)

type Point struct {
	X, Y int
	Name string
}

func main() {
	// In-scope constructors: NewBufferEncoder / NewBufferDecoder.
	// (gc's NewEncoder/NewDecoder take io.Writer/io.Reader; io is out
	// of M7 scope by design.)

	// Basic encode/decode round-trip.
	enc := gob.NewBufferEncoder()
	p := Point{X: 1, Y: 2, Name: "origin"}
	if err := enc.Encode(p); err != nil {
		fmt.Printf("Encode err: %v\n", err)
		return
	}
	data := enc.Bytes()
	fmt.Printf("Encoded %d bytes\n", len(data))

	var p2 Point
	dec := gob.NewBufferDecoder(data)
	if err := dec.Decode(&p2); err != nil {
		fmt.Printf("Decode err: %v\n", err)
		return
	}
	fmt.Printf("Decoded: %+v\n", p2)

	// Multiple values: scalar, string, slice.
	enc2 := gob.NewBufferEncoder()
	enc2.Encode(42)
	enc2.Encode("hello")
	enc2.Encode([]int{1, 2, 3})

	dec2 := gob.NewBufferDecoder(enc2.Bytes())
	var i int
	var s string
	var sl []int
	dec2.Decode(&i)
	dec2.Decode(&s)
	dec2.Decode(&sl)
	fmt.Printf("Multi: %d %q %v\n", i, s, sl)

	// Adversarial: empty struct, negative ints, empty string/slice.
	enc3 := gob.NewBufferEncoder()
	enc3.Encode(Point{})
	enc3.Encode(-12345)
	enc3.Encode("")
	enc3.Encode([]int{})
	dec3 := gob.NewBufferDecoder(enc3.Bytes())
	var p3 Point
	var n int
	var e string
	var z []int
	dec3.Decode(&p3)
	dec3.Decode(&n)
	dec3.Decode(&e)
	dec3.Decode(&z)
	fmt.Printf("Adversarial: %+v %d %q %v\n", p3, n, e, z)
}
