package main

import (
	"fmt"
	"io"
)

// io03: close protocol — write-after-close gives ErrClosedPipe, read after
// write-close gives EOF, closes are idempotent and always return nil.
func main() {
	r, w := io.Pipe()
	fmt.Println(w.Close() == nil)
	n, err := w.Write([]byte("x"))
	fmt.Println(n, err == io.ErrClosedPipe)
	n2, err2 := r.Read(make([]byte, 8))
	fmt.Println(n2, err2 == io.EOF)
	fmt.Println(r.Close() == nil)
	fmt.Println(w.Close() == nil)

	// Read-close first: the writer sees ErrClosedPipe, not EOF.
	r3, w3 := io.Pipe()
	fmt.Println(r3.Close() == nil)
	n3, err3 := w3.Write([]byte("y"))
	fmt.Println(n3, err3 == io.ErrClosedPipe)
}
