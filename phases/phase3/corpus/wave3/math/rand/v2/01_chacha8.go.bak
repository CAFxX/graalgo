package main

import (
	"fmt"
	randv2 "math/rand/v2"
)

func main() {
	// ChaCha8 with fixed seeds: Uint64 stream must match gc byte-identically.
	var seed [32]byte
	for i := range seed {
		seed[i] = byte(i)
	}
	c := randv2.NewChaCha8(seed)
	for i := 0; i < 8; i++ {
		fmt.Printf("%x\n", c.Uint64())
	}
	// All-zero seed.
	var zseed [32]byte
	c2 := randv2.NewChaCha8(zseed)
	for i := 0; i < 4; i++ {
		fmt.Printf("%x\n", c2.Uint64())
	}
	// Seed determinism: same seed -> same stream.
	c3 := randv2.NewChaCha8(seed)
	c4 := randv2.NewChaCha8(seed)
	match := true
	for i := 0; i < 100; i++ {
		if c3.Uint64() != c4.Uint64() {
			match = false
		}
	}
	fmt.Println(match)
	// Re-seed resets the stream.
	c3.Seed(zseed)
	c5 := randv2.NewChaCha8(zseed)
	fmt.Println(c3.Uint64() == c5.Uint64())
	// Rand over ChaCha8.
	r := randv2.New(randv2.NewChaCha8(seed))
	fmt.Println(r.Int64N(1000000))
	fmt.Printf("%.17g\n", r.NormFloat64())
	fmt.Println("chacha8 ok")
}
