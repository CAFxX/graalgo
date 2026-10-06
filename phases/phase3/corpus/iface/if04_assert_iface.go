package main

import "fmt"

type Writer interface{ Write([]byte) int }

type Buf struct{ n int }

func (b *Buf) Write(p []byte) int { b.n += len(p); return len(p) }

type Closer interface{ Close() int }

func main() {
	b := &Buf{}
	// Empty-interface boxing...
	var a any = b
	// ...then a type assertion back to a NON-EMPTY interface, then dispatch.
	w := a.(Writer)
	fmt.Println(w.Write([]byte("hello")))
	fmt.Println(b.n)
	// Comma-ok: a succeeding and a failing assertion.
	_, ok := a.(Writer)
	fmt.Println(ok)
	_, ok2 := a.(Closer)
	fmt.Println(ok2)
}
