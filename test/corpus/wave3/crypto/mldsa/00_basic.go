package main

// L11 (REVIEW-2026-11-18-05): crypto/mldsa — zero corpus imports before
// this program. ML-DSA-44 key generation from a fixed seed (fully
// deterministic), SignDeterministic exact signature pins, Verify
// round-trip, tampered-signature rejection, context binding, and the
// MLDSA65/MLDSA87 parameter sets.

import (
	"crypto/mldsa"
	"crypto/rand"
	"fmt"
)

func main() {
	// Fixed 32-byte seed: the key is fully deterministic.
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i*11 + 5)
	}
	priv, err := mldsa.NewPrivateKey(mldsa.MLDSA44(), seed)
	if err != nil {
		fmt.Printf("key-err=%v\n", err)
		return
	}
	pub := priv.PublicKey()
	fmt.Printf("priv-bytes=%x\n", priv.Bytes())
	fmt.Printf("pub-bytes=%x\n", pub.Bytes())

	msg := []byte("mldsa differential message")
	sig, err := priv.SignDeterministic(msg, nil)
	if err != nil {
		fmt.Printf("sign-err=%v\n", err)
		return
	}
	fmt.Printf("sig=%x\n", sig)
	fmt.Printf("verify=%v\n", mldsa.Verify(pub, msg, sig, nil) == nil)
	// Tampered signature must fail.
	bad := make([]byte, len(sig))
	copy(bad, sig)
	bad[0] ^= 0xff
	fmt.Printf("verify-tampered=%v\n", mldsa.Verify(pub, msg, bad, nil) == nil)
	// Wrong message must fail.
	fmt.Printf("verify-wrongmsg=%v\n",
		mldsa.Verify(pub, []byte("other"), sig, nil) == nil)

	// Context string binds the signature.
	sigCtx, err := priv.SignDeterministic(msg, &mldsa.Options{Context: "ctx-A"})
	if err != nil {
		fmt.Printf("sign-ctx-err=%v\n", err)
		return
	}
	fmt.Printf("verify-ctx=%v\n",
		mldsa.Verify(pub, msg, sigCtx, &mldsa.Options{Context: "ctx-A"}) == nil)
	fmt.Printf("verify-ctx-wrong=%v\n",
		mldsa.Verify(pub, msg, sigCtx, &mldsa.Options{Context: "ctx-B"}) == nil)
	fmt.Printf("verify-ctx-missing=%v\n",
		mldsa.Verify(pub, msg, sigCtx, nil) == nil)

	// Seed round-trip: same seed regenerates the same key.
	priv2, err := mldsa.NewPrivateKey(mldsa.MLDSA44(), seed)
	if err != nil {
		fmt.Printf("seed-key-err=%v\n", err)
		return
	}
	fmt.Printf("seed-roundtrip-pub=%v\n", string(priv2.PublicKey().Bytes()) == string(pub.Bytes()))

	// Randomized Sign (hedged) still verifies; only the boolean is pinned.
	sigR, err := priv.Sign(rand.Reader, msg, nil)
	if err != nil {
		fmt.Printf("sign-rand-err=%v\n", err)
		return
	}
	fmt.Printf("sig-rand-len=%d verify-rand=%v\n", len(sigR),
		mldsa.Verify(pub, msg, sigR, nil) == nil)

	// Parameter sets: key sizes differ per set.
	for _, p := range []struct {
		name string
		fn   func() mldsa.Parameters
	}{
		{"44", mldsa.MLDSA44},
		{"65", mldsa.MLDSA65},
		{"87", mldsa.MLDSA87},
	} {
		k, err := mldsa.GenerateKey(p.fn())
		if err != nil {
			fmt.Printf("gen-%s-err=%v\n", p.name, err)
			return
		}
		fmt.Printf("params-%s priv-len=%d pub-len=%d\n",
			p.name, len(k.Bytes()), len(k.PublicKey().Bytes()))
	}
}
