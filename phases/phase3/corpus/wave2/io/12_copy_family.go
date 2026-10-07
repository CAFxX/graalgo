package main

import (
	"fmt"
	"io"
	"strings"
)

// io12: Copy/CopyN/CopyBuffer through interfaces (concrete synthetic
// types behind io.Reader/io.Writer): strings.Reader -> io.Discard, plus
// the CopyN limit path and CopyBuffer with an explicit buffer.
// D1 (P3-M8 WS D): multi-result interface dispatch.
func main() {
	// Copy: full drain.
	var src io.Reader = strings.NewReader("hello")
	var dst io.Writer = io.Discard
	n, err := io.Copy(dst, src)
	fmt.Println(n, err == nil)

	// CopyN: limit shorter than input.
	var src2 io.Reader = strings.NewReader("hello world")
	n2, err2 := io.CopyN(io.Discard, src2, 5)
	fmt.Println(n2, err2 == nil)

	// CopyN: limit longer than input (EOF before n).
	var src3 io.Reader = strings.NewReader("hi")
	n3, err3 := io.CopyN(io.Discard, src3, 100)
	fmt.Println(n3, err3 == io.EOF)

	// CopyBuffer with an explicit small buffer (multiple loop iterations).
	var src4 io.Reader = strings.NewReader("0123456789abcdef")
	n4, err4 := io.CopyBuffer(io.Discard, src4, make([]byte, 3))
	fmt.Println(n4, err4 == nil)

	// CopyBuffer with nil buffer (allocates the 32KB staging buffer).
	var src5 io.Reader = strings.NewReader("xyz")
	n5, err5 := io.CopyBuffer(io.Discard, src5, nil)
	fmt.Println(n5, err5 == nil)
}
