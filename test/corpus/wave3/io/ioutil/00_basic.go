package main

// L11 (REVIEW-2026-11-18-05): io/ioutil — zero corpus imports before
// this program. The deprecated-but-present file helpers: WriteFile /
// ReadFile round-trip, TempDir / TempFile, ReadAll, ReadDir, and
// NopCloser. Paths are never printed (temp dirs are random); only
// contents, sizes, and booleans.

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	dir, err := ioutil.TempDir("", "ioutil-demo")
	if err != nil {
		fmt.Printf("tempdir-err=%v\n", err != nil)
		return
	}
	defer os.RemoveAll(dir)
	p := filepath.Join(dir, "hello.txt")
	if err := ioutil.WriteFile(p, []byte("hello ioutil\n"), 0644); err != nil {
		fmt.Printf("writefile-err=%v\n", err != nil)
		return
	}
	data, err := ioutil.ReadFile(p)
	fmt.Printf("readfile-err=%v content=%q\n", err != nil, string(data))
	// TempFile with a pattern; the name is not printed.
	f, err := ioutil.TempFile(dir, "tmp-*.txt")
	if err != nil {
		fmt.Printf("tempfile-err=%v\n", err != nil)
		return
	}
	if _, err := f.Write([]byte("temp content")); err != nil {
		fmt.Printf("tempfile-write-err=%v\n", err != nil)
		return
	}
	f.Close()
	// ReadDir lists both files (names sorted, sizes pinned).
	entries, err := ioutil.ReadDir(dir)
	if err != nil {
		fmt.Printf("readdir-err=%v\n", err != nil)
		return
	}
	// Names are random (TempFile pattern), so print only the
	// temp-prefix flag, size, and IsDir, sorted by size then flag.
	sort.Slice(entries, func(a, b int) bool {
		if entries[a].Size() != entries[b].Size() {
			return entries[a].Size() < entries[b].Size()
		}
		return entries[a].Name() < entries[b].Name()
	})
	fmt.Printf("readdir-count=%d\n", len(entries))
	for _, e := range entries {
		fmt.Printf("readdir temp=%v size=%d isdir=%v\n", strings.HasPrefix(e.Name(), "tmp-"), e.Size(), e.IsDir())
	}
	// ReadAll + NopCloser.
	all, err := ioutil.ReadAll(ioutil.NopCloser(strings.NewReader("stream-data")))
	fmt.Printf("readall-err=%v content=%q\n", err != nil, string(all))
	// Error paths.
	_, err = ioutil.ReadFile(filepath.Join(dir, "missing.txt"))
	fmt.Printf("readfile-missing-err=%v\n", err != nil)
	_, err = ioutil.ReadDir(filepath.Join(dir, "missing"))
	fmt.Printf("readdir-missing-err=%v\n", err != nil)
}
