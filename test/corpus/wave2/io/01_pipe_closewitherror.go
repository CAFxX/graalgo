package main

import (
	"errors"
	"fmt"
	"io"
)

// io01: CloseWithError wakes a blocked reader AND a blocked writer with the
// custom error; the error is stored once and never overwritten.
func main() {
	custom := errors.New("boom")

	// Blocked reader woken by writer-side CloseWithError.
	r, w := io.Pipe()
	readRes := make(chan string, 1)
	go func() {
		n, err := r.Read(make([]byte, 16))
		readRes <- fmt.Sprint(n, " ", err.Error())
	}()
	fmt.Println(w.CloseWithError(custom) == nil)
	fmt.Println(<-readRes)

	// Blocked writer woken by reader-side CloseWithError.
	r2, w2 := io.Pipe()
	writeRes := make(chan string, 1)
	go func() {
		n, err := w2.Write([]byte("hello"))
		if err != nil {
			writeRes <- fmt.Sprint(n, " ", err.Error())
			return
		}
		writeRes <- fmt.Sprint(n, " nil")
	}()
	fmt.Println(r2.CloseWithError(custom) == nil)
	fmt.Println(<-writeRes)

	// A second CloseWithError does not overwrite the first error.
	fmt.Println(w2.CloseWithError(errors.New("other")) == nil)
	n, err := r2.Read(make([]byte, 16))
	fmt.Println(n, err.Error())
}
