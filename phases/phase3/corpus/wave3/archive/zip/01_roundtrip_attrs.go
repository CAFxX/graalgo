// P3-M9 wave3: archive/zip — attribute round-trip: ExternalAttrs,
// CreatorVersion, file + archive comments, non-UTF8 names, Extra
// fields, duplicate file names readable in order.
// SCOPE-LIMITED: no synthetic archive/zip source in frontend/v1 yet
// (imports are not supported in frontend/v1 scope — WS G import overlay).
package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"time"
)

func main() {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	if err := w.SetComment("archive comment"); err != nil {
		fmt.Println("setcomment-err", err)
		return
	}
	extra := []byte{0x01, 0x02, 0x04, 0x00, 0xde, 0xad, 0xbe, 0xef}
	fw, err := w.CreateHeader(&zip.FileHeader{
		Name:           "attr.txt",
		Method:         zip.Store,
		Modified:       time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		CreatorVersion: 3<<8 | 63, // Unix, version 6.3
		ExternalAttrs:  0o100644 << 16,
		Comment:        "file comment",
		Extra:          extra,
	})
	if err != nil {
		fmt.Println("create-err", err)
		return
	}
	fw.Write([]byte("attrs"))

	// Non-UTF8 name: preserved bytewise, NonUTF8 flag set.
	badName := string([]byte{0xff, 0xfe, 'a'})
	fw2, err := w.Create(badName)
	if err != nil {
		fmt.Println("create-badname-err", err)
		return
	}
	fw2.Write([]byte("bn"))

	// Duplicate names: both entries readable in order.
	for _, content := range []string{"first", "second"} {
		fw3, err := w.Create("dup.txt")
		if err != nil {
			fmt.Println("create-dup-err", err)
			return
		}
		fw3.Write([]byte(content))
	}
	if err := w.Close(); err != nil {
		fmt.Println("close-err", err)
		return
	}

	r, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		fmt.Println("newreader-err", err)
		return
	}
	fmt.Println("archive-comment", r.Comment == "archive comment")

	f0 := r.File[0]
	// The writer keeps the CreatorVersion creator (high) byte from the
	// input but stamps the version (low) byte itself (20 = 2.0); when
	// Modified is set it appends an extended-timestamp extra field
	// (0x5455) after the user's Extra bytes.
	fmt.Println("attrs", f0.CreatorVersion == 3<<8|20,
		f0.ExternalAttrs == 0o100644<<16,
		f0.Comment == "file comment",
		bytes.HasPrefix(f0.Extra, extra))

	f1 := r.File[1]
	fmt.Println("nonutf8", f1.NonUTF8, f1.Name == badName)
	rc1, _ := f1.Open()
	out1, _ := io.ReadAll(rc1)
	rc1.Close()
	fmt.Println("nonutf8-body", string(out1) == "bn")

	var dups []string
	for _, f := range r.File[2:] {
		rc, _ := f.Open()
		out, _ := io.ReadAll(rc)
		rc.Close()
		dups = append(dups, string(out))
	}
	fmt.Println("dups", len(dups) == 2, dups[0] == "first", dups[1] == "second")

	fmt.Println("ok")
}
