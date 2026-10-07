// RFC 4231 (HMAC-SHA-256) and RFC 2202 (HMAC-MD5) vectors.
// Source: RFC 4231 §4.2 TC1, RFC 4868 §2.7.1; RFC 2202 §2 TC1.
package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
)

func main() {
	failed := false
	check := func(name, got, want string) {
		if got != want {
			fmt.Printf("FAIL %s\n", name)
			failed = true
		}
	}
	// RFC 4231 TC1
	key := bytes.Repeat([]byte{0x0b}, 20)
	h := hmac.New(sha256.New, key)
	h.Write([]byte("Hi There"))
	check("rfc4231-tc1", hex.EncodeToString(h.Sum(nil)),
		"b0344c61d8db38535ca8afceaf0bf12b881dc200c9833da726e9376c2e32cff7")
	// RFC 2202 TC1 (HMAC-MD5)
	key = bytes.Repeat([]byte{0x0b}, 16)
	h = hmac.New(md5.New, key)
	h.Write([]byte("Hi There"))
	check("rfc2202-tc1", hex.EncodeToString(h.Sum(nil)),
		"9294727a3638bb1c13f48ef8158bfc9d")
	if failed {
		os.Exit(1)
	}
	fmt.Println("PASS")
}
