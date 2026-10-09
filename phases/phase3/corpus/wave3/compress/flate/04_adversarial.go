// compress/flate 04: adversarial.
// Pins: NewWriter with level -3 and 99 returns non-nil error; 1MB of a
// single repeated byte at HuffmanOnly; all-256-byte-values input at every
// level; Write after Close returns an error.
package main

import (
	"bytes"
	"compress/flate"
	"fmt"
	"hash/crc32"
	"io"
)

func main() {
	cbuf := new(bytes.Buffer)
	for _, level := range []int{-3, 99, -100, 10} {
		_, err := flate.NewWriter(cbuf, level)
		fmt.Printf("level=%d err=%v\n", level, err != nil)
	}
	// 1MB of a single repeated byte at HuffmanOnly.
	in := bytes.Repeat([]byte{0xAB}, 1<<20)
	cbuf2 := new(bytes.Buffer)
	w, err := flate.NewWriter(cbuf2, flate.HuffmanOnly)
	if err != nil {
		fmt.Println("huffmanonly-new-err", err)
		return
	}
	w.Write(in)
	w.Close()
	comp := cbuf2.Bytes()
	r := flate.NewReader(bytes.NewReader(comp))
	out, err := io.ReadAll(r)
	r.Close()
	fmt.Printf("huffmanonly-1MB clen=%d crc=%08x rt=%v\n",
		len(comp), crc32.ChecksumIEEE(comp), bytes.Equal(out, in) && err == nil)
	// all 256 byte values at every level.
	all := make([]byte, 256)
	for i := range all {
		all[i] = byte(i)
	}
	for level := -2; level <= 9; level++ {
		cb := new(bytes.Buffer)
		w, _ := flate.NewWriter(cb, level)
		w.Write(all)
		w.Close()
		rr := flate.NewReader(bytes.NewReader(cb.Bytes()))
		dec, _ := io.ReadAll(rr)
		rr.Close()
		fmt.Printf("level=%d all256 rt=%v clen=%d\n", level, bytes.Equal(dec, all), cb.Len())
	}
	// Write after Close.
	cbuf3 := new(bytes.Buffer)
	w3, _ := flate.NewWriter(cbuf3, 6)
	w3.Close()
	_, err = w3.Write([]byte("x"))
	fmt.Println("write-after-close-err", err != nil)
	// Double Close.
	err = w3.Close()
	fmt.Println("double-close-err", err != nil, errStr(err))
}

func errStr(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}
