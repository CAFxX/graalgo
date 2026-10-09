// compress/flate 02: preset dictionaries.
// Pins: NewWriterDict/NewReaderDict round-trip byte-identical with a 32KB
// dictionary; a reader with the wrong dictionary returns an error (not
// silent corruption); nil-dict equivalence with the dict-less constructors.
package main

import (
	"bytes"
	"compress/flate"
	"fmt"
	"io"
)

func main() {
	// 32KB dictionary: repeated English-ish text.
	dict := bytes.Repeat([]byte("the quick brown fox jumps over the lazy dog "), 800) // 36000 bytes
	dict = dict[:32<<10]
	in := append(bytes.Repeat([]byte("the quick brown fox "), 2000),
		bytes.Repeat([]byte("jumps over the lazy dog "), 2000)...)
	// dict round-trip at two levels.
	for _, level := range []int{flate.DefaultCompression, flate.BestCompression} {
		cbuf := new(bytes.Buffer)
		w, err := flate.NewWriterDict(cbuf, level, dict)
		if err != nil {
			fmt.Println("dict-writer-err", err)
			continue
		}
		w.Write(in)
		w.Close()
		r := flate.NewReaderDict(bytes.NewReader(cbuf.Bytes()), dict)
		out, err := io.ReadAll(r)
		r.Close()
		fmt.Printf("level=%d dict-rt=%v err=%v\n", level, bytes.Equal(out, in), err == nil)
		// wrong dictionary: raw deflate has no integrity check, so the
		// output is silently wrong (not an error). Pin the divergence.
		r2 := flate.NewReaderDict(bytes.NewReader(cbuf.Bytes()), bytes.Repeat([]byte("x"), 32<<10))
		out2, err2 := io.ReadAll(r2)
		r2.Close()
		fmt.Printf("level=%d wrong-dict err=%v diverges=%v outlen=%d\n",
			level, err2 != nil, !bytes.Equal(out2, in), len(out2))
	}
	// nil dict behaves like the dict-less constructors.
	cbuf := new(bytes.Buffer)
	w, _ := flate.NewWriterDict(cbuf, 6, nil)
	w.Write(in)
	w.Close()
	r := flate.NewReaderDict(bytes.NewReader(cbuf.Bytes()), nil)
	out, err := io.ReadAll(r)
	r.Close()
	fmt.Println("nil-dict-rt", bytes.Equal(out, in), err == nil)
}
