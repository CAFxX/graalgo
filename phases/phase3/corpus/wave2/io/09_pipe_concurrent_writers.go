package main

import (
	"fmt"
	"io"
)

// io09: two goroutines sharing one PipeWriter — wrMu must serialize the
// writes so every byte arrives exactly once (order between the two
// writers is unspecified; counts are exact).
func main() {
	r, w := io.Pipe()
	done := make(chan int, 2)
	go func() {
		nw, ew := w.Write([]byte("aaaa"))
		if ew != nil {
			done <- -1
			return
		}
		done <- nw
	}()
	go func() {
		nw, ew := w.Write([]byte("bbbb"))
		if ew != nil {
			done <- -1
			return
		}
		done <- nw
	}()
	var out []byte
	buf := make([]byte, 8)
	for len(out) < 8 {
		n, err := r.Read(buf)
		out = append(out, buf[:n]...)
		if err != nil {
			break
		}
	}
	w.Close()
	d1, d2 := <-done, <-done
	ca, cb := 0, 0
	for _, b := range out {
		if b == 'a' {
			ca++
		} else if b == 'b' {
			cb++
		}
	}
	fmt.Println(d1, d2, len(out), ca, cb)
}
