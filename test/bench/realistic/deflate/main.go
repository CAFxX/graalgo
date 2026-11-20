// deflate: compress/flate compression and decompression of a generated text
// corpus (the gzip-class utility; compress/gzip itself waits for
// hash/crc32's assembly). Checks the round trip.
package main

import (
	"bytes"
	"compress/flate"
	"io"
	"strconv"

	"graalgo/bench/h"
)

var corpus []byte

func init() {
	seed := uint32(13)
	rnd := func(n int) int { seed = seed*1664525 + 1013904223; return int(seed>>8) % n }
	words := []string{"the", "quick", "brown", "fox", "jumps", "over", "lazy", "dog", "graalgo", "truffle", "compiler", "goroutine"}
	var b bytes.Buffer
	for b.Len() < 1<<20 {
		b.WriteString(words[rnd(len(words))])
		if rnd(8) == 0 {
			b.WriteString(strconv.Itoa(rnd(1 << 16)))
		}
		b.WriteByte(" \n"[rnd(10)/9])
	}
	corpus = b.Bytes()
}

func work() int {
	var c bytes.Buffer
	w, err := flate.NewWriter(&c, flate.DefaultCompression)
	if err != nil {
		panic(err)
	}
	w.Write(corpus)
	w.Close()
	n := c.Len()
	out, err := io.ReadAll(flate.NewReader(&c))
	if err != nil {
		panic(err)
	}
	if !bytes.Equal(out, corpus) {
		panic("round trip differs")
	}
	return n
}

func main() { h.Run("deflate", 10, work) }
