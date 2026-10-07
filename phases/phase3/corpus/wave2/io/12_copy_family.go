package main

import (
	"fmt"
	"io"
	"strings"
)

// io12: Copy/CopyN/CopyBuffer dispatch (WriterTo/ReaderFrom arms and the
// manual loop with errInvalidWrite/ErrShortWrite normalization).
// SCOPE-LIMITED: needs 2-result interface dispatch (WS D).
type countWriter struct{ n int }

func (c *countWriter) Write(p []byte) (int, error) {
	c.n += len(p)
	return len(p), nil
}

func main() {
	dst := &countWriter{}
	n, err := io.Copy(dst, strings.NewReader("hello"))
	fmt.Println(n, err == nil, dst.n)
	dst2 := &countWriter{}
	n2, err2 := io.CopyN(dst2, strings.NewReader("hello world"), 5)
	fmt.Println(n2, err2 == nil, dst2.n)
	dst3 := &countWriter{}
	n3, err3 := io.CopyBuffer(dst3, strings.NewReader("hey"), make([]byte, 8))
	fmt.Println(n3, err3 == nil, dst3.n)
}
