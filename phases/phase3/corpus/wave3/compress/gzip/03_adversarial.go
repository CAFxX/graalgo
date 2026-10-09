// compress/gzip 03: adversarial.
// Pins: 64KB comment round-trips; empty name; UTF-8 name; ModTime before
// the epoch; Close idempotence (second Close returns error); Reset reuse;
// 1-byte reads.
package main

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"time"
)

func errStr(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}

func main() {
	in := []byte("adversarial gzip")
	// 64KB comment: the writer accepts it, but the reader's 512-byte
	// header buffer rejects it -> "gzip: invalid header" on NewReader.
	cbuf := new(bytes.Buffer)
	w := gzip.NewWriter(cbuf)
	w.Comment = string(bytes.Repeat([]byte("c"), 64<<10))
	w.Name = ""
	w.Write(in)
	w.Close()
	r, err := gzip.NewReader(bytes.NewReader(cbuf.Bytes()))
	fmt.Println("bigcomment-newreader-err", errStr(err))
	if err == nil {
		out, _ := io.ReadAll(r)
		fmt.Println("bigcomment-rt", bytes.Equal(out, in), "commentlen", len(r.Comment))
		r.Close()
	}
	// Latin-1 name (é = U+00E9): allowed, round-trips.
	cbuf2 := new(bytes.Buffer)
	w2 := gzip.NewWriter(cbuf2)
	w2.Name = "héllo.txt"
	w2.Write(in)
	w2.Close()
	r2, err := gzip.NewReader(bytes.NewReader(cbuf2.Bytes()))
	if err != nil {
		fmt.Println("latin1name-newreader-err", errStr(err))
	} else {
		out2, _ := io.ReadAll(r2)
		fmt.Println("latin1name-rt", bytes.Equal(out2, in), "name", r2.Name == "héllo.txt")
		r2.Close()
	}
	// Non-Latin-1 name (→ = U+2192): Write rejects it.
	cbuf2b := new(bytes.Buffer)
	w2b := gzip.NewWriter(cbuf2b)
	w2b.Name = "héllo→world.txt"
	_, werr := w2b.Write(in)
	fmt.Println("nonlatin1name-write-err", errStr(werr))
	w2b.Close()
	// ModTime before the epoch.
	cbuf3 := new(bytes.Buffer)
	w3 := gzip.NewWriter(cbuf3)
	w3.ModTime = time.Unix(-100, 0)
	w3.Write(in)
	w3.Close()
	r3, _ := gzip.NewReader(bytes.NewReader(cbuf3.Bytes()))
	io.ReadAll(r3)
	fmt.Println("pre-epoch-modtime", r3.ModTime.Unix())
	r3.Close()
	// Close idempotence: second Close returns an error.
	cbuf4 := new(bytes.Buffer)
	w4 := gzip.NewWriter(cbuf4)
	w4.Write(in)
	fmt.Println("close1", errStr(w4.Close()))
	fmt.Println("close2-err", errStr(w4.Close()) != "<nil>")
	// Reset reuse across two streams with different headers.
	cbuf5 := new(bytes.Buffer)
	w5 := gzip.NewWriter(cbuf5)
	w5.Name = "first"
	w5.Write([]byte("first-data"))
	w5.Close()
	first := bytes.Clone(cbuf5.Bytes()) // Clone: Bytes() aliases the buffer
	cbuf5.Reset()
	w5.Reset(cbuf5)
	w5.Name = "second"
	w5.Write([]byte("second-data"))
	w5.Close()
	for i, b := range [][]byte{first, cbuf5.Bytes()} {
		rr, _ := gzip.NewReader(bytes.NewReader(b))
		d, _ := io.ReadAll(rr)
		fmt.Printf("reset%d name=%q data=%q\n", i, rr.Name, string(d))
		rr.Close()
	}
	// 1-byte reads.
	cbuf6 := new(bytes.Buffer)
	w6 := gzip.NewWriter(cbuf6)
	w6.Write(in)
	w6.Close()
	r6, _ := gzip.NewReader(bytes.NewReader(cbuf6.Bytes()))
	out6 := new(bytes.Buffer)
	one := make([]byte, 1)
	for {
		n, err := r6.Read(one)
		if n > 0 {
			out6.Write(one[:n])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Println("read1-err", errStr(err))
			break
		}
	}
	r6.Close()
	fmt.Println("read1-rt", bytes.Equal(out6.Bytes(), in))
}
