// P3-M9 wave3: archive/tar — corrupt-input behavior: truncated stream,
// bad header checksum, trailing garbage after the end-of-archive marker,
// Next past end. Pure Go over bytes.Buffer; error identity is pinned.
// SCOPE-LIMITED: no synthetic archive/tar source in frontend/v1 yet
// (imports are not supported in frontend/v1 scope — WS G import overlay).
package main

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"time"
)

func makeArchive() []byte {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	// 600-byte file so a truncation at 700 bytes cuts inside the data.
	tw.WriteHeader(&tar.Header{Name: "a.txt", Mode: 0o644, Size: 600,
		ModTime: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)})
	tw.Write(bytes.Repeat([]byte("h"), 600))
	tw.WriteHeader(&tar.Header{Name: "b/", Mode: 0o755, Typeflag: tar.TypeDir})
	tw.Close()
	return buf.Bytes()
}

func main() {
	good := makeArchive()

	// Truncated mid-file: Next succeeds, body read hits UnexpectedEOF.
	tr := tar.NewReader(bytes.NewReader(good[:700]))
	hdr, err := tr.Next()
	_, rerr := io.ReadAll(tr)
	fmt.Println("truncated", err == nil, hdr.Name == "a.txt",
		errors.Is(rerr, io.ErrUnexpectedEOF))

	// Truncated inside the second header block ([1536,2048)): Next itself
	// fails after the first file is drained.
	tr2 := tar.NewReader(bytes.NewReader(good[:1700]))
	hdr2, err := tr2.Next()
	fmt.Println("truncated-hdr1", err == nil, hdr2.Name == "a.txt")
	io.Copy(io.Discard, tr2)
	_, err = tr2.Next()
	fmt.Println("truncated-hdr2", errors.Is(err, io.ErrUnexpectedEOF))

	// Bad header checksum: flip a name byte -> ErrHeader.
	bad := bytes.Clone(good)
	bad[10] ^= 0xff
	tr3 := tar.NewReader(bytes.NewReader(bad))
	_, err = tr3.Next()
	fmt.Println("bad-checksum", err == tar.ErrHeader)

	// Trailing garbage after the two zero blocks is ignored.
	withGarbage := append(bytes.Clone(good), []byte("GARBAGE!")...)
	tr4 := tar.NewReader(bytes.NewReader(withGarbage))
	n := 0
	for {
		_, err := tr4.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Println("garbage-read-err", err)
			return
		}
		n++
	}
	fmt.Println("trailing-garbage", n == 2)

	// Next past end -> io.EOF (and stays EOF).
	tr5 := tar.NewReader(bytes.NewReader(good))
	tr5.Next()
	tr5.Next()
	_, err = tr5.Next()
	_, err2 := tr5.Next()
	fmt.Println("past-end", err == io.EOF, err2 == io.EOF)

	// Empty input -> immediate EOF, not an error.
	tr6 := tar.NewReader(bytes.NewReader(nil))
	_, err = tr6.Next()
	fmt.Println("empty", err == io.EOF)

	fmt.Println("ok")
}
