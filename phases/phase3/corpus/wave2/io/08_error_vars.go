package main

import (
	"fmt"
	"io"
)

// io08: every error var's message and the seek consts — the stable
// observable surface programs match on.
func main() {
	fmt.Println(io.EOF.Error())
	fmt.Println(io.ErrUnexpectedEOF.Error())
	fmt.Println(io.ErrShortWrite.Error())
	fmt.Println(io.ErrShortBuffer.Error())
	fmt.Println(io.ErrNoProgress.Error())
	fmt.Println(io.ErrClosedPipe.Error())
	fmt.Println(io.SeekStart, io.SeekCurrent, io.SeekEnd)
}
