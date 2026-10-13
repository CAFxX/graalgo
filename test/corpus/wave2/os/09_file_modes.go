// 09_file_modes: O_APPEND atomicity, OpenFile flags, Create perm bits,
// WriteFile/ReadFile.
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
	dir, err := os.MkdirTemp("", "os09-")
	check(err)
	defer os.RemoveAll(dir)

	// WriteFile + ReadFile.
	p := dir + "/w"
	check(os.WriteFile(p, []byte("one"), 0644))
	b, err := os.ReadFile(p)
	check(err)
	if string(b) != "one" {
		fmt.Println("FAIL: readfile =", string(b))
		os.Exit(1)
	}

	// O_APPEND: two writes append, never overwrite (R2 atomicity).
	a, err := os.OpenFile(p, os.O_WRONLY|os.O_APPEND, 0644)
	check(err)
	if _, err := a.WriteString("-two"); err != nil {
		fmt.Println("FAIL: append write:", err)
		os.Exit(1)
	}
	check(a.Close())
	b, err = os.ReadFile(p)
	check(err)
	if string(b) != "one-two" {
		fmt.Println("FAIL: append content =", string(b))
		os.Exit(1)
	}

	// Create applies the 0777 perm bits (modulo umask — owner bits stay).
	c := dir + "/c"
	f, err := os.OpenFile(c, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0640)
	check(err)
	check(f.Close())
	fi, err := os.Stat(c)
	check(err)
	if fi.Mode().Perm()&0600 != 0600 {
		fmt.Println("FAIL: create perm =", fi.Mode().Perm())
		os.Exit(1)
	}

	// O_TRUNC via OpenFile.
	t, err := os.OpenFile(p, os.O_WRONLY|os.O_TRUNC, 0644)
	check(err)
	check(t.Close())
	fi, err = os.Stat(p)
	check(err)
	if fi.Size() != 0 {
		fmt.Println("FAIL: trunc size =", fi.Size())
		os.Exit(1)
	}
	fmt.Println("ok")
}
