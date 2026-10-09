package main

// 02_glob: fs.Glob over os.DirFS — patterns, character classes,
// bad pattern, no-meta literal.
import (
	"fmt"
	"io/fs"
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
	dir, err := os.MkdirTemp("", "iofs02-")
	check(err)
	defer os.RemoveAll(dir)

	check(os.MkdirAll(dir+"/sub", 0755))
	for _, f := range []string{"a.txt", "b.txt", "c.go", "sub/d.txt", "sub/e.go"} {
		check(os.WriteFile(dir+"/"+f, []byte("x"), 0644))
	}

	fsys := os.DirFS(dir)

	glob := func(pattern string) {
		m, err := fs.Glob(fsys, pattern)
		if err != nil {
			fmt.Printf("glob %q: err=%v\n", pattern, err)
			return
		}
		sort.Strings(m)
		fmt.Printf("glob %q: %q\n", pattern, m)
	}

	glob("*.txt")
	glob("*.go")
	glob("sub/*.txt")
	glob("*")
	// No meta — literal match, nil if missing.
	glob("a.txt")
	glob("missing.txt")
	// Bad pattern — ErrBadPattern.
	glob("[")
	// Invalid path.
	glob("../*.txt")

	fmt.Println("ok")
}
