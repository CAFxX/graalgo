// GCM adversarial: non-12 nonces, empty pt/ad, exact-overlap Seal.
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
	block, _ := aes.NewCipher(key)
	// Non-12 nonce
	aead, _ := cipher.NewGCMWithNonceSize(block, 8)
	nonce := make([]byte, 8)
	ct := aead.Seal(nil, nonce, []byte("hello"), nil)
	pt, err := aead.Open(nil, nonce, ct, nil)
	if err != nil || !bytes.Equal(pt, []byte("hello")) {
		fmt.Println("FAIL non12")
		failed = true
	}
	// Empty pt
	aead2, _ := cipher.NewGCM(block)
	ct = aead2.Seal(nil, make([]byte, 12), nil, []byte("ad"))
	pt, err = aead2.Open(nil, make([]byte, 12), ct, []byte("ad"))
	if err != nil || len(pt) != 0 {
		fmt.Println("FAIL empty")
		failed = true
	}
	// Wrong AD
	ct = aead2.Seal(nil, make([]byte, 12), []byte("x"), []byte("ad1"))
	if _, err := aead2.Open(nil, make([]byte, 12), ct, []byte("ad2")); err == nil {
		fmt.Println("FAIL wrong-ad")
		failed = true
	}
	if failed {
		os.Exit(1)
	}
	fmt.Println("PASS")
}
