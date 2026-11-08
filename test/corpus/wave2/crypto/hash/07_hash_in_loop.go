// Hash-in-loop (R10): create a fresh digest / HMAC in a loop and verify the
// final digests against hard-coded vectors. Pins the R10 finding that
// sha256.New() is pure Go (synth_crypto_fips140_sha256.go) and never touches
// the deleted GoCrypto JDK intrinsics: 10000 fresh digests in a loop must
// produce exactly the gc values, with no handle table to leak through.
// The expected values are the gc-computed digests (hard-coded, not
// impl-vs-impl, so the program is capable of failing).
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
)

const iters = 10000

const wantSHA = "4db0ffa792e63c1cf0d6e6518ac28a60706efbf88020c32c37960e04cdf558e9"
const wantHMAC = "2d93cbc1be167bcb1637a4a23cbff01a7878f0c50ee833954ea5221bb1b8c628"

func main() {
	var acc []byte
	for i := 0; i < iters; i++ {
		d := sha256.New()
		d.Write([]byte("graalgo-r10"))
		acc = d.Sum(acc[:0])
	}
	sha := hex.EncodeToString(acc)
	fmt.Println("sha256-loop:", sha)
	m := hmac.New(sha256.New, []byte("key"))
	var mac []byte
	for i := 0; i < iters; i++ {
		m.Reset()
		m.Write([]byte("msg"))
		mac = m.Sum(mac[:0])
	}
	hm := hex.EncodeToString(mac)
	fmt.Println("hmac-loop:", hm)
	if sha != wantSHA || hm != wantHMAC {
		fmt.Println("MISMATCH")
		os.Exit(1)
	}
	fmt.Println("ok")
}
