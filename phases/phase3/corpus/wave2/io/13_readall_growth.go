package main

import (
	"fmt"
	"io"
	"strings"
)

// io13: ReadAll through an interface over synthetic readers: empty,
// small (single chunk), medium (crosses the 512-cap / 256-growth
// boundary), and large (exercises the chunk-list rollover and final
// right-sized copy). D1 (P3-M8 WS D): multi-result interface dispatch.
func readAllSize(n int) {
	s := strings.Repeat("a", n)
	var r io.Reader = strings.NewReader(s)
	b, err := io.ReadAll(r)
	fmt.Println(len(b), err == nil, len(b) == n && string(b) == s)
}

func main() {
	// Empty: returns empty non-nil slice, nil error.
	var r0 io.Reader = strings.NewReader("")
	b0, err0 := io.ReadAll(r0)
	fmt.Println(len(b0), b0 != nil, err0 == nil)

	readAllSize(3)     // single small read
	readAllSize(600)   // crosses initial 512 cap
	readAllSize(5000)  // multiple growth steps + chunk rollover
	readAllSize(20000) // large: several chunks, final copy
}
