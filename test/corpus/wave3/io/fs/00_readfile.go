package main

// 00_readfile: fs.ReadFile over os.DirFS — basic read, missing file,
// directory read (error), empty file.
import (
	"fmt"
	"io/fs"
	"os"
)

func check(err error) {
	if err != nil {
		fmt.Println("FAIL:", err)
		os.Exit(1)
	}
}

func main() {
	dir, err := os.MkdirTemp("", "iofs00-")
	check(err)
	defer os.RemoveAll(dir)

	check(os.WriteFile(dir+"/hello.txt", []byte("hello world"), 0644))
	check(os.WriteFile(dir+"/empty.txt", []byte{}, 0644))
	check(os.Mkdir(dir+"/subdir", 0755))

	fsys := os.DirFS(dir)

	// Basic read.
	b, err := fs.ReadFile(fsys, "hello.txt")
	check(err)
	fmt.Printf("read: %q\n", string(b))

	// Empty file.
	b, err = fs.ReadFile(fsys, "empty.txt")
	check(err)
	fmt.Printf("empty: len=%d\n", len(b))

	// Missing file — must error.
	_, err = fs.ReadFile(fsys, "missing.txt")
	fmt.Printf("missing-err: %v\n", err != nil)

	// Directory read — gc returns an error (is a directory).
	_, err = fs.ReadFile(fsys, "subdir")
	fmt.Printf("dir-err: %v\n", err != nil)

	// Invalid path (not a valid fs path) — must error.
	_, err = fs.ReadFile(fsys, "../escape")
	fmt.Printf("invalid-err: %v\n", err != nil)

	fmt.Println("ok")
}
