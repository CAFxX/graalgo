package main

import (
	"fmt"
	"path"
)

func main() {
	// Clean.
	fmt.Println(path.Clean(""))
	fmt.Println(path.Clean("/"))
	fmt.Println(path.Clean("a/b/c"))
	fmt.Println(path.Clean("a//b///c"))
	fmt.Println(path.Clean("a/./b/../c"))
	fmt.Println(path.Clean("../a"))
	// Split (2-result via Appendix M).
	dir, file := path.Split("/a/b/c")
	fmt.Println(dir, file)
	dir, file = path.Split("a")
	fmt.Println(dir, file)
	dir, file = path.Split("/a/b/")
	fmt.Println(dir, file)
	// Join.
	fmt.Println(path.Join("a", "b", "c"))
	fmt.Println(path.Join("/a", "b", "/c"))
	fmt.Println(path.Join())
	// Ext, Base, Dir, IsAbs.
	fmt.Println(path.Ext("/a/b/c.txt"))
	fmt.Println(path.Ext("c"))
	fmt.Println(path.Base("/a/b/c.txt"))
	fmt.Println(path.Base("/"))
	fmt.Println(path.Dir("/a/b/c"))
	fmt.Println(path.Dir("a"))
	fmt.Println(path.IsAbs("/a"))
	fmt.Println(path.IsAbs("a"))
}
