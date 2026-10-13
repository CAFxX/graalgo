// compress/lzw 00: round-trip across orders and literal widths.
// Pins: LSB and MSB orders x litWidth 2..8 (1 is out of range — see
// 01_corrupt.go); empty/1-byte/4KB inputs (text + binary) with bytes
// fitting the literal width; round-trip byte-identical; NewReader on
// non-lzw garbage -> error on first Read.
package main

import (
	"bytes"
	"compress/lzw"
	"fmt"
	"hash/crc32"
	"io"
)

func main() {
	for _, order := range []lzw.Order{lzw.LSB, lzw.MSB} {
		for litWidth := 2; litWidth <= 8; litWidth++ {
			max := 1<<uint(litWidth) - 1
			// inputs whose bytes fit the literal width.
			text := bytes.Repeat([]byte("lzw! "), 819) // bytes < 0x80
			for i := range text {
				text[i] = byte(int(text[i]) % (max + 1))
			}
			bin := make([]byte, 4096)
			for i := range bin {
				bin[i] = byte((i * 31) % (max + 1))
			}
			inputs := map[string][]byte{"empty": {}, "one": {byte(max)}, "text": text, "bin": bin}
			names := []string{"empty", "one", "text", "bin"}
			for _, inName := range names {
				in := inputs[inName]
				cbuf := new(bytes.Buffer)
				w := lzw.NewWriter(cbuf, order, litWidth)
				w.Write(in)
				w.Close()
				comp := cbuf.Bytes()
				r := lzw.NewReader(bytes.NewReader(comp), order, litWidth)
				out, err := io.ReadAll(r)
				r.Close()
				fmt.Printf("order=%d litWidth=%d in=%s clen=%d crc=%08x rt=%v rerr=%v\n",
					order, litWidth, inName, len(comp), crc32.ChecksumIEEE(comp),
					bytes.Equal(out, in), err == nil)
			}
		}
	}
	// garbage: error on first Read.
	for i, g := range [][]byte{
		[]byte("not lzw data"),
		{0xff, 0x00, 0x11, 0x22},
	} {
		r := lzw.NewReader(bytes.NewReader(g), lzw.LSB, 8)
		_, err := io.ReadAll(r)
		r.Close()
		fmt.Printf("garbage%d err=%v %q\n", i, err != nil, errStr(err))
	}
}

func errStr(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}
