package main

// L11 (REVIEW-2026-11-18-05): crypto/mlkem — zero corpus imports before
// this program. ML-KEM-768 and ML-KEM-1024 key generation, Encapsulate /
// Decapsulate round-trip, key encoding sizes. Keys and ciphertexts are
// random; only sizes and shared-secret equality are pinned.

import (
	"crypto/mlkem"
	"fmt"
)

func main() {
	dk, err := mlkem.GenerateKey1024()
	if err != nil {
		fmt.Printf("key1024-err=%v\n", err)
		return
	}
	ek := dk.EncapsulationKey()
	fmt.Printf("dk1024-bytes=%d ek1024-bytes=%d\n", len(dk.Bytes()), len(ek.Bytes()))
	ss, ct := ek.Encapsulate()
	fmt.Printf("ct1024-len=%d ss-len=%d\n", len(ct), len(ss))
	ss2, err := dk.Decapsulate(ct)
	fmt.Printf("decap=%v err=%v\n", string(ss2) == string(ss), err)
	// Tampered ciphertext decapsulates to a different secret (implicit rejection).
	bad := make([]byte, len(ct))
	copy(bad, ct)
	bad[0] ^= 0xff
	ss3, err := dk.Decapsulate(bad)
	fmt.Printf("decap-tampered-differs=%v err=%v\n", string(ss3) != string(ss), err)

	dk768, err := mlkem.GenerateKey768()
	if err != nil {
		fmt.Printf("key768-err=%v\n", err)
		return
	}
	ek768 := dk768.EncapsulationKey()
	fmt.Printf("dk768-bytes=%d ek768-bytes=%d\n", len(dk768.Bytes()), len(ek768.Bytes()))
	ss768, ct768 := ek768.Encapsulate()
	fmt.Printf("ct768-len=%d ss768-len=%d\n", len(ct768), len(ss768))
	ss768b, err := dk768.Decapsulate(ct768)
	fmt.Printf("decap768=%v err=%v\n", string(ss768b) == string(ss768), err)

	// Seed round-trip: the same 64-byte seed regenerates the same key.
	seed := make([]byte, 64)
	for i := range seed {
		seed[i] = byte(i*3 + 1)
	}
	dkSeed, err := mlkem.NewDecapsulationKey1024(seed)
	if err != nil {
		fmt.Printf("seed-key-err=%v\n", err)
		return
	}
	dkSeed2, err := mlkem.NewDecapsulationKey1024(seed)
	if err != nil {
		fmt.Printf("seed-key2-err=%v\n", err)
		return
	}
	fmt.Printf("seed-roundtrip-ek=%v\n", string(dkSeed2.EncapsulationKey().Bytes()) == string(dkSeed.EncapsulationKey().Bytes()))
}
