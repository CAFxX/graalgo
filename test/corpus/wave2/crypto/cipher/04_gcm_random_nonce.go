// GCM with random nonce: round-trip, nonce uniqueness, tamper, truncation,
// NonceSize/Overhead, Seal/Open panic on non-empty nonce.
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
	aead, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		fmt.Println("FAIL NewGCMWithRandomNonce:", err)
		os.Exit(1)
	}
	if aead.NonceSize() != 0 {
		fmt.Println("FAIL NonceSize")
		failed = true
	}
	if aead.Overhead() != 28 {
		fmt.Println("FAIL Overhead")
		failed = true
	}
	pt := bytes.Repeat([]byte("m"), 64)
	ad := []byte("aad")
	// Round-trip, nil dst, non-nil ad.
	ct1 := aead.Seal(nil, nil, pt, ad)
	if len(ct1) != len(pt)+28 {
		fmt.Println("FAIL Seal len")
		failed = true
	}
	pt2, err := aead.Open(nil, nil, ct1, ad)
	if err != nil || !bytes.Equal(pt2, pt) {
		fmt.Println("FAIL roundtrip")
		failed = true
	}
	// Round-trip, non-nil dst, nil ad (dst aliases the Seal output array
	// on the Open leg: dst[:0] over the ct2 backing array).
	dst := make([]byte, 0, 128)
	ct2 := aead.Seal(dst, nil, pt, nil)
	pt3, err := aead.Open(dst[:0], nil, ct2, nil)
	if err != nil || !bytes.Equal(pt3, pt) {
		fmt.Println("FAIL roundtrip-dst")
		failed = true
	}
	// Nonce uniqueness: two Seals differ in the first 12 bytes, both open.
	if bytes.Equal(ct1[:12], ct2[:12]) {
		fmt.Println("FAIL nonce-reuse")
		failed = true
	}
	// Tamper in ciphertext and in nonce prefix.
	bad := bytes.Clone(ct1)
	bad[20] ^= 1
	if _, err := aead.Open(nil, nil, bad, ad); err == nil {
		fmt.Println("FAIL tamper-ct")
		failed = true
	}
	bad = bytes.Clone(ct1)
	bad[0] ^= 1
	if _, err := aead.Open(nil, nil, bad, ad); err == nil {
		fmt.Println("FAIL tamper-nonce")
		failed = true
	}
	// Truncated ciphertext (< 28 bytes).
	if _, err := aead.Open(nil, nil, ct1[:27], ad); err == nil {
		fmt.Println("FAIL truncated")
		failed = true
	}
	// Seal/Open panic on non-empty nonce.
	func() {
		defer func() {
			if recover() == nil {
				fmt.Println("FAIL seal-nonce-no-panic")
				failed = true
			}
		}()
		aead.Seal(nil, make([]byte, 1), pt, ad)
	}()
	func() {
		defer func() {
			if recover() == nil {
				fmt.Println("FAIL open-nonce-no-panic")
				failed = true
			}
		}()
		aead.Open(nil, make([]byte, 1), ct1, ad)
	}()
	// Empty plaintext round-trip.
	ct3 := aead.Seal(nil, nil, nil, ad)
	pt4, err := aead.Open(nil, nil, ct3, ad)
	if err != nil || len(pt4) != 0 {
		fmt.Println("FAIL empty")
		failed = true
	}
	if failed {
		os.Exit(1)
	}
	fmt.Println("PASS")
}
