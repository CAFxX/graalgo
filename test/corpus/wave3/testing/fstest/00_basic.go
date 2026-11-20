package main

// L11 (REVIEW-2026-11-18-05): testing/fstest — zero corpus imports
// before this program. An in-memory file system: MapFS with files
// and a directory, read via the io/fs API (ReadFile, Glob, WalkDir,
// Stat, ReadDir, Sub), plus the missing-file error paths.

import (
	"fmt"
	"io/fs"
	"sort"
	"testing/fstest"
	"time"
)

func main() {
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	m := fstest.MapFS{
		"hello.txt":     &fstest.MapFile{Data: []byte("hello\n"), Mode: 0644, ModTime: when},
		"dir/a.txt":     &fstest.MapFile{Data: []byte("aaa"), Mode: 0644, ModTime: when},
		"dir/b.md":      &fstest.MapFile{Data: []byte("# b\n"), Mode: 0644, ModTime: when},
		"dir/sub/c.txt": &fstest.MapFile{Data: []byte("ccc"), Mode: 0600, ModTime: when},
	}
	data, err := fs.ReadFile(m, "hello.txt")
	fmt.Printf("readfile-err=%v content=%q\n", err != nil, string(data))
	matches, err := fs.Glob(m, "dir/*.txt")
	fmt.Printf("glob-err=%v matches=%q\n", err != nil, matches)
	matches, err = fs.Glob(m, "dir/**/*.txt")
	fmt.Printf("glob-deep-err=%v matches=%q\n", err != nil, matches)
	info, err := fs.Stat(m, "dir/a.txt")
	fmt.Printf("stat-err=%v size=%d mode=%v isdir=%v\n", err != nil, info.Size(), info.Mode(), info.IsDir())
	info, err = fs.Stat(m, "dir")
	fmt.Printf("stat-dir-err=%v isdir=%v\n", err != nil, info.IsDir())
	entries, err := fs.ReadDir(m, "dir")
	fmt.Printf("readdir-err=%v count=%d\n", err != nil, len(entries))
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	fmt.Printf("readdir-names=%q\n", names)
	var walked []string
	err = fs.WalkDir(m, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		walked = append(walked, path)
		return nil
	})
	sort.Strings(walked)
	fmt.Printf("walk-err=%v paths=%q\n", err != nil, walked)
	sub, err := fs.Sub(m, "dir")
	fmt.Printf("sub-err=%v\n", err != nil)
	if err == nil {
		d2, err := fs.ReadFile(sub, "a.txt")
		fmt.Printf("sub-read-err=%v content=%q\n", err != nil, string(d2))
	}
	_, err = fs.ReadFile(m, "missing.txt")
	fmt.Printf("missing-err=%v\n", err != nil)
	_, err = fs.Stat(m, "missing.txt")
	fmt.Printf("stat-missing-err=%v\n", err != nil)
}
