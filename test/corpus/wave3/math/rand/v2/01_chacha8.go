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
	// Direct Read: fixed-seed byte stream (exercises the __chacha8Read
	// pair helper, the readBuf carry-over, and refill past one block).
	cr := randv2.NewChaCha8(seed)
	buf := make([]byte, 100)
	n, err := cr.Read(buf)
	fmt.Println(n, err == nil)
	fmt.Printf("%x\n", buf)
	// Short reads: 5 then 13 bytes (readBuf carry-over across calls).
	cs := randv2.NewChaCha8(seed)
	small := make([]byte, 5)
	n, _ = cs.Read(small)
	fmt.Println(n)
	fmt.Printf("%x\n", small)
	mid := make([]byte, 13)
	n, _ = cs.Read(mid)
	fmt.Println(n)
	fmt.Printf("%x\n", mid)
	// Zero-length read.
	n, _ = cs.Read(nil)
	fmt.Println(n)
	// io.Reader interface path (the crypto/ecdsa signLegacy shape:
	// ChaCha8 consumed through io.ReadFull, not a direct call).
	var rd io.Reader = randv2.NewChaCha8(seed)
	full := make([]byte, 64)
	if _, err := io.ReadFull(rd, full); err != nil {
		fmt.Println("readfull err:", err)
	}
	fmt.Printf("%x\n", full)
	// Marshal/Unmarshal round-trip: marshal after 3 draws, unmarshal
	// into a fresh ChaCha8, and compare the next 10 draws.
	cm := randv2.NewChaCha8(seed)
	for i := 0; i < 3; i++ {
		cm.Uint64()
	}
	enc, err := cm.MarshalBinary()
	fmt.Println(err == nil)
	fmt.Printf("%x\n", enc)
	var cu randv2.ChaCha8
	if err := cu.UnmarshalBinary(enc); err != nil {
		fmt.Println("unmarshal err:", err)
	}
	roundtrip := true
	for i := 0; i < 10; i++ {
		if cm.Uint64() != cu.Uint64() {
			roundtrip = false
		}
	}
	fmt.Println(roundtrip)
	// Marshal with a partial readBuf: Read(5) leaves 3 bytes buffered,
	// so the encoding carries a "readbuf:" prefix; round-trip must
	// still reproduce the stream.
	cp := randv2.NewChaCha8(seed)
	five := make([]byte, 5)
	n, _ = cp.Read(five)
	fmt.Println(n)
	enc2, _ := cp.AppendBinary(nil)
	fmt.Printf("%x\n", enc2)
	var cu2 randv2.ChaCha8
	if err := cu2.UnmarshalBinary(enc2); err != nil {
		fmt.Println("unmarshal2 err:", err)
	}
	roundtrip2 := true
	for i := 0; i < 10; i++ {
		if cp.Uint64() != cu2.Uint64() {
			roundtrip2 = false
		}
	}
	fmt.Println(roundtrip2)
	// Malformed encodings must be rejected, not crash.
	var bad randv2.ChaCha8
	fmt.Println(bad.UnmarshalBinary([]byte("garbage")) != nil)
	fmt.Println("chacha8 ok")
}
