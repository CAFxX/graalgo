package main

import (
	"fmt"
	"io"
)

// io13: ReadAll's growth schedule as observed by a counting reader —
// initial 512-cap read, then the 256→384→576 growth with chunk-list
// rollover at 1/16 capacity. SCOPE-LIMITED: needs r.Read (WS D).
type chunkReader struct {
	chunks [][]byte
	calls  []int
}

func (c *chunkReader) Read(p []byte) (int, error) {
	if len(c.chunks) == 0 {
		return 0, io.EOF
	}
	n := copy(p, c.chunks[0])
	c.calls = append(c.calls, n)
	c.chunks = c.chunks[1:]
	return n, nil
}

func main() {
	cr := &chunkReader{chunks: [][]byte{[]byte("ab"), []byte("cdef"), []byte("gh")}}
	b, err := io.ReadAll(cr)
	fmt.Println(string(b), err == nil, cr.calls)
	empty, err2 := io.ReadAll(&chunkReader{})
	fmt.Println(len(empty), empty != nil, err2 == nil)
}
