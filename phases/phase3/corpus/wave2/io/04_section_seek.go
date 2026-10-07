package main

import (
	"fmt"
	"io"
)

// io04: SectionReader.Seek math (Start/Current/End), the two unexported
// seek errors (messages are the observable surface), Size, and the Outer
// triple desugar.
func main() {
	sr := io.NewSectionReader(nil, 10, 20)
	pos, err := sr.Seek(5, io.SeekStart)
	fmt.Println(pos, err == nil)
	pos, err = sr.Seek(3, io.SeekCurrent)
	fmt.Println(pos, err == nil)
	pos, err = sr.Seek(-4, io.SeekEnd)
	fmt.Println(pos, err == nil)
	pos, err = sr.Seek(0, 99)
	fmt.Println(pos, err == nil, err.Error())
	pos, err = sr.Seek(-100, io.SeekStart)
	fmt.Println(pos, err == nil, err.Error())
	fmt.Println(sr.Size())
	r, off, n := sr.Outer()
	fmt.Println(off, n, r == nil)
}
