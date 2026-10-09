package main

// 06_evalsymlinks: filepath.EvalSymlinks — relative-path symlinks,
// chains, missing target, non-symlink passthrough.
//
// SCOPE NOTE: absolute-path EvalSymlinks is scope-limited (os.Lstat
// fails for /home, /usr, / on the graalgo leg — the os worker's bug,
// retest trigger below). This program uses relative paths via Chdir.
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
	dir, err := os.MkdirTemp("", "fp06-")
	check(err)
	defer os.RemoveAll(dir)

	check(os.Mkdir(dir+"/real", 0755))
	check(os.WriteFile(dir+"/real/f.txt", []byte("x"), 0644))
	// link1 -> real/f.txt (relative), link2 -> link1 (chain).
	check(os.Symlink("real/f.txt", dir+"/link1"))
	check(os.Symlink("link1", dir+"/link2"))

	oldwd, err := os.Getwd()
	check(err)
	check(os.Chdir(dir))
	defer os.Chdir(oldwd)

	ev, err := filepath.EvalSymlinks("link1")
	check(err)
	fmt.Printf("link1: %q\n", ev)

	ev, err = filepath.EvalSymlinks("link2")
	check(err)
	fmt.Printf("link2-chain: %q\n", ev)

	// Non-symlink passes through (cleaned).
	ev, err = filepath.EvalSymlinks("real/./f.txt")
	check(err)
	fmt.Printf("passthrough: %q\n", ev)

	// Missing target — must error.
	_, err = filepath.EvalSymlinks("missing")
	fmt.Printf("missing-err: %v\n", err != nil)

	fmt.Println("ok")
}
