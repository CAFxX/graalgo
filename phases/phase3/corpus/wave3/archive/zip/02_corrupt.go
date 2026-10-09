// P3-M9 wave3: archive/zip — corrupt-input behavior: truncated central
// directory, flipped data bit, unknown method, wrong NewReader size.
// Error identity is pinned (errors.Is where the stdlib defines values).
// SCOPE-LIMITED: no synthetic archive/zip source in frontend/v1 yet
// (imports are not supported in frontend/v1 scope — WS G import overlay).
package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"time"
)

func makeZip() []byte {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	fw, _ := w.CreateHeader(&zip.FileHeader{Name: "f.txt", Method: zip.Deflate,
		Modified: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)})
	fw.Write(bytes.Repeat([]byte("z"), 1000))
	w.Close()
	return buf.Bytes()
}

func main() {
	good := makeZip()

	// Truncated central directory -> ErrFormat.
	_, err := zip.NewReader(bytes.NewReader(good[:len(good)-100]), int64(len(good)-100))
	fmt.Println("trunc-cd", errors.Is(err, zip.ErrFormat))

	// Empty input -> ErrFormat.
	_, err = zip.NewReader(bytes.NewReader(nil), 0)
	fmt.Println("empty", errors.Is(err, zip.ErrFormat))

	// Flipped data bit: directory parses, body read hits ErrChecksum.
	bad := bytes.Clone(good)
	bad[60] ^= 0xff
	r, err := zip.NewReader(bytes.NewReader(bad), int64(len(bad)))
	if err != nil {
		fmt.Println("flip-newreader-err", err)
		return
	}
	rc, err := r.File[0].Open()
	if err != nil {
		fmt.Println("flip-open-err", err)
		return
	}
	_, err = io.ReadAll(rc)
	rc.Close()
	fmt.Println("flip-checksum", errors.Is(err, zip.ErrChecksum))

	// Unknown method is rejected at write time.
	var b1 bytes.Buffer
	w1 := zip.NewWriter(&b1)
	_, err = w1.CreateHeader(&zip.FileHeader{Name: "x", Method: 99})
	fmt.Println("unknown-method", err != nil)
	w1.Close()

	// NewReader with an oversized size still works (EOCD is at the end).
	_, err = zip.NewReader(bytes.NewReader(good), int64(len(good)+100))
	fmt.Println("size-big", err == nil)

	// NewReader with an undersized size fails.
	_, err = zip.NewReader(bytes.NewReader(good), int64(len(good)-200))
	fmt.Println("size-small", err != nil)

	fmt.Println("ok")
}
