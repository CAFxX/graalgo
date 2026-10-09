// Reproducer for P3-M8-leftovers: anonymous interface dispatch bug.
// A type assertion to an anonymous interface, followed by a method call
// through the asserted value, must dispatch to the concrete method.
// Before fix: returns 0 (wrong itab). After fix: returns 5.
package main

import "fmt"

type hasher struct{ n int }

func (h *hasher) WriteSingle(p []byte) int { return 5 }

func main() {
	var h interface{} = &hasher{}
	// Anonymous interface assertion + dispatch (was broken)
	if w, ok := h.(interface{ WriteSingle([]byte) int }); ok {
		got := w.WriteSingle([]byte("hello"))
		fmt.Println("anon:", got)
		if got != 5 {
			panic("anonymous interface dispatch returned wrong value")
		}
	} else {
		panic("assertion failed")
	}
	fmt.Println("ok")
}
