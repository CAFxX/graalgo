// FIPS 197 Appendix B vectors. Source: FIPS-197.
package main

import (
	"crypto/aes"
	"encoding/hex"
	"fmt"
	"os"
)

func main() {
	failed := false
	check := func(name, got, want string) {
		if got != want {
			fmt.Printf("FAIL %s: got %s want %s\n", name, got, want)
			failed = true
		}
	}
	key, _ := hex.DecodeString("000102030405060708090a0b0c0d0e0f")
	pt, _ := hex.DecodeString("00112233445566778899aabbccddeeff")
	c, _ := aes.NewCipher(key)
	ct := make([]byte, 16)
	c.Encrypt(ct, pt)
	check("aes128", hex.EncodeToString(ct), "69c4e0d86a7b0430d8cdb78070b4c55a")
	pt2 := make([]byte, 16)
	c.Decrypt(pt2, ct)
	check("aes128-inv", hex.EncodeToString(pt2), hex.EncodeToString(pt))
	// AES-256
	key256, _ := hex.DecodeString("000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")
	c256, _ := aes.NewCipher(key256)
	ct256 := make([]byte, 16)
	c256.Encrypt(ct256, pt)
	check("aes256", hex.EncodeToString(ct256), "8ea2b7ca516745bfeafc49904b496089")
	if failed {
		os.Exit(1)
	}
	fmt.Println("PASS")
}
