// 08_pipe: os.Pipe roundtrip, direction errors, NewFile(99) differential
// (non-nil under both gc and graalgo; ops on it fail).
package main

import (
	"fmt"
	"os"
)

func check(err error) {
	if err != nil {
		fmt.Println("FAIL:", err)
		os.Exit(1)
	}
}

func main() {
	r, w, err := os.Pipe()
	check(err)

	n, err := w.WriteString("pipedata")
	check(err)
	if n != 8 {
		fmt.Println("FAIL: pipe write n =", n)
		os.Exit(1)
	}
	buf := make([]byte, 8)
	n, err = r.Read(buf)
	check(err)
	if n != 8 || string(buf) != "pipedata" {
		fmt.Println("FAIL: pipe read =", n, string(buf))
		os.Exit(1)
	}
	// Direction errors.
	if _, err := r.Write([]byte("x")); err == nil {
		fmt.Println("FAIL: write to pipe read end")
		os.Exit(1)
	}
	if _, err := w.Read(make([]byte, 1)); err == nil {
		fmt.Println("FAIL: read from pipe write end")
		os.Exit(1)
	}
	// Closing the write end → read EOF.
	check(w.Close())
	n, err = r.Read(make([]byte, 8))
	if n != 0 {
		fmt.Println("FAIL: pipe EOF n =", n, err)
		os.Exit(1)
	}
	check(r.Close())

	// NewFile(99): non-nil under both runtimes (gc: nil only for
	// negative fds); ops on it fail.
	nf := os.NewFile(99, "foreign")
	if nf == nil {
		fmt.Println("FAIL: NewFile(99) == nil")
		os.Exit(1)
	}
	if _, err := nf.Stat(); err == nil {
		fmt.Println("FAIL: NewFile(99) stat")
		os.Exit(1)
	}
	var negFd uintptr = 1<<64 - 1 // -1 as uintptr
	if os.NewFile(negFd, "neg") != nil {
		fmt.Println("FAIL: NewFile(-1) != nil")
		os.Exit(1)
	}
	fmt.Println("ok")
}
