package main

import (
	"embed"
	"fmt"
	"io/fs"
)

//go:embed testdata/hello.txt
var hello string

//go:embed testdata/data.bin
var data []byte

//go:embed testdata
var fsys embed.FS

// SCOPE-LIMITED: the gc leg proves this program is valid, deterministic
// Go. The graalgo leg is skipped: the frontend does not materialize
// //go:embed directives (see 00_embed.go.scopelimited).
func main() {
	fmt.Println(hello)
	fmt.Println(len(data))
	fmt.Println(string(data))

	b, err := fsys.ReadFile("testdata/hello.txt")
	fmt.Println(err == nil)
	fmt.Println(string(b))

	entries, err := fsys.ReadDir("testdata")
	fmt.Println(err == nil)
	fmt.Println(len(entries))
	for _, e := range entries {
		fmt.Println(e.Name(), e.IsDir(), e.Type().IsRegular())
	}

	subentries, err := fsys.ReadDir("testdata/sub")
	fmt.Println(err == nil)
	fmt.Println(len(subentries))
	fmt.Println(subentries[0].Name())

	matches, err := fsys.Glob("testdata/*.txt")
	fmt.Println(err == nil)
	fmt.Println(matches)

	sub, err := fsys.Sub("testdata")
	fmt.Println(err == nil)
	sb, err := fs.ReadFile(sub, "hello.txt")
	fmt.Println(err == nil)
	fmt.Println(string(sb))

	f, err := fsys.Open("testdata/data.bin")
	fmt.Println(err == nil)
	fi, err := f.Stat()
	fmt.Println(err == nil)
	fmt.Println(fi.Size(), fi.IsDir())
	f.Close()

	_, err = fsys.ReadFile("testdata/missing.txt")
	fmt.Println(err != nil)
}
