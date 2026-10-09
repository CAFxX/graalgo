package main

// 04_readdir_stat: fs.ReadDir and fs.Stat over os.DirFS — entry
// types, sorting, FileInfo surface, missing paths.
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
	dir, err := os.MkdirTemp("", "iofs04-")
	check(err)
	defer os.RemoveAll(dir)

	check(os.Mkdir(dir+"/sdir", 0755))
	check(os.WriteFile(dir+"/b.txt", []byte("bb"), 0644))
	check(os.WriteFile(dir+"/a.txt", []byte("a"), 0644))

	fsys := os.DirFS(dir)

	// ReadDir returns entries sorted by filename.
	des, err := fs.ReadDir(fsys, ".")
	check(err)
	var names []string
	for _, de := range des {
		names = append(names, de.Name())
	}
	fmt.Printf("readdir: %q sorted=%v\n", names, sort.StringsAreSorted(names))

	// DirEntry details.
	for _, de := range des {
		info, err := de.Info()
		check(err)
		fmt.Printf("entry %q: isdir=%v size=%d mode-isdir=%v\n",
			de.Name(), de.IsDir(), info.Size(), info.Mode().IsDir())
	}

	// ReadDir on missing dir — must error.
	_, err = fs.ReadDir(fsys, "missing")
	fmt.Printf("readdir-missing-err: %v\n", err != nil)

	// Stat a file.
	fi, err := fs.Stat(fsys, "a.txt")
	check(err)
	fmt.Printf("stat: name=%q size=%d isdir=%v\n", fi.Name(), fi.Size(), fi.IsDir())

	// Stat a dir.
	fi, err = fs.Stat(fsys, "sdir")
	check(err)
	fmt.Printf("stat-dir: name=%q isdir=%v\n", fi.Name(), fi.IsDir())

	// Stat missing — must error.
	_, err = fs.Stat(fsys, "missing")
	fmt.Printf("stat-missing-err: %v\n", err != nil)

	fmt.Println("ok")
}
