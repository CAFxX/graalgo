// compress/gzip 00: round-trip at every level + header fields.
// Pins: NewWriterLevel for -2..9; 0/1/64KB inputs; header fields
// round-trip (Name, Comment, ModTime truncated to 1s, OS); multistream:
// two concatenated members (different levels) read as one stream.
package main

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"hash/crc32"
	"io"
	"time"
)

func main() {
	in := bytes.Repeat([]byte("gzip test data. "), 4096) // 60KB
	for level := -2; level <= 9; level++ {
		cbuf := new(bytes.Buffer)
		w, err := gzip.NewWriterLevel(cbuf, level)
		if err != nil {
			fmt.Println("new-writer-err", level, err)
			continue
		}
		w.Name = "test.txt"
		w.Comment = "a comment"
		w.ModTime = time.Unix(1234567890, 999999999) // nanos truncated to 1s
		w.OS = 255
		w.Write(in)
		w.Close()
		comp := cbuf.Bytes()
		r, err := gzip.NewReader(bytes.NewReader(comp))
		if err != nil {
			fmt.Println("new-reader-err", level, err)
			continue
		}
		out, err := io.ReadAll(r)
		fmt.Printf("level=%d clen=%d crc=%08x rt=%v rerr=%v name=%q comment=%q modtime=%d os=%d\n",
			level, len(comp), crc32.ChecksumIEEE(comp),
			bytes.Equal(out, in), err == nil,
			r.Name, r.Comment, r.ModTime.Unix(), r.OS)
		r.Close()
	}
	// multistream: two members, different levels.
	cbuf := new(bytes.Buffer)
	w1, _ := gzip.NewWriterLevel(cbuf, gzip.BestSpeed)
	w1.Write(in[:1000])
	w1.Close()
	w2, _ := gzip.NewWriterLevel(cbuf, gzip.BestCompression)
	w2.Write(in[1000:])
	w2.Close()
	r, _ := gzip.NewReader(bytes.NewReader(cbuf.Bytes()))
	out, _ := io.ReadAll(r)
	fmt.Println("multistream-rt", bytes.Equal(out, in), "saw-multistream", r.Multistream)
	r.Close()
	// empty input.
	cbuf2 := new(bytes.Buffer)
	w3 := gzip.NewWriter(cbuf2)
	w3.Close()
	r3, _ := gzip.NewReader(bytes.NewReader(cbuf2.Bytes()))
	out3, _ := io.ReadAll(r3)
	r3.Close()
	fmt.Println("empty-rt", len(out3) == 0)
}
