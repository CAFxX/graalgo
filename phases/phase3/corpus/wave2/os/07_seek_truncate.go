// 07_seek_truncate: seek whences, sparse extension via truncate,
// ReadAt past EOF.
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
	dir, err := os.MkdirTemp("", "os07-")
	check(err)
	defer os.RemoveAll(dir)

	p := dir + "/s"
	f, err := os.OpenFile(p, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	check(err)

	_, err = f.WriteString("0123456789")
	check(err)

	// SEEK_SET / SEEK_CUR / SEEK_END.
	off, err := f.Seek(4, 0)
	check(err)
	if off != 4 {
		fmt.Println("FAIL: seek set =", off)
		os.Exit(1)
	}
	off, err = f.Seek(2, 1)
	check(err)
	if off != 6 {
		fmt.Println("FAIL: seek cur =", off)
		os.Exit(1)
	}
	off, err = f.Seek(-3, 2)
	check(err)
	if off != 7 {
		fmt.Println("FAIL: seek end =", off)
		os.Exit(1)
	}
	if _, err := f.Seek(-100, 0); err == nil {
		fmt.Println("FAIL: seek before start")
		os.Exit(1)
	}

	// ReadAt past EOF → 0, io.EOF.
	n, err := f.ReadAt(make([]byte, 4), 100)
	if n != 0 || err == nil {
		fmt.Println("FAIL: readat eof:", n, err)
		os.Exit(1)
	}

	// Sparse extension.
	check(f.Truncate(20))
	fi, err := f.Stat()
	check(err)
	if fi.Size() != 20 {
		fmt.Println("FAIL: size =", fi.Size())
		os.Exit(1)
	}
	zb := make([]byte, 10)
	n, err = f.ReadAt(zb, 10)
	check(err)
	if n != 10 {
		fmt.Println("FAIL: sparse read n =", n)
		os.Exit(1)
	}
	for i, b := range zb {
		if b != 0 {
			fmt.Println("FAIL: sparse byte", i, b)
			os.Exit(1)
		}
	}
	check(f.Close())
	fmt.Println("ok")
}
