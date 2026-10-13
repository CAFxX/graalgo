// 01_errors: the R1 error-identity chain — PathError shape, exact
// message, errors.Is against fs sentinels and os.Is* predicates.
package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

var failures int

func want(cond bool, msg string) {
	if !cond {
		fmt.Println("FAIL:", msg)
		failures++
	}
}

func main() {
	dir, err := os.MkdirTemp("", "os01-")
	if err != nil {
		fmt.Println("FAIL: mktemp:", err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir)
	missing := dir + "/nope"

	// 1. Open missing: PathError{open, path, ENOENT}.
	_, err = os.Open(missing)
	pe, ok := err.(*os.PathError)
	want(ok, "open missing: not a *PathError")
	if ok {
		want(pe.Op == "open", "open missing: op")
		want(pe.Path == missing, "open missing: path kept the user string")
	}
	want(errors.Is(err, fs.ErrNotExist), "open missing: Is fs.ErrNotExist")
	want(errors.Is(err, os.ErrNotExist), "open missing: Is os.ErrNotExist")
	want(os.IsNotExist(err), "open missing: os.IsNotExist")
	want(err.Error() == "open "+missing+": no such file or directory",
		"open missing: message: "+err.Error())

	// 2. Stat missing.
	_, err = os.Stat(missing)
	want(os.IsNotExist(err), "stat missing: os.IsNotExist")
	want(err.Error() == "stat "+missing+": no such file or directory",
		"stat missing: message: "+err.Error())

	// 3. O_EXCL on existing → EEXIST.
	p := dir + "/e"
	f, err := os.Create(p)
	want(err == nil, "create")
	if err == nil {
		f.Close()
	}
	_, err = os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	want(os.IsExist(err), "o_excl: os.IsExist")
	want(errors.Is(err, fs.ErrExist), "o_excl: Is fs.ErrExist")
	want(err.Error() == "open "+p+": file exists", "o_excl: message: "+err.Error())

	// 4. Read on write-only → EBADF.
	wf, err := os.OpenFile(p, os.O_WRONLY, 0)
	want(err == nil, "open write-only")
	if err == nil {
		_, err = wf.Read(make([]byte, 4))
		want(err != nil && err.Error() == "read "+p+": bad file descriptor",
			"read write-only: "+fmt.Sprint(err))
		// 5. Double close → ErrClosed.
		wf.Close()
		err = wf.Close()
		want(errors.Is(err, os.ErrClosed), "double close: Is ErrClosed")
		want(err.Error() == "close "+p+": file already closed",
			"double close: message: "+err.Error())
	}

	// 6. NUL path → EINVAL.
	_, err = os.Open("a\x00b")
	want(err != nil && err.Error() == "open a\x00b: invalid argument",
		"nul path: "+fmt.Sprint(err))

	// 7. Mkdir missing parent → ENOENT.
	err = os.Mkdir(dir+"/nope/x", 0755)
	want(os.IsNotExist(err), "mkdir missing parent")

	// 8. Remove missing → ENOENT.
	err = os.Remove(missing)
	want(os.IsNotExist(err), "remove missing")

	// 9. Chmod missing → ENOENT.
	err = os.Chmod(missing, 0644)
	want(os.IsNotExist(err), "chmod missing")

	// 10. Readlink on a regular file → EINVAL.
	_, err = os.Readlink(p)
	want(err != nil, "readlink regular file")

	// 11. Nil *File receiver → bare ErrInvalid.
	var nf *os.File
	_, err = nf.Read(make([]byte, 4))
	want(errors.Is(err, fs.ErrInvalid), "nil file read")

	// 12. A regular file in a parent position → ENOTDIR (not ENOENT).
	_, err = os.Stat(p + "/x")
	want(err != nil && err.Error() == "stat "+p+"/x: not a directory",
		"stat file parent: "+fmt.Sprint(err))
	_, err = os.Open(p + "/x")
	want(err != nil && err.Error() == "open "+p+"/x: not a directory",
		"open file parent: "+fmt.Sprint(err))

	if failures > 0 {
		os.Exit(1)
	}
	fmt.Println("ok")
}
