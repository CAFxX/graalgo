package main

import (
	"fmt"
	"io"
)

// io06: OffsetWriter.Seek math — SeekEnd is invalid for OffsetWriter
// (unlike SectionReader), offsets below base are rejected.
func main() {
	ow := io.NewOffsetWriter(nil, 100)
	pos, err := ow.Seek(10, io.SeekStart)
	fmt.Println(pos, err == nil)
	pos, err = ow.Seek(5, io.SeekCurrent)
	fmt.Println(pos, err == nil)
	pos, err = ow.Seek(0, io.SeekEnd)
	fmt.Println(pos, err == nil, err.Error())
	pos, err = ow.Seek(-200, io.SeekStart)
	fmt.Println(pos, err == nil, err.Error())
}
