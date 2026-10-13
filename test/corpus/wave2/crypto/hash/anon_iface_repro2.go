// Reproducer v3: h is of interface type Hash (single-method), asserted
// to anonymous interface. No multi-result functions.
package main

import "fmt"

type Hash interface {
	Sum() int
}

type hasher struct{ n int }

func (h *hasher) WriteSingle(p []byte) int { return 5 }
func (h *hasher) Sum() int                 { return 42 }

func hashWrite(h Hash, p []byte) int {
	// Anonymous interface assertion (was broken in __hashWrite)
	if w, ok := h.(interface{ WriteSingle([]byte) int }); ok {
		return w.WriteSingle(p)
	}
	panic("assertion failed")
}

func main() {
	var h Hash = &hasher{}
	got := hashWrite(h, []byte("hello"))
	fmt.Println("anon:", got)
	if got != 5 {
		panic("anonymous interface dispatch returned wrong value")
	}
	fmt.Println("ok")
}
