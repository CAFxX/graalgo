// os_files.go — package os from upstream source over internal/poll and
// syscall: arguments, environment, files, directories, links, pipes (the
// runtime poller), errors.Is against fs.ErrNotExist, os.Exit.
//
// args: arg1 arg2
// env: FMTPATH_T=tv
package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

func check(what string, err error) {
	if err != nil {
		fmt.Println(what, "ERR:", err)
	}
}

func main() {
	fmt.Println(len(os.Args), os.Args)
	fmt.Println(os.Getenv("FMTPATH_T"), os.Getenv("NOPE_X") == "")
	v, ok := os.LookupEnv("FMTPATH_T")
	fmt.Println(v, ok, len(os.Environ()) > 0)
	dir, err := os.MkdirTemp("", "ggos")
	check("mkdirtemp", err)
	defer os.RemoveAll(dir)
	p := filepath.Join(dir, "a.txt")
	check("write", os.WriteFile(p, []byte("hello\nworld\n"), 0o644))
	b, err := os.ReadFile(p)
	fmt.Printf("%q %v\n", b, err)
	_, err = os.ReadFile(filepath.Join(dir, "nope"))
	fmt.Println(errors.Is(err, fs.ErrNotExist), errors.Is(err, os.ErrNotExist), os.IsNotExist(err))
	var pe *fs.PathError
	fmt.Println(errors.As(err, &pe), pe.Op, filepath.Base(pe.Path), pe.Err)
	f, err := os.Create(filepath.Join(dir, "b.txt"))
	check("create", err)
	n, err := f.WriteString("0123456789")
	fmt.Println(n, err)
	off, err := f.Seek(3, io.SeekStart)
	fmt.Println(off, err)
	n, err = f.Write([]byte("XY"))
	fmt.Println(n, err)
	n, err = f.WriteAt([]byte("Z"), 9)
	fmt.Println(n, err)
	check("sync", f.Sync())
	st, err := f.Stat()
	fmt.Println(st.Name(), st.Size(), st.Mode(), st.IsDir(), err)
	check("close", f.Close())
	fmt.Println(f.Close() != nil)
	f, err = os.Open(filepath.Join(dir, "b.txt"))
	check("open", err)
	buf := make([]byte, 4)
	for {
		n, err := f.Read(buf)
		fmt.Printf("%d %q %v\n", n, buf[:n], err)
		if err != nil {
			break
		}
	}
	n, err = f.ReadAt(buf, 2)
	fmt.Printf("%d %q %v\n", n, buf[:n], err)
	f.Close()
	check("mkdir", os.Mkdir(filepath.Join(dir, "sub"), 0o755))
	check("mkdirall", os.MkdirAll(filepath.Join(dir, "x", "y", "z"), 0o755))
	check("rename", os.Rename(p, filepath.Join(dir, "sub", "c.txt")))
	ents, err := os.ReadDir(dir)
	check("readdir", err)
	var names []string
	for _, e := range ents {
		names = append(names, fmt.Sprint(e.Name(), ":", e.IsDir()))
	}
	sort.Strings(names)
	fmt.Println(names)
	fi, err := os.Stat(filepath.Join(dir, "sub", "c.txt"))
	fmt.Println(fi.Size(), fi.Mode().IsRegular(), fi.Mode().Perm(), err)
	_, err = os.Stat(filepath.Join(dir, "zzz"))
	fmt.Println(os.IsNotExist(err))
	check("truncate", os.Truncate(filepath.Join(dir, "sub", "c.txt"), 3))
	b, _ = os.ReadFile(filepath.Join(dir, "sub", "c.txt"))
	fmt.Printf("%q\n", b)
	check("symlink", os.Symlink("c.txt", filepath.Join(dir, "sub", "l")))
	l, err := os.Readlink(filepath.Join(dir, "sub", "l"))
	fmt.Println(l, err)
	check("chmod", os.Chmod(filepath.Join(dir, "sub", "c.txt"), 0o600))
	fi, _ = os.Lstat(filepath.Join(dir, "sub", "l"))
	fmt.Println(fi.Mode()&fs.ModeSymlink != 0)
	check("remove", os.Remove(filepath.Join(dir, "sub", "l")))
	err = os.Remove(filepath.Join(dir, "sub"))
	fmt.Println(err != nil)
	wd, err := os.Getwd()
	fmt.Println(wd != "", err)
	check("chdir", os.Chdir(dir))
	wd2, _ := os.Getwd()
	fmt.Println(filepath.Base(wd2) == filepath.Base(dir))
	r, w, err := os.Pipe()
	check("pipe", err)
	go func() { w.Write([]byte("through pipe")); w.Close() }()
	pb, err := io.ReadAll(r)
	fmt.Printf("%q %v\n", pb, err)
	fmt.Println(os.Getpid() > 0, os.Getuid() >= 0, os.Getppid() > 0)
	h, err := os.Hostname()
	fmt.Println(h != "", err)
	var walked []string
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		rel, _ := filepath.Rel(dir, path)
		walked = append(walked, rel)
		return nil
	})
	fmt.Println(walked)
	fmt.Fprintln(os.Stderr, "bye")
	os.Exit(3)
}
