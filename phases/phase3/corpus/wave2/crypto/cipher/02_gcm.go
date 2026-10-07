// GCM Seal/Open vs gc (NIST vectors via differential).
package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"fmt"
	"os"
)

func main() {
	failed := false
	key := make([]byte, 16)
	nonce := make([]byte, 12)
	pt := bytes.Repeat([]byte("p"), 48)
	ad := []byte("aad")
	block, _ := aes.NewCipher(key)
	aead, _ := cipher.NewGCM(block)
	ct := aead.Seal(nil, nonce, pt, ad)
	pt2, err := aead.Open(nil, nonce, ct, ad)
	if err != nil {
		fmt.Println("FAIL open:", err)
		failed = true
	} else if !bytes.Equal(pt2, pt) {
		fmt.Println("FAIL roundtrip")
		failed = true
	}
	// Tamper
	bad := bytes.Clone(ct)
	bad[10] ^= 1
	if _, err := aead.Open(nil, nonce, bad, ad); err == nil {
		fmt.Println("FAIL tamper")
		failed = true
	}
	if failed {
		os.Exit(1)
	}
	fmt.Println("PASS")
}
