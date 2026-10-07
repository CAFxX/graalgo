// Clone and BinaryMarshaler round-trip vs gc.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding"
	"encoding/hex"
	"fmt"
	"hash"
	"os"
)

func main() {
	failed := false
	h := sha256.New()
	h.Write([]byte("hello "))
	c, err := h.(hash.Cloner).Clone()
	if err != nil {
		fmt.Println("FAIL clone:", err)
		os.Exit(1)
	}
	h.Write([]byte("world"))
	c.(hash.Hash).Write([]byte("world"))
	if hex.EncodeToString(h.Sum(nil)) != hex.EncodeToString(c.(hash.Hash).Sum(nil)) {
		fmt.Println("FAIL clone-sum")
		failed = true
	}
	// Marshal
	m, err := h.(encoding.BinaryMarshaler).MarshalBinary()
	if err != nil {
		fmt.Println("FAIL marshal:", err)
		os.Exit(1)
	}
	h2 := sha256.New()
	if err := h2.(encoding.BinaryUnmarshaler).UnmarshalBinary(m); err != nil {
		fmt.Println("FAIL unmarshal:", err)
		os.Exit(1)
	}
	if hex.EncodeToString(h.Sum(nil)) != hex.EncodeToString(h2.Sum(nil)) {
		fmt.Println("FAIL marshal-sum")
		failed = true
	}
	_ = bytes.MinRead
	if failed {
		os.Exit(1)
	}
	fmt.Println("PASS")
}
