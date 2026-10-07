package main

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

// io14: ReadAtLeast's 4-way edge matrix — ErrShortBuffer before any read,
// EOF-after-partial to ErrUnexpectedEOF, error-after-min dropped, and
// n >= min iff err == nil. SCOPE-LIMITED: needs r.Read (WS D).
var errBoom = errors.New("boom")

// failReader yields at most 2 bytes per Read and fails with errBoom once
// drained — so a ReadAtLeast that stops at min never sees the error.
type failReader struct{ data []byte }

func (f *failReader) Read(p []byte) (int, error) {
	if len(f.data) == 0 {
		return 0, errBoom
	}
	n := 2
	if len(f.data) < n {
		n = len(f.data)
	}
	if n > len(p) {
		n = len(p)
	}
	copy(p, f.data[:n])
	f.data = f.data[n:]
	return n, nil
}

func main() {
	// min > len(buf): ErrShortBuffer, no read issued.
	n, err := io.ReadAtLeast(strings.NewReader("hello"), make([]byte, 3), 5)
	fmt.Println(n, err == io.ErrShortBuffer)
	// Partial then EOF: ErrUnexpectedEOF.
	n2, err2 := io.ReadAtLeast(strings.NewReader("hi"), make([]byte, 5), 5)
	fmt.Println(n2, err2 == io.ErrUnexpectedEOF)
	// Error after >= min bytes: dropped (the boom never fires).
	fr := &failReader{data: []byte("abcdef")}
	n3, err3 := io.ReadAtLeast(fr, make([]byte, 5), 3)
	fmt.Println(n3, err3 == nil)
	// n >= min iff err == nil (ReadFull).
	n4, err4 := io.ReadFull(strings.NewReader("hello world"), make([]byte, 5))
	fmt.Println(n4, err4 == nil)
}
