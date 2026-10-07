package main

import (
	"bytes"
	"fmt"
)

func main() {
	// NewBuffer / NewBufferString / read-only observers
	// (Truncate/Reset/Next write the int8 lastRead field: runtime crash, see README)
	b := bytes.NewBufferString("hello")
	fmt.Println(b.Len())
	fmt.Println(b.Cap())
	fmt.Println(b.Available())
	fmt.Println(string(b.Bytes()))
	fmt.Println(b.String())

	nb := bytes.NewBuffer([]byte("world"))
	fmt.Println(nb.Len())
	fmt.Println(string(nb.Bytes()))

	eb := bytes.NewBuffer(nil)
	fmt.Println(eb.Len())
	fmt.Println(eb.Cap())
	fmt.Println(string(eb.Bytes()))
	fmt.Println(eb.String() == "")

	// Grow (no lastRead write on the grow path)
	g := bytes.NewBufferString("abcdef")
	g.Grow(100)
	fmt.Println(g.Cap() >= 106)
	fmt.Println(g.Len())
	fmt.Println(string(g.Bytes()))

	// AvailableBuffer is always empty (len 0)
	fmt.Println(len(g.AvailableBuffer()) == 0)
	fmt.Println(len(eb.AvailableBuffer()) == 0)

	// Bytes aliases the buffer contents
	bb := bytes.NewBufferString("abc")
	fmt.Println(bytes.Equal(bb.Bytes(), []byte("abc")))
}
