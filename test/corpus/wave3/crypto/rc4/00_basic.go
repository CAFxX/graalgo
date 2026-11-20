package main

// L11 (REVIEW-2026-11-18-05): crypto/rc4 — zero corpus imports before
// this program. RC4 stream cipher: exact keystream pins for fixed keys,
// encrypt/decrypt round-trip, Reset reproducibility, bad key sizes.

import (
	"crypto/rc4"
	"fmt"
)

func main() {
	key := []byte("Key")
	c, err := rc4.NewCipher(key)
	if err != nil {
		fmt.Printf("new-err=%v\n", err)
		return
	}
	// RFC 6229 vector: key "Key", 16 zero bytes -> EB 02 2D ... (first 8 shown).
	ks := make([]byte, 16)
	c.XORKeyStream(ks, make([]byte, 16))
	fmt.Printf("keystream-key=%x\n", ks)

	// Round-trip: encrypt then decrypt with a fresh cipher.
	pt := []byte("Plaintext")
	enc := make([]byte, len(pt))
	c2, _ := rc4.NewCipher([]byte("Wiki"))
	c2.XORKeyStream(enc, pt)
	fmt.Printf("wiki-ct=%x\n", enc)
	dec := make([]byte, len(pt))
	c3, _ := rc4.NewCipher([]byte("Wiki"))
	c3.XORKeyStream(dec, enc)
	fmt.Printf("wiki-roundtrip=%q\n", string(dec))

	// Reset reproduces the same keystream.
	c4, _ := rc4.NewCipher([]byte("Key"))
	out1 := make([]byte, 8)
	c4.XORKeyStream(out1, make([]byte, 8))
	c4.Reset()
	out2 := make([]byte, 8)
	c4.XORKeyStream(out2, make([]byte, 8))
	fmt.Printf("reset-reproduces=%v\n", string(out1) == string(out2))

	// Bad key sizes are rejected.
	_, err = rc4.NewCipher([]byte{})
	fmt.Printf("empty-key-err=%v\n", err != nil)
	_, err = rc4.NewCipher(make([]byte, 257))
	fmt.Printf("long-key-err=%v\n", err != nil)
}
