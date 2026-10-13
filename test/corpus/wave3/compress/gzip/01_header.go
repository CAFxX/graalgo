// compress/gzip 01: header encoding.
// Pins: the XFL byte reflects the level (2 = max compression, 4 = fastest);
// all Writer.Header fields round-trip; BestCompression with empty name;
// OS byte values.
package main

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"time"
)

func xfl(level int) byte {
	cbuf := new(bytes.Buffer)
	w, _ := gzip.NewWriterLevel(cbuf, level)
	w.Close()
	// gzip header: magic(2) + method(1) + flags(1) + mtime(4) + XFL(1) + OS(1)
	return cbuf.Bytes()[8]
}

func main() {
	fmt.Println("xfl-best", xfl(gzip.BestCompression))       // 2
	fmt.Println("xfl-fastest", xfl(gzip.BestSpeed))           // 4
	fmt.Println("xfl-default", xfl(gzip.DefaultCompression)) // 0
	fmt.Println("xfl-huffman", xfl(gzip.HuffmanOnly))        // 0
	// full header round-trip.
	cbuf := new(bytes.Buffer)
	w := gzip.NewWriter(cbuf)
	w.Header = gzip.Header{
		Name:    "hello.txt",
		Comment: "comment here",
		ModTime: time.Unix(1600000000, 0),
		OS:      3, // unix
	}
	w.Write([]byte("data"))
	w.Close()
	r, _ := gzip.NewReader(bytes.NewReader(cbuf.Bytes()))
	io.ReadAll(r)
	fmt.Printf("hdr name=%q comment=%q modtime=%d os=%d\n",
		r.Name, r.Comment, r.ModTime.Unix(), r.OS)
	r.Close()
	// BestCompression with empty name: no FNAME flag.
	cbuf2 := new(bytes.Buffer)
	w2, _ := gzip.NewWriterLevel(cbuf2, gzip.BestCompression)
	w2.Close()
	fmt.Println("empty-name-flags", cbuf2.Bytes()[3]) // flags byte: 0
	// OS round-trips for a few values.
	for _, os := range []byte{0, 3, 7, 255} {
		cb := new(bytes.Buffer)
		ww := gzip.NewWriter(cb)
		ww.OS = os
		ww.Close()
		rr, _ := gzip.NewReader(bytes.NewReader(cb.Bytes()))
		io.ReadAll(rr)
		fmt.Printf("os=%d back=%d\n", os, rr.OS)
		rr.Close()
	}
}
