// compress/zlib 02: adversarial.
// Pins: invalid level error; empty vs nil dict equivalence; Reset with a
// new dict; 1-byte reads; dictionary larger than 32KB (only the last 32KB
// are used — pinned via wrong-dict error on read).
package main

import (
	"bytes"
	"compress/zlib"
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
	cbuf := new(bytes.Buffer)
	for _, level := range []int{-3, 99} {
		_, err := zlib.NewWriterLevel(cbuf, level)
		fmt.Printf("level=%d err=%v %q\n", level, err != nil, errStr(err))
		_, err = zlib.NewWriterLevelDict(cbuf, level, nil)
		fmt.Printf("dictlevel=%d err=%v\n", level, err != nil)
	}
	in := bytes.Repeat([]byte("zlib adversarial "), 1000)
	// empty dict vs nil dict.
	for i, dict := range [][]byte{nil, {}} {
		cb := new(bytes.Buffer)
		w, _ := zlib.NewWriterLevelDict(cb, 6, dict)
		w.Write(in)
		w.Close()
		r, _ := zlib.NewReaderDict(bytes.NewReader(cb.Bytes()), dict)
		out, err := io.ReadAll(r)
		r.Close()
		fmt.Printf("dict%d rt=%v err=%v\n", i, bytes.Equal(out, in), err == nil)
	}
	// Reset with a new dict.
	dict1 := bytes.Repeat([]byte("first dictionary "), 2048)
	dict2 := bytes.Repeat([]byte("second dictionary "), 2048)
	cb1 := new(bytes.Buffer)
	w1, _ := zlib.NewWriterLevelDict(cb1, 6, dict1)
	w1.Write(in)
	w1.Close()
	r, _ := zlib.NewReaderDict(bytes.NewReader(cb1.Bytes()), dict1)
	io.ReadAll(r)
	// reset to a stream built with dict2.
	cb2 := new(bytes.Buffer)
	w2, _ := zlib.NewWriterLevelDict(cb2, 6, dict2)
	w2.Write(in)
	w2.Close()
	err := r.(zlib.Resetter).Reset(bytes.NewReader(cb2.Bytes()), dict2)
	fmt.Println("reset-err", errStr(err))
	out, err := io.ReadAll(r)
	r.Close()
	fmt.Println("reset-newdict-rt", bytes.Equal(out, in), err == nil)
	// dictionary larger than 32KB: dict identity is the Adler-32 of the
	// full bytes, so the exact same 64KB dict works on both sides...
	bigdict := bytes.Repeat([]byte("0123456789abcdef"), 4096) // 64KB
	cb3 := new(bytes.Buffer)
	w3, _ := zlib.NewWriterLevelDict(cb3, 9, bigdict)
	w3.Write(in)
	w3.Close()
	r3, err := zlib.NewReaderDict(bytes.NewReader(cb3.Bytes()), bigdict)
	fmt.Println("bigdict-newreader-err", errStr(err))
	if err == nil {
		out3, err3 := io.ReadAll(r3)
		r3.Close()
		fmt.Println("bigdict-same64k-rt", bytes.Equal(out3, in), err3 == nil)
	}
	// ...while a different dict is rejected by checksum, not clipped.
	r4, err := zlib.NewReaderDict(bytes.NewReader(cb3.Bytes()), bytes.Repeat([]byte("z"), 65536))
	fmt.Println("bigdict-diffdict-err", errStr(err))
	if err == nil {
		io.ReadAll(r4)
		r4.Close()
	}
	// 1-byte reads.
	cb5 := new(bytes.Buffer)
	w5 := zlib.NewWriter(cb5)
	w5.Write(in)
	w5.Close()
	r5, _ := zlib.NewReader(bytes.NewReader(cb5.Bytes()))
	out5 := new(bytes.Buffer)
	one := make([]byte, 1)
	for {
		n, err := r5.Read(one)
		if n > 0 {
			out5.Write(one[:n])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
	}
	r5.Close()
	fmt.Println("read1-rt", bytes.Equal(out5.Bytes(), in))
}
