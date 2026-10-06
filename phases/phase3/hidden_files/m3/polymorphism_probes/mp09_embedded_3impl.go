package main

import "fmt"

type Writer interface{ Write(p []byte) int }
type Closer interface{ Close() bool }
type WriteCloser interface {
	Writer
	Closer
}

type A struct{ n int }

func (a *A) Write(p []byte) int { a.n += len(p); return len(p) }
func (a *A) Close() bool        { return a.n > 0 }

type B struct{ s string }

func (b *B) Write(p []byte) int { b.s += string(p); return len(p) }
func (b *B) Close() bool        { return len(b.s) > 0 }

type C struct{ c int }

func (c *C) Write(p []byte) int { c.c++; return len(p) }
func (c *C) Close() bool        { return c.c > 2 }

func use(wc WriteCloser) int {
	n := wc.Write([]byte("hey"))
	ok := wc.Close()
	if ok {
		return n
	}
	return -n
}

func main() {
	wcs := []WriteCloser{&A{}, &B{}, &C{}}
	for _, wc := range wcs {
		fmt.Println(use(wc))
	}
}
