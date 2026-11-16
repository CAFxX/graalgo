package main

import (
	"bytes"
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
	// Read: varied lengths incl. sub-8, exact-8, and multi-8 chunks.
	for _, n := range []int{0, 1, 7, 8, 9, 16, 63, 64} {
		cr := randv2.NewChaCha8(seed)
		buf := make([]byte, n)
		m, err := cr.Read(buf)
		fmt.Printf("read n=%d m=%d err=%v %x\n", n, m, err, buf)
	}
	// Read determinism: two fresh instances, same reads.
	ca := randv2.NewChaCha8(seed)
	cb := randv2.NewChaCha8(seed)
	ba := make([]byte, 100)
	bb := make([]byte, 100)
	ca.Read(ba)
	cb.Read(bb)
	fmt.Println(bytes.Equal(ba, bb))
	// Interleaved Uint64/Read: the interleaving order is
	// implementation-defined, but two identically-interleaved
	// instances must agree.
	ia := randv2.NewChaCha8(seed)
	ib := randv2.NewChaCha8(seed)
	_ = ia.Uint64()
	_ = ib.Uint64()
	ra := make([]byte, 37)
	rb := make([]byte, 37)
	ia.Read(ra)
	ib.Read(rb)
	fmt.Println(bytes.Equal(ra, rb))
	fmt.Printf("%x\n", ia.Uint64())
	fmt.Printf("%x\n", ib.Uint64())
	// Marshal/Unmarshal round-trip with a pending readBuf (read 3
	// bytes so readLen > 0), then continue reading identically.
	ma := randv2.NewChaCha8(seed)
	tmp := make([]byte, 3)
	ma.Read(tmp)
	data, err := ma.MarshalBinary()
	fmt.Println(err)
	mb := randv2.NewChaCha8(zseed)
	err = mb.UnmarshalBinary(data)
	fmt.Println(err)
	ta := make([]byte, 64)
	tb := make([]byte, 64)
	ma.Read(ta)
	mb.Read(tb)
	fmt.Println(bytes.Equal(ta, tb))
	fmt.Println("chacha8 ok")
}
