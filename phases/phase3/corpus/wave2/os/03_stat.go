// 03_stat: Stat/Lstat, FileInfo surface, SameFile.
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
	dir, err := os.MkdirTemp("", "os03-")
	check(err)
	defer os.RemoveAll(dir)

	p := dir + "/f"
	check(os.WriteFile(p, []byte("12345"), 0644))

	fi, err := os.Stat(p)
	check(err)
	if fi.Name() != "f" || fi.Size() != 5 || !fi.Mode().IsRegular() {
		fmt.Println("FAIL: stat", fi.Name(), fi.Size(), fi.Mode())
		os.Exit(1)
	}
	if fi.ModTime().Unix() <= 0 {
		fmt.Println("FAIL: modtime", fi.ModTime())
		os.Exit(1)
	}

	di, err := os.Stat(dir)
	check(err)
	if !di.Mode().IsDir() {
		fmt.Println("FAIL: dir mode", di.Mode())
		os.Exit(1)
	}

	lp := dir + "/link"
	check(os.Symlink(p, lp))
	li, err := os.Lstat(lp)
	check(err)
	if li.Mode()&os.ModeSymlink == 0 {
		fmt.Println("FAIL: lstat mode", li.Mode())
		os.Exit(1)
	}
	si, err := os.Stat(lp)
	check(err)
	if !si.Mode().IsRegular() || si.Size() != 5 {
		fmt.Println("FAIL: stat-through-link", si.Mode(), si.Size())
		os.Exit(1)
	}

	// SameFile: two stats of the same file; different files differ.
	fi2, err := os.Stat(p)
	check(err)
	if !os.SameFile(fi, fi2) {
		fmt.Println("FAIL: SameFile true case")
		os.Exit(1)
	}
	p2 := dir + "/g"
	check(os.WriteFile(p2, []byte("x"), 0644))
	gi, err := os.Stat(p2)
	check(err)
	if os.SameFile(fi, gi) {
		fmt.Println("FAIL: SameFile false case")
		os.Exit(1)
	}
	fmt.Println("ok")
}
