// 00_iotest: reader/writer fault-injection wrappers behave as documented.
package main

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing/iotest"
)

func readAll(r io.Reader) (string, error) {
	b, err := io.ReadAll(r)
	return string(b), err
}

func main() {
	// OneByteReader: 1-byte reads, content preserved.
	s, err := readAll(iotest.OneByteReader(strings.NewReader("hello")))
	fmt.Println("onebyte:", s, err)

	// HalfReader: at most half per read, content preserved.
	s, err = readAll(iotest.HalfReader(strings.NewReader("hello world")))
	fmt.Println("half:", s, err)

	// DataErrReader: surfaces data with the error.
	r := iotest.DataErrReader(strings.NewReader("abc"))
	b := make([]byte, 2)
	var out string
	for {
		n, err := r.Read(b)
		out += string(b[:n])
		if err != nil {
			fmt.Println("dataerr:", out, err == io.EOF)
			break
		}
	}

	// TimeoutReader: first read fails with ErrTimeout.
	r2 := iotest.TimeoutReader(strings.NewReader("abc"))
	_, err = r2.Read(make([]byte, 4))
	fmt.Println("timeout:", err == iotest.ErrTimeout)

	// ErrReader: always fails.
	boom := errors.New("boom")
	_, err = iotest.ErrReader(boom).Read(make([]byte, 4))
	fmt.Println("errreader:", err == boom)

	// TruncateWriter: truncates at n.
	var sb strings.Builder
	w := iotest.TruncateWriter(&sb, 5)
	n, err := w.Write([]byte("hello world"))
	fmt.Println("truncate:", n, err, sb.String() == "hello")

	// TestReader: strings.Reader passes (nil error).
	if err := iotest.TestReader(strings.NewReader("test content"), []byte("test content")); err != nil {
		fmt.Println("BAD testreader:", err)
	} else {
		fmt.Println("testreader ok")
	}

	// NewReadLogger / NewWriteLogger: passthrough (log goes to stderr, ignored).
	lr := iotest.NewReadLogger("r>", strings.NewReader("logme"))
	s, err = readAll(lr)
	lw := iotest.NewWriteLogger("w>", io.Discard)
	_, werr := lw.Write([]byte("logme"))
	fmt.Println("loggers:", s == "logme", err == nil, werr == nil)
}
