package main

// P3-M9 tier-5b: crypto/ecdsa — deterministic sign/verify on P-256 with a
// fixed private scalar and fixed-k reader; tampered signature and wrong
// hash rejection.

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"fmt"
	"math/big"
)

// detReader is a deterministic xorshift64* stream; identical in both legs.
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

func fixedKey(c elliptic.Curve, dHex string) *ecdsa.PrivateKey {
	d, _ := new(big.Int).SetString(dHex, 16)
	x, y := c.ScalarBaseMult(d.Bytes())
	return &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{Curve: c, X: x, Y: y},
		D:         d,
	}
}

func main() {
	priv := fixedKey(elliptic.P256(),
		"c9afa9d845ba75166b5c215767b1d6934e50c3db36e89b127b8a622b120f672")
	hash := []byte("this is a test hash for ecdsa!!")

	r, s, err := ecdsa.Sign(&detReader{s: 0x5eed}, priv, hash)
	if err != nil {
		fmt.Printf("sign-err=%v\n", err)
		return
	}
	fmt.Printf("r=%x\n", r)
	fmt.Printf("s=%x\n", s)
	fmt.Printf("verify=%v\n", ecdsa.Verify(&priv.PublicKey, hash, r, s))

	// Tampered s.
	badS := new(big.Int).Add(s, big.NewInt(1))
	fmt.Printf("tampered=%v\n", ecdsa.Verify(&priv.PublicKey, hash, r, badS))

	// Wrong hash.
	fmt.Printf("wronghash=%v\n",
		ecdsa.Verify(&priv.PublicKey, []byte("different hash................"), r, s))

	// r,s = 0 rejected.
	fmt.Printf("zero=%v\n",
		ecdsa.Verify(&priv.PublicKey, hash, big.NewInt(0), big.NewInt(0)))

	// Public key view.
	fmt.Printf("pub-x=%x\n", priv.PublicKey.X)
	fmt.Printf("pub-y=%x\n", priv.PublicKey.Y)
}
