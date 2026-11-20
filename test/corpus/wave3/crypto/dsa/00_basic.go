package main

// L11 (REVIEW-2026-11-18-05): crypto/dsa — zero corpus imports before
// this program. Parameter generation (L1024_N160, deterministic reader),
// key generation, Sign/Verify round-trip, tampered-signature rejection.
// Only deterministic facts are printed (signature bytes are random).

import (
	"crypto/dsa"
	"crypto/sha256"
	"fmt"
	"math/big"
	mrand "math/rand"
)

func main() {
	rng := mrand.New(mrand.NewSource(1805))
	var params dsa.Parameters
	if err := dsa.GenerateParameters(&params, rng, dsa.L1024N160); err != nil {
		fmt.Printf("params-err=%v\n", err)
		return
	}
	fmt.Printf("p-bits=%d q-bits=%d g-bits=%d\n",
		params.P.BitLen(), params.Q.BitLen(), params.G.BitLen())
	// q divides p-1.
	pm1 := new(big.Int).Sub(params.P, big.NewInt(1))
	fmt.Printf("q-divides-pminus1=%v\n", new(big.Int).Mod(pm1, params.Q).Sign() == 0)

	priv := new(dsa.PrivateKey)
	priv.Parameters = params
	if err := dsa.GenerateKey(priv, rng); err != nil {
		fmt.Printf("key-err=%v\n", err)
		return
	}
	fmt.Printf("x-bits=%d y-bits=%d\n", priv.X.BitLen(), priv.Y.BitLen())

	msg := []byte("dsa differential message")
	h := sha256.Sum256(msg)
	r, s, err := dsa.Sign(rng, priv, h[:])
	if err != nil {
		fmt.Printf("sign-err=%v\n", err)
		return
	}
	fmt.Printf("sig-len-r=%d sig-len-s=%d\n", r.BitLen(), s.BitLen())
	fmt.Printf("verify=%v\n", dsa.Verify(&priv.PublicKey, h[:], r, s))
	// Tampered signature must fail.
	rBad := new(big.Int).Add(r, big.NewInt(1))
	fmt.Printf("verify-tampered=%v\n", dsa.Verify(&priv.PublicKey, h[:], rBad, s))
	// Wrong hash must fail.
	h2 := sha256.Sum256([]byte("other message"))
	fmt.Printf("verify-wronghash=%v\n", dsa.Verify(&priv.PublicKey, h2[:], r, s))
}
