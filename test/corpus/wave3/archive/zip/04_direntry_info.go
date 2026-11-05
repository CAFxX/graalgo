// P3-M9 wave3: archive/zip — fs.DirEntry/fs.File interface dispatch:
// fs.ReadDir + DirEntry.Info, Reader.Open(dir) + Stat + Read.
// SCOPE-LIMITED: the m7 desugar rows for fileListEntry.Info,
// openDir.Stat, openDir.Read and headerFileInfo.Info landed in
// review-1105-mr-table-missing (A-P1-11, REVIEW-2026-11-05-15, plus the
// NEEDS-FIX fix-up adding the headerFileInfo.Info row); the carried
// archive/zip FixedZone fix is still open (see
// 04_direntry_info.scopelimited).
package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
)

func main() {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	if _, err := w.Create("sub/"); err != nil {
		fmt.Println("create-dir-err", err)
		return
	}
	fw, err := w.Create("sub/a.txt")
	if err != nil {
		fmt.Println("create-file-err", err)
		return
	}
	fw.Write([]byte("hello"))
	fw2, err := w.Create("top.txt")
	if err != nil {
		fmt.Println("create-top-err", err)
		return
	}
	fw2.Write([]byte("top"))
	if err := w.Close(); err != nil {
		fmt.Println("close-err", err)
		return
	}
	raw := buf.Bytes()

	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		fmt.Println("newreader-err", err)
		return
	}
	fmt.Println("count", len(zr.File) == 3)

	// fs.ReadDir on the zip root: dir + file entries, sorted by name.
	// Each entry's Info() dispatches through the fs.DirEntry interface:
	// fileListEntry.Info for the directory, headerFileInfo.Info for the
	// file.
	ents, err := fs.ReadDir(zr, ".")
	if err != nil {
		fmt.Println("readdir-err", err)
		return
	}
	fmt.Println("readdir", len(ents) == 2, ents[0].Name() == "sub", ents[1].Name() == "top.txt")
	for _, e := range ents {
		fi, err := e.Info()
		if err != nil {
			fmt.Println("info-err", e.Name(), err)
			return
		}
		fmt.Println("info", e.Name(), fi.IsDir() == e.IsDir(), fi.Name() == e.Name())
	}
	// fs.ReadDir inside the directory.
	sub, err := fs.ReadDir(zr, "sub")
	if err != nil {
		fmt.Println("readdir-sub-err", err)
		return
	}
	fmt.Println("readdir-sub", len(sub) == 1, sub[0].Name() == "a.txt")
	sfi, err := sub[0].Info()
	if err != nil {
		fmt.Println("info-sub-err", err)
		return
	}
	fmt.Println("info-sub", sfi.Size() == 5, !sfi.IsDir())

	// Reader.Open on the directory: Stat + Read via the fs.File interface.
	f, err := zr.Open("sub")
	if err != nil {
		fmt.Println("open-dir-err", err)
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		fmt.Println("dirstat-err", err)
		return
	}
	fmt.Println("dirstat", fi.IsDir(), fi.Name() == "sub")
	b := make([]byte, 4)
	n, rerr := f.Read(b)
	var perr *fs.PathError
	fmt.Println("dirread", n == 0, errors.As(rerr, &perr), perr != nil && perr.Op == "read" && perr.Path == "sub" && perr.Err.Error() == "is a directory")

	fmt.Println("ok")
}
