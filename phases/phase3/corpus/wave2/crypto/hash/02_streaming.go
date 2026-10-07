// Streaming Write (1-byte, block-size, odd chunks) vs gc.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
)

func main() {
	failed := false
	data := bytes.Repeat([]byte("x"), 1000)
	for _, chunk := range []int{1, 7, 64, 100} {
		h := sha256.New()
		for i := 0; i < len(data); i += chunk {
			end := i + chunk
			if end > len(data) {
				end = len(data)
			}
			h.Write(data[i:end])
		}
		got := hex.EncodeToString(h.Sum(nil))
		r := sha256.New()
		r.Write(data)
		want := hex.EncodeToString(r.Sum(nil))
		if got != want {
			fmt.Printf("FAIL chunk=%d\n", chunk)
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
	fmt.Println("PASS")
}
