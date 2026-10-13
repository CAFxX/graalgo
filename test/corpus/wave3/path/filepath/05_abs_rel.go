package main

// 05_abs_rel: filepath.Abs, Rel, IsAbs — relative-to-cwd resolution,
// error cases (Rel with relative base, unresolvable).
import (
	"fmt"
	"os"
	"path/filepath"
)

func check(err error) {
	if err != nil {
		fmt.Println("FAIL:", err)
		os.Exit(1)
	}
}

func main() {
	dir, err := os.MkdirTemp("", "fp05-")
	check(err)
	defer os.RemoveAll(dir)

	oldwd, err := os.Getwd()
	check(err)
	check(os.Chdir(dir))
	defer os.Chdir(oldwd)

	// Abs of a relative path joins with cwd; print only the tail.
	abs, err := filepath.Abs("sub/file.txt")
	check(err)
	fmt.Printf("abs-tail: %q isabs=%v\n", filepath.Base(abs), filepath.IsAbs(abs))

	// Abs of an already-absolute path just cleans.
	abs2, err := filepath.Abs("/a//b/./c")
	check(err)
	fmt.Printf("abs-clean: %q\n", abs2)

	fmt.Printf("isabs: /a=%v a=%v empty=%v\n",
		filepath.IsAbs("/a"), filepath.IsAbs("a"), filepath.IsAbs(""))

	// Rel computations.
	for _, c := range [][2]string{
		{"/a/b", "/a/b/c"},
		{"/a/b/c", "/a/b"},
		{"/a/b", "/a/c/d"},
		{"/a", "/a"},
		{"/", "/a/b"},
	} {
		rel, err := filepath.Rel(c[0], c[1])
		fmt.Printf("rel %q %q: %q err=%v\n", c[0], c[1], rel, err)
	}

	// Rel with a relative base — must error.
	_, err = filepath.Rel("a", "/b")
	fmt.Printf("rel-relbase-err: %v\n", err != nil)

	// Rel to itself.
	rel, err := filepath.Rel("/a/b", "/a/b")
	check(err)
	fmt.Printf("rel-self: %q\n", rel)

	fmt.Println("ok")
}
