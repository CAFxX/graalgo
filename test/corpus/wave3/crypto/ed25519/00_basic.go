package main

// L11 (REVIEW-2026-11-18-05): crypto/ed25519 — zero corpus imports
// before this program. NewKeyFromSeed with a fixed seed, Sign/Verify,
// Equal, Seed round-trip. Ed25519 is deterministic (RFC 8032), so all
// printed values are pinned.

import (
	"crypto/ed25519"
	"fmt"
)

func main() {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i)
	}
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)
	fmt.Printf("pub=%x\n", []byte(pub))
	fmt.Printf("seed-roundtrip=%v\n", priv.Seed() != nil &&
		string(priv.Seed()) == string(seed))
	fmt.Printf("equal-self=%v\n", priv.Equal(ed25519.NewKeyFromSeed(seed)))

	msg := []byte("differential corpus message")
	sig := ed25519.Sign(priv, msg)
	fmt.Printf("sig=%x\n", sig)
	fmt.Printf("verify=%v\n", ed25519.Verify(pub, msg, sig))

	// Tampered message and tampered signature both fail.
	tampered := make([]byte, len(msg))
	copy(tampered, msg)
	tampered[0] ^= 0xff
	fmt.Printf("verify-tampered-msg=%v\n", ed25519.Verify(pub, tampered, sig))
	badSig := make([]byte, len(sig))
	copy(badSig, sig)
	badSig[0] ^= 0xff
	fmt.Printf("verify-tampered-sig=%v\n", ed25519.Verify(pub, msg, badSig))

	// Empty message signs and verifies.
	emptySig := ed25519.Sign(priv, nil)
	fmt.Printf("verify-empty=%v\n", ed25519.Verify(pub, nil, emptySig))

	// Wrong-length signature fails.
	fmt.Printf("verify-short-sig=%v\n", ed25519.Verify(pub, msg, sig[:32]))
}
