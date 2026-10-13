// 14_epipe: write to a broken pipe (read end closed, non-stdout fd).
// gc: Write returns EPIPE as an error and SURVIVES (only fd 1/2 die via
// SIGPIPE). graalgo must match: EPIPE error return, not death (R7: the
// SIGPIPE dance in GoOs.write is stdio-only by design).
package main

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

func main() {
	r, w, err := os.Pipe()
	if err != nil {
		fmt.Println("pipe err:", err)
		os.Exit(1)
	}
	r.Close()
	n, err := w.Write([]byte("hello"))
	fmt.Println("n:", n)
	if !errors.Is(err, syscall.EPIPE) {
		fmt.Println("not EPIPE:", err)
		os.Exit(1)
	}
	fmt.Println("EPIPE-OK")
}
