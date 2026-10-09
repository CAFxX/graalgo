// compress/flate 01: streaming behavior.
// Pins: 1-byte-at-a-time writes into Writer and 1-byte reads from Reader
// round-trip; Reader over a 3-byte-chunk source; Reset reuse of Writer
// and Reader across streams.
package main

import (
	"bytes"
	"compress/flate"
	"fmt"
	"io"
)

// chunkReader returns at most n bytes per Read call.
type chunkReader struct {
	b []byte
	n int
}

func (c *chunkReader) Read(p []byte) (int, error) {
	if len(c.b) == 0 {
		return 0, io.EOF
	}
	m := c.n
	if m > len(c.b) {
		m = len(c.b)
	}
	if m > len(p) {
		m = len(p)
	}
	copy(p, c.b[:m])
	c.b = c.b[m:]
	return m, nil
}

func main() {
	in := bytes.Repeat([]byte("abcdef"), 1000) // 6000 bytes
	// 1-byte writes.
	cbuf := new(bytes.Buffer)
	w, _ := flate.NewWriter(cbuf, flate.DefaultCompression)
	for _, b := range in {
		if _, err := w.Write([]byte{b}); err != nil {
			fmt.Println("write1-err", err)
		}
	}
	w.Close()
	// 1-byte reads.
	r := flate.NewReader(bytes.NewReader(cbuf.Bytes()))
	out := new(bytes.Buffer)
	one := make([]byte, 1)
	for {
		n, err := r.Read(one)
		if n > 0 {
			out.Write(one[:n])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Println("read1-err", err)
			break
		}
	}
	r.Close()
	fmt.Println("byte-at-a-time-rt", bytes.Equal(out.Bytes(), in))
	// Reader over a 3-byte-chunk source.
	cbuf2 := new(bytes.Buffer)
	w2, _ := flate.NewWriter(cbuf2, flate.BestCompression)
	w2.Write(in)
	w2.Close()
	r2 := flate.NewReader(&chunkReader{b: cbuf2.Bytes(), n: 3})
	out2, err2 := io.ReadAll(r2)
	r2.Close()
	fmt.Println("chunk3-rt", bytes.Equal(out2, in), "err", err2 == nil)
	// Reset reuse: one Writer for 3 streams, one Reader for 3 streams.
	cbuf3 := new(bytes.Buffer)
	w3, _ := flate.NewWriter(cbuf3, 6)
	streams := [][]byte{in[:100], in[:2000], in}
	cat := new(bytes.Buffer)
	var lens []int
	for _, s := range streams {
		cbuf3.Reset()
		w3.Reset(cbuf3)
		w3.Write(s)
		w3.Close()
		lens = append(lens, cbuf3.Len())
		cat.Write(cbuf3.Bytes())
	}
	r3 := flate.NewReader(bytes.NewReader(nil))
	ok := true
	off := 0
	for i, s := range streams {
		part := cat.Bytes()[off : off+lens[i]]
		off += lens[i]
		r3.(flate.Resetter).Reset(bytes.NewReader(part), nil)
		dec, err := io.ReadAll(r3)
		if err != nil || !bytes.Equal(dec, s) {
			ok = false
		}
	}
	r3.Close()
	fmt.Println("reset-reuse-rt", ok)
}
