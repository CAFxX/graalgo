package main

// 01_base_dir_ext: filepath.Base, Dir, Ext, Split — edge cases with
// trailing slashes, dots, empty strings.
import (
	"fmt"
	"path/filepath"
)

func main() {
	paths := []string{
		"",
		".",
		"/",
		"a",
		"a/",
		"/a/b/c.txt",
		"/a/b/",
		"a.b.c",
		".hidden",
		"a.",
		"..",
		"a/b/.",
	}
	for _, p := range paths {
		fmt.Printf("path %q: base=%q dir=%q ext=%q\n",
			p, filepath.Base(p), filepath.Dir(p), filepath.Ext(p))
	}
	for _, p := range []string{"a/b/c", "a/b/", "a", "/"} {
		d, f := filepath.Split(p)
		fmt.Printf("split %q: dir=%q file=%q\n", p, d, f)
	}
	fmt.Println("ok")
}
