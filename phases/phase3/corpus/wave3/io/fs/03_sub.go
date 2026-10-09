package main

// 03_sub: fs.Sub over os.DirFS — subtree isolation, invalid dir,
// nested Sub, error path rewriting.
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
	dir, err := os.MkdirTemp("", "iofs03-")
	check(err)
	defer os.RemoveAll(dir)

	check(os.MkdirAll(dir+"/sub/deep", 0755))
	check(os.WriteFile(dir+"/top.txt", []byte("top"), 0644))
	check(os.WriteFile(dir+"/sub/mid.txt", []byte("mid"), 0644))
	check(os.WriteFile(dir+"/sub/deep/leaf.txt", []byte("leaf"), 0644))

	fsys := os.DirFS(dir)

	// Sub on "sub" — read through the subtree.
	sub, err := fs.Sub(fsys, "sub")
	check(err)
	b, err := fs.ReadFile(sub, "mid.txt")
	check(err)
	fmt.Printf("sub-read: %q\n", string(b))

	// The subtree root is opaque: parent paths are rejected.
	_, err = fs.ReadFile(sub, "../top.txt")
	fmt.Printf("escape-err: %v\n", err != nil)

	// Nested Sub.
	deep, err := fs.Sub(sub, "deep")
	check(err)
	b, err = fs.ReadFile(deep, "leaf.txt")
	check(err)
	fmt.Printf("nested-read: %q\n", string(b))

	// Sub on a file (not a dir) — must error.
	_, err = fs.Sub(fsys, "top.txt")
	fmt.Printf("file-err: %v\n", err != nil)

	// Sub on missing dir — must error.
	_, err = fs.Sub(fsys, "missing")
	fmt.Printf("missing-err: %v\n", err != nil)

	// Sub with invalid path — must error.
	_, err = fs.Sub(fsys, "../evil")
	fmt.Printf("invalid-err: %v\n", err != nil)

	// Sub on "." — must error (gc: "cannot Sub(\".\")").
	_, err = fs.Sub(fsys, ".")
	fmt.Printf("dot-err: %v\n", err != nil)

	fmt.Println("ok")
}
