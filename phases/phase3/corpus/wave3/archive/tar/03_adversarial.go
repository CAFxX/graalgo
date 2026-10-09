// P3-M9 wave3: archive/tar — adversarial: 512-boundary size edges,
// 100-vs-101-char names, FormatUnknown auto-select (not an error),
// write-contract violations (ErrWriteTooLong / ErrWriteAfterClose),
// sparse PAX records as plain records, empty archive.
// SCOPE-LIMITED: no synthetic archive/tar source in frontend/v1 yet
// (imports are not supported in frontend/v1 scope — WS G import overlay).
package main

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"strings"
	"time"
)

func main() {
	modTime := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	// Sizes at 512-block edges round-trip byte-identical.
	for _, n := range []int{0, 511, 512, 513, 1023, 1024} {
		var buf bytes.Buffer
		tw := tar.NewWriter(&buf)
		body := bytes.Repeat([]byte{byte(n)}, n)
		if err := tw.WriteHeader(&tar.Header{Name: "f", Mode: 0o644,
			Size: int64(n), ModTime: modTime}); err != nil {
			fmt.Println("edge-write-err", n, err)
			return
		}
		tw.Write(body)
		tw.Close()
		tr := tar.NewReader(&buf)
		hdr, err := tr.Next()
		out, _ := io.ReadAll(tr)
		fmt.Println("edge", n, err == nil, hdr.Size == int64(n), bytes.Equal(out, body))
	}

	// Name of exactly 100 chars fits USTAR; 101 forces PAX.
	for _, n := range []int{100, 101} {
		var buf bytes.Buffer
		tw := tar.NewWriter(&buf)
		name := strings.Repeat("a", n)
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: 0, ModTime: modTime})
		tw.Close()
		tr := tar.NewReader(&buf)
		hdr, err := tr.Next()
		want := tar.FormatUSTAR
		if n == 101 {
			want = tar.FormatPAX
		}
		fmt.Println("namelen", n, err == nil, hdr.Name == name, hdr.Format == want)
	}

	// FormatUnknown on write auto-selects (it is not an error).
	var b1 bytes.Buffer
	tw1 := tar.NewWriter(&b1)
	err := tw1.WriteHeader(&tar.Header{Name: "auto", Mode: 0o644,
		Size: 1, Format: tar.FormatUnknown})
	tw1.Write([]byte("x"))
	tw1.Close()
	tr1 := tar.NewReader(&b1)
	hdr1, _ := tr1.Next()
	fmt.Println("formatunknown", err == nil, hdr1.Format == tar.FormatUSTAR)

	// Writing more than Size -> ErrWriteTooLong.
	var b2 bytes.Buffer
	tw2 := tar.NewWriter(&b2)
	tw2.WriteHeader(&tar.Header{Name: "s", Mode: 0o644, Size: 2})
	_, err = tw2.Write([]byte("toolong"))
	fmt.Println("write-too-long", err == tar.ErrWriteTooLong)
	tw2.Close()

	// Write before any header -> ErrWriteTooLong as well (pins identity).
	var b3 bytes.Buffer
	tw3 := tar.NewWriter(&b3)
	_, err = tw3.Write([]byte("x"))
	fmt.Println("write-before-header", err == tar.ErrWriteTooLong)
	tw3.Close()

	// Write after Close -> ErrWriteAfterClose; Close is idempotent.
	var b4 bytes.Buffer
	tw4 := tar.NewWriter(&b4)
	tw4.Close()
	_, err = tw4.Write([]byte("x"))
	err2 := tw4.Close()
	fmt.Println("write-after-close", err == tar.ErrWriteAfterClose, err2 == nil)

	// Sparse GNU PAX records are carried as plain PAX records: the reader
	// does not interpret them (pins the documented non-behavior).
	var b5 bytes.Buffer
	tw5 := tar.NewWriter(&b5)
	err = tw5.WriteHeader(&tar.Header{Name: "sparse", Mode: 0o644, Size: 10,
		ModTime: modTime, Format: tar.FormatPAX,
		PAXRecords: map[string]string{
			"GNU.sparse.major":    "1",
			"GNU.sparse.minor":    "0",
			"GNU.sparse.name":     "sparse",
			"GNU.sparse.realsize": "100",
			"GNU.sparse.map":      "0,5,50,5",
		}})
	if err != nil {
		fmt.Println("sparse-write-err", err)
		return
	}
	tw5.Write(bytes.Repeat([]byte("s"), 10))
	tw5.Close()
	tr5 := tar.NewReader(&b5)
	hdr5, err := tr5.Next()
	out5, rerr := io.ReadAll(tr5)
	fmt.Println("sparse", err == nil, rerr == nil,
		hdr5.PAXRecords["GNU.sparse.map"] == "0,5,50,5",
		len(out5) == 10, string(out5) == strings.Repeat("s", 10))

	// Close with no files -> valid empty archive (two zero blocks).
	var b6 bytes.Buffer
	tw6 := tar.NewWriter(&b6)
	tw6.Close()
	fmt.Println("empty-archive", len(b6.Bytes()) == 1024)
	tr6 := tar.NewReader(&b6)
	_, err = tr6.Next()
	fmt.Println("empty-next", err == io.EOF)

	fmt.Println("ok")
}
