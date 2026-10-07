package main

import (
	"fmt"
	"strings"
)

func main() {
	// Basic Builder use (single-result methods only; Write/WriteRune/
	// WriteString are multi-result and fail fast under WS D).
	var b strings.Builder
	b.WriteByte('h')
	b.WriteByte('i')
	fmt.Println(b.String())
	fmt.Println(b.Len())
	fmt.Println(b.Cap() >= b.Len())

	// Grow and Reset
	b.Grow(100)
	fmt.Println(b.Cap() >= 100)
	b.Reset()
	fmt.Println(b.Len())
	fmt.Println(b.String() == "")

	// WriteByte error is always nil
	var b2 strings.Builder
	err := b2.WriteByte('x')
	fmt.Println(err == nil)
	fmt.Println(b2.String())
}
