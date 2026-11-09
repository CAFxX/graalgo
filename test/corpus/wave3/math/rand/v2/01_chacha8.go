package main

import (
	"fmt"
	"io"
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
	// R08-S-03: Read bytes must match gc for a fixed seed (the
	// crypto/ecdsa legacy signing path reads through io.Reader).
	c6 := randv2.NewChaCha8(seed)
	buf := make([]byte, 37)
	n, err := c6.Read(buf)
	fmt.Println(n, err)
	fmt.Printf("%x\n", buf)
	// Short reads drain the readBuf across calls.
	c7 := randv2.NewChaCha8(seed)
	for i := 0; i < 3; i++ {
		small := make([]byte, 5)
		n, err := c7.Read(small)
		fmt.Println(n, err)
		fmt.Printf("%x\n", small)
	}
	// MarshalBinary/UnmarshalBinary round-trip preserves the stream.
	c8 := randv2.NewChaCha8(seed)
	_ = c8.Uint64()
	data, err := c8.MarshalBinary()
	fmt.Println(err)
	c9 := randv2.NewChaCha8(zseed)
	err = c9.UnmarshalBinary(data)
	fmt.Println(err)
	fmt.Println(c8.Uint64() == c9.Uint64())
	// io.ReadFull over ChaCha8 — the crypto/ecdsa __mrrandFieldElement
	// shape (interface dispatch to __chacha8Read via the itab).
	c10 := randv2.NewChaCha8(seed)
	full := make([]byte, 32)
	n, err = io.ReadFull(c10, full)
	fmt.Println(n, err)
	fmt.Printf("%x\n", full)
	fmt.Println("chacha8 ok")
}
