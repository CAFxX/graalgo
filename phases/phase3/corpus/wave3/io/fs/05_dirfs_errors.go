package main

// 05_dirfs_errors: os.DirFS error paths — empty root, invalid names,
// PathError path rewriting (fs-relative, never the joined path).
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
	dir, err := os.MkdirTemp("", "iofs05-")
	check(err)
	defer os.RemoveAll(dir)
	check(os.WriteFile(dir+"/f.txt", []byte("x"), 0644))

	// Empty root — gc: "os: DirFS with empty root".
	efs := os.DirFS("")
	_, err = fs.ReadFile(efs, "f.txt")
	if err != nil {
		fmt.Printf("empty-root-err: %q\n", err.Error())
	} else {
		fmt.Printf("empty-root-err: <nil>\n")
	}

	// Invalid name (absolute) — must error.
	fsys := os.DirFS(dir)
	_, err = fs.Stat(fsys, "/abs")
	fmt.Printf("abs-err: %v\n", err != nil)

	// NUL byte in name — must error (not a valid path element).
	_, err = fs.Stat(fsys, "a\x00b")
	fmt.Printf("nul-err: %v\n", err != nil)

	// PathError reports the fs-relative name, not the joined path.
	_, err = fs.Stat(fsys, "missing.txt")
	if pe, ok := err.(*fs.PathError); ok {
		fmt.Printf("patherror: op=%q path=%q\n", pe.Op, pe.Path)
	} else {
		fmt.Printf("patherror: not-a-PathError: %T\n", err)
	}

	// Open returns an fs.File; Read + Stat + Close work through it.
	f, err := fsys.Open("f.txt")
	check(err)
	buf := make([]byte, 8)
	n, err := f.Read(buf)
	fmt.Printf("open-read: n=%d data=%q err=%v\n", n, string(buf[:n]), err)
	fi, err := f.Stat()
	check(err)
	fmt.Printf("open-stat: size=%d\n", fi.Size())
	check(f.Close())

	fmt.Println("ok")
}
