package main

// L11 (REVIEW-2026-11-18-05): crypto/mlkem/mlkemtest — zero corpus
// imports before this program. Deterministic encapsulation with fixed
// randomness: exact shared-secret pins (same key + same randomness gives
// the same secret), ciphertext lengths, and Decapsulate agreement.

import (
	"crypto/mlkem"
	"crypto/mlkem/mlkemtest"
	"fmt"
)

func main() {
	seed := make([]byte, 64)
	for i := range seed {
		seed[i] = byte(i*3 + 1)
	}
	dk, err := mlkem.NewDecapsulationKey1024(seed)
	if err != nil {
		fmt.Printf("key1024-err=%v\n", err)
		return
	}
	ek := dk.EncapsulationKey()

	// Fixed 32-byte randomness: fully deterministic encapsulation.
	rnd := make([]byte, 32)
	for i := range rnd {
		rnd[i] = byte(i*7 + 3)
	}
	ss, ct, err := mlkemtest.Encapsulate1024(ek, rnd)
	if err != nil {
		fmt.Printf("encap-err=%v\n", err)
		return
	}
	fmt.Printf("ss1024=%x\n", ss)
	fmt.Printf("ct1024-len=%d\n", len(ct))
	// Same inputs again: identical shared secret.
	ss2, ct2, err := mlkemtest.Encapsulate1024(ek, rnd)
	fmt.Printf("deterministic-ss=%v ct=%v err=%v\n",
		string(ss2) == string(ss), string(ct2) == string(ct), err)
	// Decapsulate agrees with the deterministic encapsulation.
	dec, err := dk.Decapsulate(ct)
	fmt.Printf("decap-agrees=%v err=%v\n", string(dec) == string(ss), err)
	// Different randomness: different shared secret.
	rnd2 := make([]byte, 32)
	for i := range rnd2 {
		rnd2[i] = byte(255 - i)
	}
	ss3, _, err := mlkemtest.Encapsulate1024(ek, rnd2)
	fmt.Printf("randomness-sensitive=%v err=%v\n", string(ss3) != string(ss), err)

	// ML-KEM-768 deterministic path (same fixed seed).
	dk768, err := mlkem.NewDecapsulationKey768(seed)
	if err != nil {
		fmt.Printf("key768-err=%v\n", err)
		return
	}
	ss768, ct768, err := mlkemtest.Encapsulate768(dk768.EncapsulationKey(), rnd)
	if err != nil {
		fmt.Printf("encap768-err=%v\n", err)
		return
	}
	fmt.Printf("ss768=%x\n", ss768)
	fmt.Printf("ct768-len=%d\n", len(ct768))
	dec768, err := dk768.Decapsulate(ct768)
	fmt.Printf("decap768-agrees=%v err=%v\n", string(dec768) == string(ss768), err)
}
