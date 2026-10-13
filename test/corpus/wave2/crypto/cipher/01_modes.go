// CBC/CTR/CFB/OFB round-trips vs gc.
package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"fmt"
	"os"
)

var failed = false

func check(name string, got, want []byte) {
	if !bytes.Equal(got, want) {
		fmt.Printf("FAIL %s\n", name)
		failed = true
	}
}

func main() {
	key, _ := hex.DecodeString("000102030405060708090a0b0c0d0e0f")
	iv, _ := hex.DecodeString("000102030405060708090a0b0c0d0e0f")
	pt := bytes.Repeat([]byte("a"), 64)
	block, _ := aes.NewCipher(key)

	// CBC
	enc := cipher.NewCBCEncrypter(block, iv)
	ct := make([]byte, 64)
	enc.CryptBlocks(ct, pt)
	dec := cipher.NewCBCDecrypter(block, iv)
	pt2 := make([]byte, 64)
	dec.CryptBlocks(pt2, ct)
	check("cbc", pt2, pt)

	// CTR
	ctr := cipher.NewCTR(block, iv)
	ct = make([]byte, 64)
	ctr.XORKeyStream(ct, pt)
	ctr2 := cipher.NewCTR(block, iv)
	pt2 = make([]byte, 64)
	ctr2.XORKeyStream(pt2, ct)
	check("ctr", pt2, pt)

	// CFB
	cfb := cipher.NewCFBEncrypter(block, iv)
	ct = make([]byte, 64)
	cfb.XORKeyStream(ct, pt)
	cfbd := cipher.NewCFBDecrypter(block, iv)
	pt2 = make([]byte, 64)
	cfbd.XORKeyStream(pt2, ct)
	check("cfb", pt2, pt)

	// OFB
	ofb := cipher.NewOFB(block, iv)
	ct = make([]byte, 64)
	ofb.XORKeyStream(ct, pt)
	ofb2 := cipher.NewOFB(block, iv)
	pt2 = make([]byte, 64)
	ofb2.XORKeyStream(pt2, ct)
	check("ofb", pt2, pt)

	if failed {
		os.Exit(1)
	}
	fmt.Println("PASS")
}
