package main

import (
	"bytes"
	"encoding/gob"
	"fmt"
)

type Point struct {
	X, Y int
	Name string
}

func main() {
	// Basic encode/decode
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	p := Point{X: 1, Y: 2, Name: "origin"}
	if err := enc.Encode(p); err != nil {
		fmt.Printf("Encode err: %v\n", err)
		return
	}
	fmt.Printf("Encoded %d bytes\n", buf.Len())

	var p2 Point
	dec := gob.NewDecoder(&buf)
	if err := dec.Decode(&p2); err != nil {
		fmt.Printf("Decode err: %v\n", err)
		return
	}
	fmt.Printf("Decoded: %+v\n", p2)

	// Multiple values
	var buf2 bytes.Buffer
	enc2 := gob.NewEncoder(&buf2)
	enc2.Encode(42)
	enc2.Encode("hello")
	enc2.Encode([]int{1, 2, 3})

	dec2 := gob.NewDecoder(&buf2)
	var i int
	var s string
	var sl []int
	dec2.Decode(&i)
	dec2.Decode(&s)
	dec2.Decode(&sl)
	fmt.Printf("Multi: %d %q %v\n", i, s, sl)
}
