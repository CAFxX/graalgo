package main

// 01_walkdir: fs.WalkDir over os.DirFS — full traversal order,
// SkipDir pruning, error propagation.
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
	dir, err := os.MkdirTemp("", "iofs01-")
	check(err)
	defer os.RemoveAll(dir)

	check(os.MkdirAll(dir+"/a/b", 0755))
	check(os.MkdirAll(dir+"/c", 0755))
	check(os.WriteFile(dir+"/a/f1.txt", []byte("1"), 0644))
	check(os.WriteFile(dir+"/a/b/f2.txt", []byte("2"), 0644))
	check(os.WriteFile(dir+"/c/f3.txt", []byte("3"), 0644))
	check(os.WriteFile(dir+"/top.txt", []byte("t"), 0644))

	fsys := os.DirFS(dir)

	// Full walk — collect and sort (walk order is lexical within a dir,
	// but sort for determinism).
	var walked []string
	err = fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		walked = append(walked, path)
		return nil
	})
	check(err)
	sort.Strings(walked)
	fmt.Printf("walk: %q\n", walked)

	// SkipDir: prune directory "a".
	var pruned []string
	err = fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == "a" {
			return fs.SkipDir
		}
		pruned = append(pruned, path)
		return nil
	})
	check(err)
	sort.Strings(pruned)
	fmt.Printf("pruned: %q\n", pruned)

	// Walk a missing root — fn receives the error.
	var gotErr error
	err = fs.WalkDir(fsys, "missing", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			gotErr = err
			return nil
		}
		return nil
	})
	check(err)
	fmt.Printf("missing-root-err: %v\n", gotErr != nil)

	// Callback error aborts the walk.
	sentinel := fmt.Errorf("stop")
	err = fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		return sentinel
	})
	fmt.Printf("abort-err-is-sentinel: %v\n", err == sentinel)

	fmt.Println("ok")
}
