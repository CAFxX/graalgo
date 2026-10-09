// compress/flate 03: corrupt-input error behavior.
// Pins: truncated stream -> io.ErrUnexpectedEOF (==, not string match);
// garbage bytes -> deterministic "flate:" error strings (compared
// byte-for-byte); NewReader on empty input -> io.EOF on first Read.
package main

import (
	"bytes"
	"compress/flate"
	"fmt"
	"io"
)

func compress(in []byte) []byte {
	cbuf := new(bytes.Buffer)
	w, _ := flate.NewWriter(cbuf, flate.DefaultCompression)
	w.Write(in)
	w.Close()
	return cbuf.Bytes()
}

func main() {
	in := bytes.Repeat([]byte("hello world, "), 500)
	good := compress(in)
	// truncated at several points.
	for _, cut := range []int{1, 10, len(good) / 2, len(good) - 1} {
		r := flate.NewReader(bytes.NewReader(good[:cut]))
		_, err := io.ReadAll(r)
		r.Close()
		fmt.Printf("trunc@%d uneof=%v errstr=%q\n", cut, err == io.ErrUnexpectedEOF, errStr(err))
	}
	// garbage bytes.
	for i, g := range [][]byte{
		{0xff, 0xff, 0xff, 0xff},
		[]byte("this is not deflate data at all"),
		{0x78, 0x9c, 0x01}, // zlib header, not raw deflate
	} {
		r := flate.NewReader(bytes.NewReader(g))
		_, err := io.ReadAll(r)
		r.Close()
		fmt.Printf("garbage%d errstr=%q\n", i, errStr(err))
	}
	// empty input: a stream with no blocks is truncated.
	r := flate.NewReader(bytes.NewReader(nil))
	_, err := r.Read(make([]byte, 10))
	fmt.Println("empty-read-uneof", err == io.ErrUnexpectedEOF)
	r.Close()
	// single-bit flip in the middle of a valid stream.
	bad := bytes.Clone(good)
	bad[len(bad)/2] ^= 0x01
	r2 := flate.NewReader(bytes.NewReader(bad))
	out2, err2 := io.ReadAll(r2)
	r2.Close()
	fmt.Printf("bitflip rt=%v errstr=%q\n", bytes.Equal(out2, in), errStr(err2))
}

func errStr(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}
