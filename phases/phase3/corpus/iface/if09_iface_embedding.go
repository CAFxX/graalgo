package main

import "fmt"

type Writer interface{ Write([]byte) int }
type Closer interface{ Close() int }

// Interface embedding: the method set is the flattened union (the
// exporter flattens via go/types' method set); the itab is built from
// that list.
type WriteCloser interface {
	Writer
	Closer
}

type Buf struct {
	n      int
	closed bool
}

func (b *Buf) Write(p []byte) int { b.n += len(p); return len(p) }
func (b *Buf) Close() int {
	b.closed = true
	return b.n
}

func finish(wc WriteCloser) int {
	wc.Write([]byte("abc"))
	return wc.Close()
}

func main() {
	b := &Buf{}
	var wc WriteCloser = b
	fmt.Println(finish(wc))
	fmt.Println(b.n, b.closed)
}
