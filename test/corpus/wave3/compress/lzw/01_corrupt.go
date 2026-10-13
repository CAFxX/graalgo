// compress/lzw 01: corrupt-input error behavior.
// Pins: truncated mid-code -> io.ErrUnexpectedEOF; litWidth 0 and 9 at
// construction -> the invalid width surfaces as an error on Read
// ("lzw: litWidth %d out of range"); unknown order -> "lzw: unknown order".
package main

import (
	"bytes"
	"compress/lzw"
	"fmt"
	"io"
)

func errStr(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}

func main() {
	in := bytes.Repeat([]byte("lzw corrupt "), 400)
	for _, order := range []lzw.Order{lzw.LSB, lzw.MSB} {
		cbuf := new(bytes.Buffer)
		w := lzw.NewWriter(cbuf, order, 8)
		w.Write(in)
		w.Close()
		good := cbuf.Bytes()
		for _, cut := range []int{1, 5, len(good) / 2, len(good) - 1} {
			r := lzw.NewReader(bytes.NewReader(good[:cut]), order, 8)
			_, err := io.ReadAll(r)
			r.Close()
			fmt.Printf("order=%d trunc@%d uneof=%v err=%q\n",
				order, cut, err == io.ErrUnexpectedEOF, errStr(err))
		}
	}
	// invalid litWidth (valid range is 2..8).
	for _, lw := range []int{0, 1, 9, 100} {
		r := lzw.NewReader(bytes.NewReader([]byte{0x00}), lzw.LSB, lw)
		_, err := io.ReadAll(r)
		r.Close()
		fmt.Printf("litWidth=%d err=%q\n", lw, errStr(err))
	}
	// invalid order.
	r := lzw.NewReader(bytes.NewReader([]byte{0x00}), lzw.Order(42), 8)
	_, err := io.ReadAll(r)
	r.Close()
	fmt.Println("badorder err", errStr(err))
	// writer with invalid litWidth: error surfaces on Write/Close.
	w := lzw.NewWriter(io.Discard, lzw.LSB, 0)
	_, werr := w.Write([]byte("x"))
	cerr := w.Close()
	fmt.Println("writer-badlitwidth write-err", errStr(werr), "close-err", errStr(cerr))
}
