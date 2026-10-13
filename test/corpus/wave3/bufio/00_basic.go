package main

import (
	"bytes"
	"fmt"
	"bufio"
)

func main() {
	// Reader: Read, ReadByte, Peek.
	data := "hello world\nfoo bar\nbaz"
	r := bufio.NewReader(bytes.NewReader([]byte(data)))
	buf := make([]byte, 5)
	n, err := r.Read(buf)
	fmt.Println(n, err, string(buf[:n]))
	c, err := r.ReadByte()
	fmt.Println(c, err)
	peek, err := r.Peek(5)
	fmt.Println(string(peek), err)
	// ReadBytes, ReadString.
	r2 := bufio.NewReader(bytes.NewReader([]byte("a,b,c\n")))
	b, err := r2.ReadBytes(',')
	fmt.Println(string(b), err)
	s, err := r2.ReadString('\n')
	fmt.Println(s, err)
	// ReadSlice.
	r3 := bufio.NewReader(bytes.NewReader([]byte("x:y:z")))
	sl, err := r3.ReadSlice(':')
	fmt.Println(string(sl), err)
	// Discard.
	r4 := bufio.NewReader(bytes.NewReader([]byte("0123456789")))
	d, err := r4.Discard(5)
	fmt.Println(d, err)
	c2, _ := r4.ReadByte()
	fmt.Println(c2)
	// Writer: Write, WriteString, Flush.
	// (P3-M9: avoid &out — frontend/v1 doesn't support & on Ident.)
	out := new(bytes.Buffer)
	w := bufio.NewWriter(out)
	n2, err := w.Write([]byte("hello "))
	fmt.Println(n2, err)
	n3, err := w.WriteString("world")
	fmt.Println(n3, err)
	w.Flush()
	fmt.Println(out.String())
}
