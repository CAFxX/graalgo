package main

import (
	"fmt"
	"io"
)

// io15: a 2-target call through an interface VALUE (var rd io.Reader = r;
// rd.Read(buf)). SCOPE-LIMITED: ADR-0003 IfaceDispatchNode is
// single-result only; no 2-result method can be emitted (WS D).
func main() {
	r, w := io.Pipe()
	go func() {
		w.Write([]byte("x"))
		w.Close()
	}()
	var rd io.Reader = r
	buf := make([]byte, 8)
	n, err := rd.Read(buf)
	fmt.Println(n, err == nil, string(buf[:n]))
	n2, err2 := rd.Read(buf)
	fmt.Println(n2, err2 == io.EOF)
}
