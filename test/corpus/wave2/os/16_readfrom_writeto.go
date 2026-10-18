// 16_readfrom_writeto: (*os.File).ReadFrom / (*os.File).WriteTo — the
// D4 copy semantics (io.ReadAll + concrete write / concrete read-all +
// io.WriteString), real since the 2026-10-18 review-p0-os remediation.
package main

import (
	"bytes"
	"fmt"
	"os"
	"strings"
)

func check(err error) {
	if err != nil {
		fmt.Println("FAIL:", err)
		os.Exit(1)
	}
}

func main() {
	dir, err := os.MkdirTemp("", "os16-")
	check(err)
	defer os.RemoveAll(dir)

	// ReadFrom: copy from a strings.Reader into the file.
	f, err := os.Create(dir + "/data")
	check(err)
	const payload = "hello-readfrom-0123456789"
	n, err := f.ReadFrom(strings.NewReader(payload))
	check(err)
	if n != int64(len(payload)) {
		fmt.Println("FAIL: ReadFrom n =", n)
		os.Exit(1)
	}
	check(f.Close())

	// WriteTo: copy the file out into a bytes.Buffer.
	f2, err := os.Open(dir + "/data")
	check(err)
	var buf bytes.Buffer
	n2, err := f2.WriteTo(&buf)
	check(err)
	if n2 != int64(len(payload)) {
		fmt.Println("FAIL: WriteTo n =", n2)
		os.Exit(1)
	}
	if buf.String() != payload {
		fmt.Printf("FAIL: WriteTo data = %q\n", buf.String())
		os.Exit(1)
	}
	check(f2.Close())

	// ReadFrom propagates a source read error (D4: byte/error behavior
	// matches the generic copy loop).
	f3, err := os.Create(dir + "/data2")
	check(err)
	_, err = f3.ReadFrom(errorReader{})
	if err == nil || err.Error() != "boom" {
		fmt.Println("FAIL: ReadFrom error =", err)
		os.Exit(1)
	}
	check(f3.Close())

	fmt.Println("ok")
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, fmt.Errorf("boom") }
