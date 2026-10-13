package main

import "fmt"

type Writer interface{ Write([]byte) int }

type Buf struct{ n int }

func (b *Buf) Write(p []byte) int {
	if b == nil {
		return -1
	}
	b.n += len(p)
	return len(p)
}

func main() {
	// Typed nil in the interface: the method RUNS with a nil receiver —
	// no panic (unlike if06's nil interface).
	var w Writer = (*Buf)(nil)
	fmt.Println(w.Write([]byte("hello")))
	fmt.Println(w == nil)
}
