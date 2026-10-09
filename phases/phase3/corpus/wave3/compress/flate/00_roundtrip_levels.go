// compress/flate 00: round-trip at every compression level.
// Pins: NewWriter(w, level) for level -2..9 (HuffmanOnly..BestCompression),
// 0/1/64KB inputs (zeros, text, deterministic pseudo-random); compressed
// bytes are checksummed (differential byte-identity of the encoder);
// NewReader round-trip is byte-identical; Flush mid-stream correctness.
package main

import (
	"bytes"
	"compress/flate"
	"fmt"
	"hash/crc32"
	"io"
)

// xorshift32 with a fixed seed: deterministic pseudo-random input.
func pseudorandom(n int) []byte {
	b := make([]byte, n)
	x := uint32(0x12345678)
	for i := range b {
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		b[i] = byte(x)
	}
	return b
}

func text(n int) []byte {
	s := "the quick brown fox jumps over the lazy dog. "
	b := make([]byte, n)
	for i := range b {
		b[i] = s[i%len(s)]
	}
	return b
}

func main() {
	inputs := map[string][]byte{
		"empty":  {},
		"one":    {0x42},
		"zeros":  bytes.Repeat([]byte{0}, 64<<10),
		"text":   text(64 << 10),
		"random": pseudorandom(64 << 10),
	}
	names := []string{"empty", "one", "zeros", "text", "random"}
	for level := -2; level <= 9; level++ {
		for _, inName := range names {
			in := inputs[inName]
			cbuf := new(bytes.Buffer)
			w, err := flate.NewWriter(cbuf, level)
			if err != nil {
				fmt.Println("new-writer-err", level, err)
				continue
			}
			if _, err := w.Write(in); err != nil {
				fmt.Println("write-err", level, inName, err)
				continue
			}
			if err := w.Close(); err != nil {
				fmt.Println("close-err", level, inName, err)
				continue
			}
			comp := cbuf.Bytes()
			// round-trip
			r := flate.NewReader(bytes.NewReader(comp))
			out, err := io.ReadAll(r)
			r.Close()
			if err != nil {
				fmt.Println("read-err", level, inName, err)
				continue
			}
			fmt.Printf("level=%d in=%s clen=%d crc=%08x rt=%v\n",
				level, inName, len(comp), crc32.ChecksumIEEE(comp),
				bytes.Equal(out, in))
		}
	}
	// Flush mid-stream: half the input, Flush, rest, Close.
	cbuf := new(bytes.Buffer)
	w, _ := flate.NewWriter(cbuf, flate.DefaultCompression)
	in := text(4096)
	w.Write(in[:2048])
	if err := w.Flush(); err != nil {
		fmt.Println("flush-err", err)
	}
	w.Write(in[2048:])
	w.Close()
	r := flate.NewReader(bytes.NewReader(cbuf.Bytes()))
	out, _ := io.ReadAll(r)
	r.Close()
	fmt.Println("flush-midstream-rt", bytes.Equal(out, in))
	// io.Copy path (streaming writer).
	cbuf2 := new(bytes.Buffer)
	w2, _ := flate.NewWriter(cbuf2, flate.BestSpeed)
	io.Copy(w2, bytes.NewReader(text(1024)))
	w2.Close()
	r2 := flate.NewReader(bytes.NewReader(cbuf2.Bytes()))
	out2, _ := io.ReadAll(r2)
	r2.Close()
	fmt.Println("iocopy-rt", bytes.Equal(out2, text(1024)))
}
