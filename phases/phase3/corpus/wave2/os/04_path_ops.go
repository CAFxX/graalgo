// 04_path_ops: Mkdir/MkdirAll/Remove/RemoveAll/Rename/Symlink/Readlink/
// Link/Chmod/Chtimes/Truncate. Permission-denied checks skip under root.
package main

import (
	"fmt"
	"os"
	"time"
)

func check(err error) {
	if err != nil {
		fmt.Println("FAIL:", err)
		os.Exit(1)
	}
}

func main() {
	dir, err := os.MkdirTemp("", "os04-")
	check(err)
	defer os.RemoveAll(dir)

	// MkdirAll + RemoveAll.
	deep := dir + "/a/b/c"
	check(os.MkdirAll(deep, 0755))
	if _, err := os.Stat(deep); err != nil {
		fmt.Println("FAIL: mkdirall stat:", err)
		os.Exit(1)
	}
	check(os.RemoveAll(dir + "/a"))
	if _, err := os.Stat(dir + "/a"); !os.IsNotExist(err) {
		fmt.Println("FAIL: removeall")
		os.Exit(1)
	}
	// RemoveAll of a missing path is nil; MkdirAll("") is ENOENT.
	check(os.RemoveAll(dir + "/missing"))
	if err := os.MkdirAll("", 0755); !os.IsNotExist(err) {
		fmt.Println("FAIL: mkdirall empty:", err)
		os.Exit(1)
	}

	// Rename.
	src := dir + "/src.txt"
	check(os.WriteFile(src, []byte("data"), 0644))
	dst := dir + "/dst.txt"
	check(os.Rename(src, dst))
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		fmt.Println("FAIL: rename src gone")
		os.Exit(1)
	}
	b, err := os.ReadFile(dst)
	check(err)
	if string(b) != "data" {
		fmt.Println("FAIL: rename content")
		os.Exit(1)
	}

	// Symlink + Readlink + Link.
	link := dir + "/link"
	check(os.Symlink(dst, link))
	t, err := os.Readlink(link)
	check(err)
	if t != dst {
		fmt.Println("FAIL: readlink =", t)
		os.Exit(1)
	}
	hard := dir + "/hard"
	check(os.Link(dst, hard))
	hi, err := os.Stat(hard)
	check(err)
	di, err := os.Stat(dst)
	check(err)
	if !os.SameFile(hi, di) {
		fmt.Println("FAIL: hardlink SameFile")
		os.Exit(1)
	}

	// Chmod.
	check(os.Chmod(dst, 0600))
	fi, err := os.Stat(dst)
	check(err)
	if fi.Mode().Perm() != 0600 {
		fmt.Println("FAIL: chmod perm =", fi.Mode().Perm())
		os.Exit(1)
	}
	// Chmod with setuid bits: descriptive error, never masked (M8).
	err = os.Chmod(dst, 04755)
	if err == nil {
		fmt.Println("SETUID-OK")
	} else {
		fmt.Println("SETUID-ERR")
	}

	// Chtimes.
	mtime := time.Unix(1700000000, 0)
	check(os.Chtimes(dst, mtime, mtime))
	fi, err = os.Stat(dst)
	check(err)
	if fi.ModTime().Unix() != 1700000000 {
		fmt.Println("FAIL: chtimes =", fi.ModTime().Unix())
		os.Exit(1)
	}

	// Truncate (path form).
	check(os.Truncate(dst, 2))
	fi, err = os.Stat(dst)
	check(err)
	if fi.Size() != 2 {
		fmt.Println("FAIL: truncate size =", fi.Size())
		os.Exit(1)
	}

	// Permission-denied (skip under root: root bypasses permission checks).
	if os.Geteuid() == 0 {
		fmt.Println("SKIP-PERMDENIED (root)")
	} else {
		ro := dir + "/ro"
		check(os.WriteFile(ro, []byte("x"), 0444))
		if _, err := os.OpenFile(ro, os.O_WRONLY, 0); !os.IsPermission(err) {
			fmt.Println("FAIL: perm denied open:", err)
			os.Exit(1)
		}
		if err := os.Mkdir(dir+"/rodir", 0555); err != nil {
			fmt.Println("FAIL: mkdir rodir:", err)
			os.Exit(1)
		}
		if _, err := os.Create(dir + "/rodir/x"); !os.IsPermission(err) {
			fmt.Println("FAIL: perm denied create:", err)
			os.Exit(1)
		}
		fmt.Println("PERMDENIED-OK")
	}
	fmt.Println("ok")
}
