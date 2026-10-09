package main

// 04_walk: filepath.Walk and filepath.WalkDir on a hermetic temp tree —
// traversal order, SkipDir, error propagation, symlink handling.
import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func check(err error) {
	if err != nil {
		fmt.Println("FAIL:", err)
		os.Exit(1)
	}
}

func main() {
	dir, err := os.MkdirTemp("", "fp04-")
	check(err)
	defer os.RemoveAll(dir)

	check(os.MkdirAll(dir+"/a/b", 0755))
	check(os.MkdirAll(dir+"/c", 0755))
	check(os.WriteFile(dir+"/a/f1.txt", []byte("1"), 0644))
	check(os.WriteFile(dir+"/a/b/f2.txt", []byte("2"), 0644))
	check(os.WriteFile(dir+"/c/f3.txt", []byte("3"), 0644))
	check(os.WriteFile(dir+"/top.txt", []byte("t"), 0644))
	check(os.Symlink("top.txt", dir+"/link"))

	strip := func(paths []string) []string {
		for i, p := range paths {
			if p == dir {
				paths[i] = "."
			} else {
				paths[i] = strings.TrimPrefix(p, dir+"/")
			}
		}
		sort.Strings(paths)
		return paths
	}

	// Walk (FileInfo variant).
	var walked []string
	err = filepath.Walk(dir, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		walked = append(walked, path)
		return nil
	})
	check(err)
	fmt.Printf("walk: %q\n", strip(walked))

	// WalkDir (DirEntry variant).
	var dwalked []string
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		dwalked = append(dwalked, path)
		return nil
	})
	check(err)
	fmt.Printf("walkdir: %q\n", strip(dwalked))

	// Walk does not follow the symlink (link reported, not traversed).
	var sawLink bool
	err = filepath.Walk(dir, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			sawLink = true
		}
		return nil
	})
	check(err)
	fmt.Printf("saw-symlink: %v\n", sawLink)

	// SkipDir prunes.
	var pruned []string
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == "a" {
			return filepath.SkipDir
		}
		pruned = append(pruned, path)
		return nil
	})
	check(err)
	fmt.Printf("pruned: %q\n", strip(pruned))

	// Missing root: fn gets the error.
	var gotErr error
	err = filepath.Walk(filepath.Join([]string{dir, "missing"}...), func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			gotErr = err
		}
		return nil
	})
	check(err)
	fmt.Printf("missing-root-err: %v\n", gotErr != nil)

	fmt.Println("ok")
}
