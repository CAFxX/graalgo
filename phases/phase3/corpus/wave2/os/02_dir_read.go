// 02_dir_read: directory iteration — ReadDir (File and package level),
// Readdirnames, chunked reads, and DirEntry types.
package main

import (
	"fmt"
	"io"
	"os"
	"sort"
)

func check(err error) {
	if err != nil {
		fmt.Println("FAIL:", err)
		os.Exit(1)
	}
}

func main() {
	dir, err := os.MkdirTemp("", "os02-")
	check(err)
	defer os.RemoveAll(dir)

	for _, n := range []string{"b", "a", "c"} {
		f, err := os.Create(dir + "/" + n)
		check(err)
		check(f.Close())
	}
	check(os.Mkdir(dir+"/sub", 0755))
	check(os.Symlink(dir+"/a", dir+"/link"))

	// Package-level ReadDir, sorted.
	des, err := os.ReadDir(dir)
	check(err)
	var names []string
	for _, de := range des {
		names = append(names, de.Name())
	}
	sort.Strings(names)
	want := []string{"a", "b", "c", "link", "sub"}
	if fmt.Sprint(names) != fmt.Sprint(want) {
		fmt.Println("FAIL: readdir names =", names)
		os.Exit(1)
	}
	// DirEntry types.
	types := map[string]bool{}
	for _, de := range des {
		types[de.Name()] = de.IsDir()
		if de.Name() == "link" {
			if de.Type()&os.ModeSymlink == 0 {
				fmt.Println("FAIL: link type =", de.Type())
				os.Exit(1)
			}
			// Info is about the link itself, not its target (gc docs).
			fi, err := de.Info()
			check(err)
			if fi.Mode()&os.ModeSymlink == 0 {
				fmt.Println("FAIL: link info mode =", fi.Mode())
				os.Exit(1)
			}
		}
	}
	if !types["sub"] || types["a"] {
		fmt.Println("FAIL: isdir map =", types)
		os.Exit(1)
	}

	// Chunked File.ReadDir.
	f, err := os.Open(dir)
	check(err)
	var got []string
	for {
		chunk, err := f.ReadDir(2)
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Println("FAIL: chunked:", err)
			os.Exit(1)
		}
		if len(chunk) == 0 {
			break
		}
		for _, de := range chunk {
			got = append(got, de.Name())
		}
	}
	check(f.Close())
	sort.Strings(got)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		fmt.Println("FAIL: chunked names =", got)
		os.Exit(1)
	}

	// Readdirnames(-1).
	f2, err := os.Open(dir)
	check(err)
	rn, err := f2.Readdirnames(-1)
	check(err)
	check(f2.Close())
	sort.Strings(rn)
	if fmt.Sprint(rn) != fmt.Sprint(want) {
		fmt.Println("FAIL: readdirnames =", rn)
		os.Exit(1)
	}
	fmt.Println("ok")
}
