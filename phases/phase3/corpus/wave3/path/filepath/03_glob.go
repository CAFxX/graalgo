package main

// 03_glob: filepath.Glob on a hermetic temp tree — patterns, sorted
// output, bad pattern, no-meta literal, meta in dir part.
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
	dir, err := os.MkdirTemp("", "fp03-")
	check(err)
	defer os.RemoveAll(dir)

	check(os.MkdirAll(dir+"/sub", 0755))
	for _, f := range []string{"a.txt", "b.txt", "c.go", "sub/d.txt", "sub/e.go"} {
		check(os.WriteFile(dir+"/"+f, []byte("x"), 0644))
	}

	glob := func(pattern string) {
		m, err := filepath.Glob(pattern)
		if err != nil {
			fmt.Printf("glob: err=%v\n", err)
			return
		}
		// Strip the temp-dir prefix for determinism.
		for i, s := range m {
			m[i] = s[len(dir)+1:]
		}
		fmt.Printf("glob: %q\n", m)
	}

	glob(filepath.Join([]string{dir, "*.txt"}...))
	glob(filepath.Join([]string{dir, "*.go"}...))
	glob(filepath.Join([]string{dir, "sub", "*.txt"}...))
	glob(filepath.Join([]string{dir, "*"}...))
	// Bad pattern.
	_, err = filepath.Glob(filepath.Join([]string{dir, "["}...))
	fmt.Printf("bad-pattern-err: %v\n", err)
	// No meta, existing file — returns it.
	glob(filepath.Join([]string{dir, "a.txt"}...))
	// No meta, missing file — nil, no error.
	m, err := filepath.Glob(filepath.Join([]string{dir, "missing.txt"}...))
	fmt.Printf("no-meta-missing: m=%v err=%v\n", m, err)

	fmt.Println("ok")
}
