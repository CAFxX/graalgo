package main

import (
	"bytes"
	"fmt"
)

func main() {
	// Trim + ToUpper + ReplaceAll (ASCII only; no func values)
	fmt.Println(string(bytes.ReplaceAll(
		bytes.ToUpper(bytes.TrimSpace([]byte("  hello  "))),
		[]byte("L"), []byte("1"))))

	// Split + Join round-trip
	parts := bytes.Split([]byte("a,b,c"), []byte(","))
	fmt.Println(string(bytes.Join(parts, []byte(","))))

	// Fields + Join
	fmt.Println(string(bytes.Join(bytes.Fields([]byte("  a b c  ")), []byte("-"))))

	// Replace + ToUpper
	fmt.Println(string(bytes.ToUpper(bytes.Replace([]byte("a-b-c"), []byte("-"), []byte("_"), -1))))

	// Buffer: Grow + Bytes + Len/Cap observers
	b := bytes.NewBuffer(nil)
	b.Grow(16)
	fmt.Println(b.Cap() >= 16)
	fmt.Println(b.Len())
	fmt.Println(string(b.Bytes()))

	// Buffer contents feed package funcs
	b2 := bytes.NewBufferString("  padded  ")
	trimmed := bytes.TrimSpace(b2.Bytes())
	fmt.Println(string(trimmed))
	fmt.Println(bytes.Count(b2.Bytes(), []byte("d")))

	// SplitAfter + TrimPrefix intersection
	sp := bytes.SplitAfter([]byte("--a--b--"), []byte("--"))
	for i := 0; i < len(sp); i++ {
		fmt.Println(string(bytes.TrimPrefix(sp[i], []byte("--"))))
	}

	// Reader Reset + Len/Size, then package funcs on the side
	r := bytes.NewReader([]byte("hello"))
	fmt.Println(r.Len(), r.Size())
	r.Reset([]byte("world!"))
	fmt.Println(r.Len(), r.Size())
}
