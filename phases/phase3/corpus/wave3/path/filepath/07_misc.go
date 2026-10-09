package main

// 07_misc: VolumeName, ToSlash, FromSlash, SplitList, IsLocal.
import (
	"fmt"
	"path/filepath"
)

func main() {
	// VolumeName is always "" on unix.
	fmt.Printf("volumename: %q %q\n",
		filepath.VolumeName("/a/b"), filepath.VolumeName("a/b"))

	fmt.Printf("toslash: %q fromslash: %q\n",
		filepath.ToSlash("a/b/c"), filepath.FromSlash("a/b/c"))

	fmt.Printf("splitlist: %q\n", filepath.SplitList("a:b:c"))
	fmt.Printf("splitlist-empty: %q\n", filepath.SplitList(""))

	// IsLocal: true only for paths confined to the current directory.
	for _, p := range []string{
		"a/b", "./a", "a/../b", "..", "../a", "/a", "", ".",
	} {
		fmt.Printf("islocal %q: %v\n", p, filepath.IsLocal(p))
	}

	fmt.Println("ok")
}
