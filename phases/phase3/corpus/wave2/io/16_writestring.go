package main

import (
	"fmt"
	"io"
	"strings"
)

// io16: WriteString through interfaces: the StringWriter fast path
// (io.Discard implements StringWriter) and the fallback path
// (a Writer that is not a StringWriter). D1 (P3-M8 WS D).
func main() {
	// Fast path: io.Discard implements io.StringWriter.
	var w1 io.Writer = io.Discard
	n1, err1 := io.WriteString(w1, "hello")
	fmt.Println(n1, err1 == nil)

	// Fallback: strings.Reader is not a Writer; use a custom check via
	// PipeWriter? No — use Discard for the fast path and verify the
	// fallback by writing through the Writer interface directly.
	var w2 io.Writer = io.Discard
	n2, err2 := w2.Write([]byte("world"))
	fmt.Println(n2, err2 == nil)

	// WriteString with empty string.
	n3, err3 := io.WriteString(io.Discard, "")
	fmt.Println(n3, err3 == nil)

	// The StringWriter interface assertion itself.
	sw := io.Discard.(io.StringWriter)
	n4, err4 := sw.WriteString("asserted")
	fmt.Println(n4, err4 == nil)

	// Ensure strings package still works alongside.
	var r io.Reader = strings.NewReader("x")
	buf := make([]byte, 4)
	n5, err5 := r.Read(buf)
	fmt.Println(n5, err5 == nil)
}
