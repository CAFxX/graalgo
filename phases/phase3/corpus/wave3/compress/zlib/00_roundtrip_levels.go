// compress/zlib 00: round-trip at every level + dict.
// Pins: NewWriterLevel for -2..9; 0/1/64KB inputs; NewWriterLevelDict /
// NewReaderDict round-trip; Writer without Close (only Flush) leaves the
// stream unterminated -> reader sees io.ErrUnexpectedEOF.
package main

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"hash/crc32"
	"io"
)

func main() {
	in := bytes.Repeat([]byte("zlib test data. "), 4096)
	one := []byte{0x7f}
	for level := -2; level <= 9; level++ {
		for _, tc := range []struct {
			name string
			data []byte
		}{{"empty", nil}, {"one", one}, {"data", in}} {
			cbuf := new(bytes.Buffer)
			w, err := zlib.NewWriterLevel(cbuf, level)
			if err != nil {
				fmt.Println("new-writer-err", level, err)
				continue
			}
			w.Write(tc.data)
			w.Close()
			comp := cbuf.Bytes()
			r, err := zlib.NewReader(bytes.NewReader(comp))
			if err != nil {
				fmt.Println("new-reader-err", level, tc.name, err)
				continue
			}
			out, err := io.ReadAll(r)
			r.Close()
			fmt.Printf("level=%d in=%s clen=%d crc=%08x rt=%v rerr=%v\n",
				level, tc.name, len(comp), crc32.ChecksumIEEE(comp),
				bytes.Equal(out, tc.data), err == nil)
		}
	}
	// dict round-trip.
	dict := bytes.Repeat([]byte("dictionary words "), 2048) // 34KB -> clipped to 32KB
	cbuf := new(bytes.Buffer)
	w, _ := zlib.NewWriterLevelDict(cbuf, zlib.BestCompression, dict)
	w.Write(in)
	w.Close()
	r, _ := zlib.NewReaderDict(bytes.NewReader(cbuf.Bytes()), dict)
	out, err := io.ReadAll(r)
	r.Close()
	fmt.Println("dict-rt", bytes.Equal(out, in), err == nil)
	// Flush without Close: unterminated stream.
	cbuf2 := new(bytes.Buffer)
	w2, _ := zlib.NewWriterLevel(cbuf2, 6)
	w2.Write(in)
	w2.Flush() // no Close
	r2, err2 := zlib.NewReader(bytes.NewReader(cbuf2.Bytes()))
	if err2 != nil {
		fmt.Println("flushonly-newreader-err", err2)
	} else {
		_, err3 := io.ReadAll(r2)
		r2.Close()
		fmt.Println("flushonly-uneof", err3 == io.ErrUnexpectedEOF)
	}
}
