package main

import (
	"fmt"
	"io"
)

// io05: NewSectionReader's overflow branch (off > maxint64-n clamps the
// limit to maxint64) and the exact-size boundary.
func main() {
	const maxint64 = int64(1<<63 - 1)
	sr := io.NewSectionReader(nil, maxint64-5, 10)
	fmt.Println(sr.Size())
	sr2 := io.NewSectionReader(nil, 0, maxint64)
	fmt.Println(sr2.Size())
	sr3 := io.NewSectionReader(nil, 7, 0)
	fmt.Println(sr3.Size())
}
