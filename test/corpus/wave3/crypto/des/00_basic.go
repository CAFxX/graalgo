package main

// L11 (REVIEW-2026-11-18-05): crypto/des — zero corpus imports before
// this program. DES + TripleDES block ciphers: fixed keys, encrypt/decrypt
// round-trip, exact ciphertext pins, block size, bad key sizes.

import (
	"crypto/des"
	"fmt"
)

func main() {
	key := []byte("deskey!!") // 8 bytes
	c, err := des.NewCipher(key)
	if err != nil {
		fmt.Printf("new-err=%v\n", err)
		return
	}
	fmt.Printf("block-size=%d\n", c.BlockSize())
	plain := []byte("block123")
	ct := make([]byte, 8)
	c.Encrypt(ct, plain)
	fmt.Printf("des-ct=%x\n", ct)
	pt := make([]byte, 8)
	c.Decrypt(pt, ct)
	fmt.Printf("des-roundtrip=%q\n", string(pt))
	// Decrypt of the ciphertext with a wrong key differs.
	wrong, _ := des.NewCipher([]byte("wrongkey"))
	bad := make([]byte, 8)
	wrong.Decrypt(bad, ct)
	fmt.Printf("des-wrongkey-differs=%v\n", string(bad) != string(plain))

	// TripleDES, 24-byte key.
	k3 := []byte("12345678abcdefghABCDEFGH")
	c3, err := des.NewTripleDESCipher(k3)
	if err != nil {
		fmt.Printf("3des-err=%v\n", err)
		return
	}
	fmt.Printf("3des-block-size=%d\n", c3.BlockSize())
	ct3 := make([]byte, 8)
	c3.Encrypt(ct3, plain)
	fmt.Printf("3des-ct=%x\n", ct3)
	pt3 := make([]byte, 8)
	c3.Decrypt(pt3, ct3)
	fmt.Printf("3des-roundtrip=%q\n", string(pt3))

	// Bad key sizes are rejected.
	_, err = des.NewCipher([]byte("short"))
	fmt.Printf("des-shortkey-err=%v\n", err != nil)
	_, err = des.NewTripleDESCipher([]byte("12345678abcdefgh")) // 16-byte: valid K1==K3 form
	fmt.Printf("3des-16key-err=%v\n", err != nil)
	_, err = des.NewTripleDESCipher([]byte("tiny"))
	fmt.Printf("3des-tinykey-err=%v\n", err != nil)
}
