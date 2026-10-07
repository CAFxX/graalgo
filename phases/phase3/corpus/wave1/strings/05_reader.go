package main

import (
	"fmt"
	"strings"
)

func main() {
	// Reader basics (single-result methods; Read/ReadAt/ReadByte/ReadRune/
	// Seek are multi-result and fail fast under WS D).
	r := strings.NewReader("hello")
	fmt.Println(r.Len())
	fmt.Println(r.Size())
	r.Reset("world")
	fmt.Println(r.Len())
	fmt.Println(r.Size())
}
