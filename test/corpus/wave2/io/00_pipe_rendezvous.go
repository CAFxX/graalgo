package main

import (
	"fmt"
	"io"
)

// io00: basic pipe rendezvous — one Write consumed by multiple Reads,
// Close delivers EOF, write-after-EOF accounting is exact.
func main() {
	r, w := io.Pipe()
	done := make(chan string, 1)
	go func() {
		buf := make([]byte, 8)
		var out []byte
		for {
			n, err := r.Read(buf)
			out = append(out, buf[:n]...)
			if err != nil {
				done <- fmt.Sprint(len(out), " ", err == io.EOF)
				return
			}
		}
	}()
	msg := []byte("hello pipe")
	n, err := w.Write(msg)
	fmt.Println("write:", n, err == nil)
	w.Close()
	fmt.Println(<-done)
}
