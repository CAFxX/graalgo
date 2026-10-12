package main

// P3-M9 tier-5b: crypto/ecdsa — GenerateKey with a deterministic reader,
// key validation (X,Y on curve, D in range), ECDH-method agreement.

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"fmt"
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
	c := elliptic.P256()
	priv, err := ecdsa.GenerateKey(c, &detReader{s: 0xbead})
	if err != nil {
		fmt.Printf("gen-err=%v\n", err)
		return
	}
	fmt.Printf("d-in-range=%v\n",
		priv.D.Sign() > 0 && priv.D.Cmp(c.Params().N) < 0)
	fmt.Printf("pub-oncurve=%v\n", c.IsOnCurve(priv.PublicKey.X, priv.PublicKey.Y))
	// D*G == (X,Y).
	x, y := c.ScalarBaseMult(priv.D.Bytes())
	fmt.Printf("dG-eq=%v\n", x.Cmp(priv.PublicKey.X) == 0 && y.Cmp(priv.PublicKey.Y) == 0)
	fmt.Printf("d=%x\n", priv.D)

	// ECDH agreement between two deterministic keys.
	priv2, err := ecdsa.GenerateKey(c, &detReader{s: 0xdead})
	if err != nil {
		fmt.Printf("gen2-err=%v\n", err)
		return
	}
	// Shared-secret agreement between two deterministic keys, computed
	// both ways through the raw curve (no ecdh package needed).
	sx1, _ := c.ScalarMult(priv2.PublicKey.X, priv2.PublicKey.Y, priv.D.Bytes())
	sx2, _ := c.ScalarMult(priv.PublicKey.X, priv.PublicKey.Y, priv2.D.Bytes())
	fmt.Printf("agreement=%v\n", sx1.Cmp(sx2) == 0)
	fmt.Printf("shared-x=%x\n", sx1)
}
