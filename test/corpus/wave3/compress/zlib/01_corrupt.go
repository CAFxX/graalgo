// compress/zlib 01: corrupt-input error behavior.
// Pins: bad Adler-32 -> "zlib: invalid checksum"; bad header check bits ->
// "zlib: invalid header"; truncated -> io.ErrUnexpectedEOF. Error strings
// compared exactly.
package main

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
)

func compress(in []byte) []byte {
	cbuf := new(bytes.Buffer)
	w := zlib.NewWriter(cbuf)
	w.Write(in)
	w.Close()
	return cbuf.Bytes()
}

func errStr(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}

func main() {
	in := bytes.Repeat([]byte("zlib corrupt test "), 500)
	good := compress(in)
	// bad Adler-32: flip a bit in the last 4 bytes.
	bad := bytes.Clone(good)
	bad[len(bad)-1] ^= 0x01
	r, err := zlib.NewReader(bytes.NewReader(bad))
	if err != nil {
		fmt.Println("badadler-newreader-err", errStr(err))
	} else {
		_, err = io.ReadAll(r)
		r.Close()
		fmt.Println("badadler err", errStr(err))
	}
	// bad header check bits: zlib header is 2 bytes; FCHECK must make
	// (CMF*256+FLG) % 31 == 0.
	badh := bytes.Clone(good)
	badh[1] ^= 0x01
	_, err = zlib.NewReader(bytes.NewReader(badh))
	fmt.Println("badheader err", errStr(err))
	// wrong method (not deflate).
	badm := bytes.Clone(good)
	badm[0] = 0x18 // CM = 8 -> 8 & 0x0f... use 0x78->0x18: CM=8 still; use 0x58
	badm[0] = 0x58
	_, err = zlib.NewReader(bytes.NewReader(badm))
	fmt.Println("badmethod err", errStr(err))
	// truncated.
	for _, cut := range []int{1, 3, len(good) / 2, len(good) - 5} {
		r, err := zlib.NewReader(bytes.NewReader(good[:cut]))
		if err != nil {
			fmt.Printf("trunc@%d newreader-err=%q\n", cut, errStr(err))
			continue
		}
		_, err = io.ReadAll(r)
		r.Close()
		fmt.Printf("trunc@%d uneof=%v err=%q\n", cut, err == io.ErrUnexpectedEOF, errStr(err))
	}
}
