package main

import (
	"fmt"
	"io"
	"strings"
)

// io17: user-declared interface structural dispatch at runtime
// (D1, P3-M8 WS D; reviewer-2 P2-2). A user interface with the same
// method shape as io.Reader must dispatch to the concrete helper —
// the headline "user-declared interfaces dispatch too" claim, pinned
// at runtime, not just export shape. Sequential reads pin stateful
// advancement (the reader's offset moves), proving the helper really
// executes. Differential vs gc.
type reader interface {
	Read(p []byte) (int, error)
}

func main() {
	// User interface holding *strings.Reader: three sequential reads
	// (3 + 3 + 2 bytes of "abcdefgh"), then EOF.
	var r reader = strings.NewReader("abcdefgh")
	buf := make([]byte, 3)
	n1, err1 := r.Read(buf)
	fmt.Println(n1, err1 == nil, string(buf[:n1]))
	n2, err2 := r.Read(buf)
	fmt.Println(n2, err2 == nil, string(buf[:n2]))
	n3, err3 := r.Read(buf)
	fmt.Println(n3, err3 == nil, string(buf[:n3]))
	n4, err4 := r.Read(buf)
	fmt.Println(n4, err4 == io.EOF)

	// The stdlib interface alongside: same dynamic type through
	// io.Reader must behave identically.
	var ir io.Reader = strings.NewReader("xy")
	b2 := make([]byte, 4)
	m1, e1 := ir.Read(b2)
	fmt.Println(m1, e1 == nil, string(b2[:m1]))
	m2, e2 := ir.Read(b2)
	fmt.Println(m2, e2 == io.EOF)
}
