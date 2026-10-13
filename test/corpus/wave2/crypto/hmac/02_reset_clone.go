// HMAC Reset-after-Sum and Clone vs gc.
package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"os"
)

func main() {
	failed := false
	key := []byte("key")
	data := []byte("data")
	h := hmac.New(sha256.New, key)
	h.Write(data)
	s1 := hex.EncodeToString(h.Sum(nil))
	// Sum again (should be idempotent)
	s2 := hex.EncodeToString(h.Sum(nil))
	if s1 != s2 {
		fmt.Println("FAIL sum-idempotent")
		failed = true
	}
	// Clone
	h2 := hmac.New(sha256.New, key)
	h2.Write(data[:2])
	c, _ := h2.(hash.Cloner).Clone()
	h2.Write(data[2:])
	c.(hash.Hash).Write(data[2:])
	if hex.EncodeToString(h2.Sum(nil)) != hex.EncodeToString(c.(hash.Hash).Sum(nil)) {
		fmt.Println("FAIL clone")
		failed = true
	}
	_ = bytes.MinRead
	if failed {
		os.Exit(1)
	}
	fmt.Println("PASS")
}
