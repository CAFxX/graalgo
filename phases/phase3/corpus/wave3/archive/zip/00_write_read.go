// P3-M9 wave3: archive/zip — write+read: files + dirs, Store and
// Deflate, header fields (Name, sizes, CRC32, Modified 2s truncation),
// OpenReader on a hermetic temp-dir file. Pure Go; Deflate via std.
// SCOPE-LIMITED: no synthetic archive/zip source in frontend/v1 yet
// (imports are not supported in frontend/v1 scope — WS G import overlay).
package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"time"
)

func main() {
	modTime := time.Date(2026, 10, 9, 16, 0, 1, 0, time.UTC) // odd second
	deflated := bytes.Repeat([]byte("abcdef"), 5000)          // 30000 bytes
	stored := []byte("stored-bytes")

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	fw, err := w.CreateHeader(&zip.FileHeader{
		Name: "deflated.txt", Method: zip.Deflate, Modified: modTime})
	if err != nil {
		fmt.Println("create-deflated-err", err)
		return
	}
	fw.Write(deflated)
	fw2, err := w.CreateHeader(&zip.FileHeader{
		Name: "stored.bin", Method: zip.Store, Modified: modTime})
	if err != nil {
		fmt.Println("create-stored-err", err)
		return
	}
	fw2.Write(stored)
	if _, err := w.Create("sub/"); err != nil {
		fmt.Println("create-dir-err", err)
		return
	}
	if err := w.Close(); err != nil {
		fmt.Println("close-err", err)
		return
	}
	raw := buf.Bytes()

	r, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		fmt.Println("newreader-err", err)
		return
	}
	fmt.Println("count", len(r.File) == 3)

	f0 := r.File[0]
	fmt.Println("f0", f0.Name == "deflated.txt", f0.Method == zip.Deflate,
		f0.UncompressedSize64 == uint64(len(deflated)),
		f0.CRC32 == crc32.ChecksumIEEE(deflated))
	// MS-DOS time has 2s granularity: odd second 01 -> 00.
	fmt.Println("f0-time", f0.Modified.Second() == 0,
		f0.Modified.Year() == 2026, f0.Modified.Minute() == 0)
	rc0, _ := f0.Open()
	out0, _ := io.ReadAll(rc0)
	rc0.Close()
	fmt.Println("f0-body", bytes.Equal(out0, deflated))

	f1 := r.File[1]
	fmt.Println("f1", f1.Name == "stored.bin", f1.Method == zip.Store,
		f1.UncompressedSize64 == uint64(len(stored)),
		f1.CompressedSize64 == uint64(len(stored)))

	f2 := r.File[2]
	fmt.Println("f2-dir", f2.Name == "sub/", f2.UncompressedSize64 == 0)

	// OpenReader on a hermetic temp-dir file.
	dir, err := os.MkdirTemp("", "ziptest")
	if err != nil {
		fmt.Println("mktemp-err", err)
		return
	}
	defer os.RemoveAll(dir)
	zpath := filepath.Join(dir, "a.zip")
	if err := os.WriteFile(zpath, raw, 0o644); err != nil {
		fmt.Println("writefile-err", err)
		return
	}
	rr, err := zip.OpenReader(zpath)
	if err != nil {
		fmt.Println("openreader-err", err)
		return
	}
	names := []string{rr.File[0].Name, rr.File[1].Name, rr.File[2].Name}
	rr.Close()
	fmt.Println("openreader", len(names) == 3,
		names[0] == "deflated.txt", names[2] == "sub/")

	fmt.Println("ok")
}
