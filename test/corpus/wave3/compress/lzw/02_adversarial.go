// compress/lzw 02: adversarial.
// Pins: 1-byte reads; Reset reuse across order changes; empty input;
// Close idempotence; literal-width edge (litWidth 8 with byte 0xff,
// which exceeds the max literal).
package main

import (
	"bytes"
	"compress/lzw"
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
	in := bytes.Repeat([]byte("lzw adversarial "), 300)
	// 1-byte reads, both orders.
	for _, order := range []lzw.Order{lzw.LSB, lzw.MSB} {
		cbuf := new(bytes.Buffer)
		w := lzw.NewWriter(cbuf, order, 8)
		w.Write(in)
		w.Close()
		r := lzw.NewReader(bytes.NewReader(cbuf.Bytes()), order, 8)
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
				fmt.Println("read1-err", errStr(err))
				break
			}
		}
		r.Close()
		fmt.Printf("order=%d read1-rt=%v\n", order, bytes.Equal(out.Bytes(), in))
	}
	// Reset reuse across order changes.
	cbuf := new(bytes.Buffer)
	w := lzw.NewWriter(cbuf, lzw.LSB, 8)
	w.Write(in)
	w.Close()
	lsb := bytes.Clone(cbuf.Bytes()) // Clone: Bytes() aliases cbuf's buffer
	cbuf.Reset()
	w2 := lzw.NewWriter(cbuf, lzw.MSB, 8)
	w2.Write(in)
	w2.Close()
	msb := cbuf.Bytes()
	r := lzw.NewReader(bytes.NewReader(lsb), lzw.LSB, 8)
	d1, _ := io.ReadAll(r)
	// Reset to the MSB stream (order change across Reset).
	r.(interface {
		Reset(r io.Reader, order lzw.Order, litWidth int)
	}).Reset(bytes.NewReader(msb), lzw.MSB, 8)
	d2, err2 := io.ReadAll(r)
	r.Close()
	fmt.Println("reset-orderchange rt1", bytes.Equal(d1, in), "rt2", bytes.Equal(d2, in), err2 == nil)
	// empty input.
	cbuf2 := new(bytes.Buffer)
	w3 := lzw.NewWriter(cbuf2, lzw.LSB, 8)
	w3.Close()
	r3 := lzw.NewReader(bytes.NewReader(cbuf2.Bytes()), lzw.LSB, 8)
	out3, err3 := io.ReadAll(r3)
	r3.Close()
	fmt.Println("empty-rt", len(out3) == 0, err3 == nil)
	// Close idempotence.
	r4 := lzw.NewReader(bytes.NewReader(lsb), lzw.LSB, 8)
	fmt.Println("close1", errStr(r4.Close()), "close2", errStr(r4.Close()))
	// byte 0xff with litWidth 7: exceeds max literal (0x7f) -> writer error.
	// (litWidth 8 allows 0xff; the range check is skipped when maxLit == 0xff.)
	cbuf4 := new(bytes.Buffer)
	w4 := lzw.NewWriter(cbuf4, lzw.LSB, 7)
	_, werr := w4.Write([]byte{0xff})
	fmt.Println("write-0xff-litwidth7-err", errStr(werr))
	w4.Close()
}
