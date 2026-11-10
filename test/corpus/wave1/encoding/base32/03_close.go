package main

import (
	"bytes"
	"encoding/base32"
	"fmt"
)

func main() {
	// Close with nothing written: no flush, nil error.
	var buf bytes.Buffer
	w := base32.NewEncoder(base32.StdEncoding, &buf)
	fmt.Println(w.Close() == nil, buf.Len())
	// Close is idempotent: a second Close still returns nil.
	fmt.Println(w.Close() == nil, buf.Len())
	// Same for a fresh encoder over a fresh buffer (HexEncoding).
	var buf2 bytes.Buffer
	w2 := base32.NewEncoder(base32.HexEncoding, &buf2)
	fmt.Println(w2.Close() == nil, buf2.Len())
}
