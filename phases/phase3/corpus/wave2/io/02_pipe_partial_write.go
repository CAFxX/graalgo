package main

import (
	"fmt"
	"io"
)

// io02: a 100-byte Write consumed by 7-byte Reads — the write loop must
// deliver every byte across multiple rendezvous with exact accounting.
func main() {
	r, w := io.Pipe()
	data := make([]byte, 100)
	for i := range data {
		data[i] = byte(i)
	}
	wrote := make(chan string, 1)
	go func() {
		n, err := w.Write(data)
		w.Close()
		wrote <- fmt.Sprint(n, " ", err == nil)
	}()
	var out []byte
	buf := make([]byte, 7)
	for {
		n, err := r.Read(buf)
		out = append(out, buf[:n]...)
		if err != nil {
			break
		}
	}
	fmt.Println(<-wrote)
	fmt.Println(len(out), out[0], out[99])
	ok := len(out) == 100
	for i := range out {
		if out[i] != byte(i) {
			ok = false
		}
	}
	fmt.Println(ok)
}
