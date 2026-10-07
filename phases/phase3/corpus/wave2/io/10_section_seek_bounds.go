package main

import (
	"fmt"
	"io"
)

// io10: seek bounds — seeking past the section limit is allowed (only
// offsets below base are rejected); a zero-length section still seeks.
func main() {
	sr := io.NewSectionReader(nil, 10, 20)
	pos, err := sr.Seek(1000, io.SeekStart)
	fmt.Println(pos, err == nil)
	pos, err = sr.Seek(-11, io.SeekStart)
	fmt.Println(pos, err == nil, err.Error())
	empty := io.NewSectionReader(nil, 3, 0)
	fmt.Println(empty.Size())
	pos, err = empty.Seek(0, io.SeekStart)
	fmt.Println(pos, err == nil)
}
