// 00_file_rw: basic file write/read/seek/readAt/writeAt/stat/truncate/sync.
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
	dir, err := os.MkdirTemp("", "os00-")
	check(err)
	defer os.RemoveAll(dir)

	p := dir + "/f.txt"
	f, err := os.Create(p)
	check(err)

	n, err := f.WriteString("hello world")
	check(err)
	if n != 11 {
		fmt.Println("FAIL: write n =", n)
		os.Exit(1)
	}

	off, err := f.Seek(0, 0)
	check(err)
	if off != 0 {
		fmt.Println("FAIL: seek =", off)
		os.Exit(1)
	}
	buf := make([]byte, 5)
	n, err = f.Read(buf)
	check(err)
	if n != 5 || string(buf) != "hello" {
		fmt.Println("FAIL: read =", n, string(buf))
		os.Exit(1)
	}

	rb := make([]byte, 5)
	n, err = f.ReadAt(rb, 6)
	check(err)
	if n != 5 || string(rb) != "world" {
		fmt.Println("FAIL: readat =", n, string(rb))
		os.Exit(1)
	}

	n, err = f.WriteAt([]byte("Go!"), 6)
	check(err)
	if n != 3 {
		fmt.Println("FAIL: writeat n =", n)
		os.Exit(1)
	}

	fi, err := f.Stat()
	check(err)
	if fi.Size() != 11 || !fi.Mode().IsRegular() {
		fmt.Println("FAIL: stat", fi.Size(), fi.Mode())
		os.Exit(1)
	}

	check(f.Truncate(4))
	check(f.Sync())
	check(f.Close())

	b, err := os.ReadFile(p)
	check(err)
	if string(b) != "hell" {
		fmt.Println("FAIL: final content =", string(b))
		os.Exit(1)
	}
	fmt.Println("ok")
}
