package main

import (
	"bytes"
	"fmt"
)

func main() {
	// NewReader / Len / Size
	r := bytes.NewReader([]byte("héllo world"))
	fmt.Println(r.Len())
	fmt.Println(r.Size())

	// Reset changes everything observable
	r.Reset([]byte("ab"))
	fmt.Println(r.Len())
	fmt.Println(r.Size())

	// Empty reader
	e := bytes.NewReader(nil)
	fmt.Println(e.Len())
	fmt.Println(e.Size())
	e.Reset(nil)
	fmt.Println(e.Len())

	// Len/Size after Reset with larger input
	r.Reset([]byte("0123456789"))
	fmt.Println(r.Len())
	fmt.Println(r.Size())
}
