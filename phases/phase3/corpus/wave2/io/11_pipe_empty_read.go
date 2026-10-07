package main

import (
	"fmt"
	"io"
)

// io11: a zero-length Read consumes a rendezvous but moves no bytes — the
// writer's loop re-sends the unwritten remainder on the next Read.
func main() {
	r, w := io.Pipe()
	go func() {
		w.Write([]byte("hi"))
		w.Close()
	}()
	n1, e1 := r.Read([]byte{})
	fmt.Println(n1, e1 == nil)
	buf := make([]byte, 8)
	n2, e2 := r.Read(buf)
	fmt.Println(n2, e2 == nil, string(buf[:n2]))
	n3, e3 := r.Read(buf)
	fmt.Println(n3, e3 == io.EOF)
}
