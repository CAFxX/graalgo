// P3-M9 wave3: net.Pipe — pure Go (channels), no socket intrinsics.
// Pipe-leg of the net.Pipe split (03-corpus-design.md §1.4).
package main

import (
	"fmt"
	"io"
	"net"
)

func main() {
	c1, c2 := net.Pipe()

	// Basic write/read.
	done := make(chan bool)
	go func() {
		buf := make([]byte, 5)
		n, err := io.ReadFull(c2, buf)
		if err != nil || n != 5 || string(buf) != "hello" {
			fmt.Println("read-err")
			done <- false
			return
		}
		fmt.Println("read-ok")
		done <- true
	}()

	n, err := c1.Write([]byte("hello"))
	if err != nil || n != 5 {
		fmt.Println("write-err")
		return
	}
	fmt.Println("write-ok")
	if !<-done {
		return
	}

	// Addr.
	fmt.Println("local", c1.LocalAddr().Network(), c1.LocalAddr().String())
	fmt.Println("remote", c1.RemoteAddr().Network(), c1.RemoteAddr().String())

	// Close: read after remote close gives EOF.
	c1.Close()
	buf := make([]byte, 10)
	_, err = c2.Read(buf)
	fmt.Println("eof", err == io.EOF)
	c2.Close()

	fmt.Println("ok")
}
