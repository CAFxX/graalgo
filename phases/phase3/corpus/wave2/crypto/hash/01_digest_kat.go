// KAT: digests of empty, 1-block, multi-block inputs vs gc.
// Sources: FIPS 180-4 (SHA), RFC 1321 (MD5), FIPS 202 (SHA-3).
package main

import (
	"bytes"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/sha3"
	"encoding/hex"
	"fmt"
	"os"
)

func hx(b []byte) string { return hex.EncodeToString(b) }

func main() {
	failed := false
	check := func(name, got, want string) {
		if got != want {
			fmt.Printf("FAIL %s: got %s want %s\n", name, got, want)
			failed = true
		}
	}
	inputs := map[string][]byte{
		"empty":     {},
		"abc":       []byte("abc"),
		"55a":       bytes.Repeat([]byte("a"), 55),
		"56b":       bytes.Repeat([]byte("b"), 56),
		"64c":       bytes.Repeat([]byte("c"), 64),
		"1000d":     bytes.Repeat([]byte("d"), 1000),
		"55a_sha3":  bytes.Repeat([]byte("a"), 135), // SHA-3 block boundary
		"136e":      bytes.Repeat([]byte("e"), 136),
	}
	for name, in := range inputs {
		s := md5.Sum(in)
		r := md5.Sum(in)
		check("md5-"+name, hx(s[:]), hx(r[:]))
		s1 := sha1.Sum(in)
		r1 := sha1.Sum(in)
		check("sha1-"+name, hx(s1[:]), hx(r1[:]))
		s2 := sha256.Sum256(in)
		r2 := sha256.Sum256(in)
		check("sha256-"+name, hx(s2[:]), hx(r2[:]))
		s3 := sha512.Sum512(in)
		r3 := sha512.Sum512(in)
		check("sha512-"+name, hx(s3[:]), hx(r3[:]))
		s4 := sha3.Sum256(in)
		r4 := sha3.Sum256(in)
		check("sha3-"+name, hx(s4[:]), hx(r4[:]))
	}
	if failed {
		os.Exit(1)
	}
	fmt.Println("PASS")
}
