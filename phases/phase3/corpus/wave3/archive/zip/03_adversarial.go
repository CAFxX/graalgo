// P3-M9 wave3: archive/zip — adversarial: empty archive, 0-length file,
// data-descriptor streaming write via a non-io.Seeker writer,
// zip-slip-style "../" names NOT sanitized (documented non-behavior),
// 64KB comment rejection vs 65535 acceptance.
// SCOPE-LIMITED: no synthetic archive/zip source in frontend/v1 yet
// (imports are not supported in frontend/v1 scope — WS G import overlay).
package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"strings"
	"time"
)

func main() {
	// Empty archive: valid, zero files.
	var b0 bytes.Buffer
	w0 := zip.NewWriter(&b0)
	if err := w0.Close(); err != nil {
		fmt.Println("empty-close-err", err)
		return
	}
	r0, err := zip.NewReader(bytes.NewReader(b0.Bytes()), int64(b0.Len()))
	fmt.Println("empty", err == nil, len(r0.File) == 0)

	// 0-length file round-trips (CRC32 of empty is 0).
	var b1 bytes.Buffer
	w1 := zip.NewWriter(&b1)
	fw1, _ := w1.CreateHeader(&zip.FileHeader{Name: "empty.txt", Method: zip.Deflate,
		Modified: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)})
	fw1.Write(nil)
	w1.Close()
	r1, _ := zip.NewReader(bytes.NewReader(b1.Bytes()), int64(b1.Len()))
	f1 := r1.File[0]
	fmt.Println("zerolen", f1.UncompressedSize64 == 0, f1.CRC32 == 0)

	// Data-descriptor path: bytes.Buffer is not an io.Seeker, so the
	// writer must use a data descriptor (signature 0x08074b50). Read-back
	// is still exact.
	var b2 bytes.Buffer
	w2 := zip.NewWriter(&b2)
	fw2, _ := w2.CreateHeader(&zip.FileHeader{Name: "d", Method: zip.Deflate,
		Modified: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)})
	fw2.Write([]byte("descriptor-data"))
	w2.Close()
	raw2 := b2.Bytes()
	r2, _ := zip.NewReader(bytes.NewReader(raw2), int64(len(raw2)))
	f2 := r2.File[0]
	rc2, _ := f2.Open()
	out2, _ := io.ReadAll(rc2)
	rc2.Close()
	fmt.Println("datadesc", bytes.Contains(raw2, []byte{0x50, 0x4b, 0x07, 0x08}),
		f2.UncompressedSize64 == 15, string(out2) == "descriptor-data")

	// Zip-slip-style names are NOT sanitized by Reader (documented
	// non-behavior — callers must sanitize).
	var b3 bytes.Buffer
	w3 := zip.NewWriter(&b3)
	fw3, _ := w3.Create("../evil.txt")
	fw3.Write([]byte("evil"))
	fw3b, _ := w3.Create("/abs.txt")
	fw3b.Write([]byte("abs"))
	w3.Close()
	r3, _ := zip.NewReader(bytes.NewReader(b3.Bytes()), int64(b3.Len()))
	fmt.Println("nosanitize", r3.File[0].Name == "../evil.txt",
		r3.File[1].Name == "/abs.txt")

	// 65536-byte comment rejected; 65535 accepted and round-trips.
	var b4 bytes.Buffer
	w4 := zip.NewWriter(&b4)
	w4.Create("f")
	err = w4.SetComment(strings.Repeat("c", 65536))
	fmt.Println("comment-64k", err != nil)
	w4.Close()

	var b5 bytes.Buffer
	w5 := zip.NewWriter(&b5)
	w5.Create("f")
	if err := w5.SetComment(strings.Repeat("c", 65535)); err != nil {
		fmt.Println("comment-65535-err", err)
		return
	}
	w5.Close()
	r5, _ := zip.NewReader(bytes.NewReader(b5.Bytes()), int64(b5.Len()))
	fmt.Println("comment-65535", len(r5.Comment) == 65535,
		r5.Comment == strings.Repeat("c", 65535))

	fmt.Println("ok")
}
