package main

// L11 (REVIEW-2026-11-18-05): crypto/pbkdf2 — zero corpus imports before
// this program. PBKDF2 key derivation with SHA-256/SHA-1/SHA-512: exact
// derived-key pins (RFC 7914 / RFC 6070 vectors), iteration sensitivity,
// key-length behavior, and invalid-argument errors.

import (
	"crypto/pbkdf2"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
)

func main() {
	// RFC 6070-style vectors (password "password", salt "salt").
	k, err := pbkdf2.Key(sha1.New, "password", []byte("salt"), 1, 20)
	fmt.Printf("sha1-iter1=%x err=%v\n", k, err)
	k, err = pbkdf2.Key(sha1.New, "password", []byte("salt"), 2, 20)
	fmt.Printf("sha1-iter2=%x err=%v\n", k, err)
	k, err = pbkdf2.Key(sha1.New, "password", []byte("salt"), 4096, 20)
	fmt.Printf("sha1-iter4096=%x err=%v\n", k, err)

	// SHA-256: iteration count changes the output; requesting more bytes
	// extends the same block stream, so the 32-byte prefix of a 64-byte
	// derivation equals the 32-byte derivation at the same iteration count.
	k1, _ := pbkdf2.Key(sha256.New, "password", []byte("salt"), 1, 32)
	k2, _ := pbkdf2.Key(sha256.New, "password", []byte("salt"), 2, 32)
	fmt.Printf("sha256-iter1=%x\n", k1)
	fmt.Printf("sha256-iter-sensitive=%v\n", string(k1) != string(k2))
	k100_32, _ := pbkdf2.Key(sha256.New, "password", []byte("salt"), 100, 32)
	k100_64, _ := pbkdf2.Key(sha256.New, "password", []byte("salt"), 100, 64)
	fmt.Printf("sha256-len64=%d prefix-matches-32=%v\n",
		len(k100_64), string(k100_64[:32]) == string(k100_32))

	// SHA-512, empty password/salt edge cases.
	k512, err := pbkdf2.Key(sha512.New, "", []byte{}, 1000, 64)
	fmt.Printf("sha512-empty=%x err=%v\n", k512, err)

	// Invalid arguments are rejected.
	_, err = pbkdf2.Key(sha256.New, "password", []byte("salt"), 0, 32)
	fmt.Printf("iter0-err=%v\n", err != nil)
	_, err = pbkdf2.Key(sha256.New, "password", []byte("salt"), 1, 0)
	fmt.Printf("len0-err=%v\n", err != nil)
}
