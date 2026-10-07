package main

import (
	"fmt"
	"io"
	"strings"
)

// io14: ReadAtLeast/ReadFull edge matrix through interfaces over
// strings.Reader (synthetic): ErrShortBuffer before any read,
// EOF-after-partial to ErrUnexpectedEOF, exact reads, and the
// n >= min iff err == nil contract. D1 (P3-M8 WS D).
func main() {
	// min > len(buf): ErrShortBuffer, no read issued.
	var r1 io.Reader = strings.NewReader("hello")
	n, err := io.ReadAtLeast(r1, make([]byte, 3), 5)
	fmt.Println(n, err == io.ErrShortBuffer)

	// Partial then EOF: ErrUnexpectedEOF.
	var r2 io.Reader = strings.NewReader("hi")
	n2, err2 := io.ReadAtLeast(r2, make([]byte, 5), 5)
	fmt.Println(n2, err2 == io.ErrUnexpectedEOF)

	// Exact: n == min, err == nil.
	var r3 io.Reader = strings.NewReader("hello")
	buf3 := make([]byte, 5)
	n3, err3 := io.ReadAtLeast(r3, buf3, 5)
	fmt.Println(n3, err3 == nil, string(buf3))

	// More than min available: stops at min... (reads until min).
	var r4 io.Reader = strings.NewReader("hello world")
	buf4 := make([]byte, 11)
	n4, err4 := io.ReadAtLeast(r4, buf4, 5)
	fmt.Println(n4, err4 == nil)

	// ReadFull: exact len(buf).
	var r5 io.Reader = strings.NewReader("hello world")
	buf5 := make([]byte, 5)
	n5, err5 := io.ReadFull(r5, buf5)
	fmt.Println(n5, err5 == nil, string(buf5))

	// ReadFull: short input -> ErrUnexpectedEOF.
	var r6 io.Reader = strings.NewReader("hi")
	n6, err6 := io.ReadFull(r6, make([]byte, 5))
	fmt.Println(n6, err6 == io.ErrUnexpectedEOF)

	// ReadFull: empty input -> EOF (n == 0).
	var r7 io.Reader = strings.NewReader("")
	n7, err7 := io.ReadFull(r7, make([]byte, 5))
	fmt.Println(n7, err7 == io.EOF)
}
