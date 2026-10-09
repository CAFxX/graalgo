// compress/gzip 02: corrupt-input error behavior.
// Pins: bad magic -> "gzip: invalid header"; truncated mid-stream ->
// io.ErrUnexpectedEOF; flipped payload bit -> "gzip: invalid checksum";
// NewReader on empty input -> io.EOF. Error strings compared exactly.
package main

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
)

func compress(in []byte) []byte {
	cbuf := new(bytes.Buffer)
	w := gzip.NewWriter(cbuf)
	w.Write(in)
	w.Close()
	return cbuf.Bytes()
}

func errStr(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}

func main() {
	in := bytes.Repeat([]byte("gzip corrupt test "), 500)
	good := compress(in)
	// bad magic: need at least 10 bytes to get past the header read.
	for i, g := range [][]byte{
		{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		{0x1f, 0x8c, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		append([]byte("not gzip!!"), 0x00, 0x00),
	} {
		_, err := gzip.NewReader(bytes.NewReader(g))
		fmt.Printf("badmagic%d newreader-err=%q\n", i, errStr(err))
	}
	// truncated.
	for _, cut := range []int{5, 10, 18, len(good) / 2, len(good) - 8} {
		r, err := gzip.NewReader(bytes.NewReader(good[:cut]))
		if err != nil {
			fmt.Printf("trunc@%d newreader-err=%q\n", cut, errStr(err))
			continue
		}
		_, err = io.ReadAll(r)
		r.Close()
		fmt.Printf("trunc@%d uneof=%v err=%q\n", cut, err == io.ErrUnexpectedEOF, errStr(err))
	}
	// flipped payload bit (after the 10-byte header).
	bad := bytes.Clone(good)
	bad[20] ^= 0x01
	r, _ := gzip.NewReader(bytes.NewReader(bad))
	_, err := io.ReadAll(r)
	r.Close()
	fmt.Printf("bitflip err=%q\n", errStr(err))
	// empty input.
	_, err = gzip.NewReader(bytes.NewReader(nil))
	fmt.Println("empty-newreader-eof", err == io.EOF)
}
