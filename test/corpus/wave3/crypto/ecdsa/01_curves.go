package main

// P3-M9 tier-5b: crypto/ecdsa — fixed-scalar keys on all four NIST curves,
// public-key consistency, ASN.1 sign/verify.

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"fmt"
	"math/big"
)

type detReader struct{ s uint64 }

func (r *detReader) Read(b []byte) (int, error) {
	for i := range b {
		r.s ^= r.s >> 12
		r.s ^= r.s << 25
		r.s ^= r.s >> 27
		b[i] = byte((r.s * 0x2545F4914F6CDD1D) >> 56)
	}
	return len(b), nil
}

func main() {
	curves := []elliptic.Curve{elliptic.P224(), elliptic.P256(), elliptic.P384(), elliptic.P521()}
	dHex := "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"
	for i, c := range curves {
		d, _ := new(big.Int).SetString(dHex, 16)
		// Reduce d mod N to keep it in range for every curve.
		d.Mod(d, c.Params().N)
		if d.Sign() == 0 {
			d.SetInt64(1)
		}
		x, y := c.ScalarBaseMult(d.Bytes())
		priv := &ecdsa.PrivateKey{
			PublicKey: ecdsa.PublicKey{Curve: c, X: x, Y: y},
			D:         d,
		}
		hash := []byte{byte(i), 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16,
			17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}
		sig, err := ecdsa.SignASN1(&detReader{s: uint64(100 + i)}, priv, hash)
		if err != nil {
			fmt.Printf("%s asn1-err=%v\n", c.Params().Name, err)
			continue
		}
		fmt.Printf("%s asn1-len=%d verify=%v\n",
			c.Params().Name, len(sig), ecdsa.VerifyASN1(&priv.PublicKey, hash, sig))
		fmt.Printf("%s pub-x=%x\n", c.Params().Name, x)
	}
}
