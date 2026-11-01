package main

import (
	"bytes"
	"fmt"
	"io"
	randv2 "math/rand/v2"
)

// R4: ChaCha8 differential case. Formerly scope-limited
// (01_chacha8.scopelimited: "& on SelectorExpr not supported"); the
// frontend now exports &field and pointer-receiver calls on fields,
// and ChaCha8.Read/AppendBinary/MarshalBinary are served by
// __-helpers, so the whole type — not just Uint64 — is pinned here
// byte-identically vs gc. Fixed seeds only.

func readAll(c *randv2.ChaCha8, n int) []byte {
	buf := make([]byte, n)
	m, err := c.Read(buf)
	if m != n || err != nil {
		panic("short read")
	}
	return buf
}

func readVia(r io.Reader, n int) []byte {
	buf := make([]byte, n)
	m, err := r.Read(buf)
	if m != n || err != nil {
		panic("short interface read")
	}
	return buf
}

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

	// Read at fixed lengths from fresh instances: the 1/7-byte reads
	// exercise the readBuf tail, 8/16 cross whole words, 256 crosses
	// a refill, 300 crosses into the next block.
	for _, n := range []int{0, 1, 7, 8, 9, 16, 17, 64, 100, 256, 300} {
		fmt.Printf("read %d: %x\n", n, readAll(randv2.NewChaCha8(seed), n))
	}
	// Sequential reads on one instance, past refill and reseed
	// boundaries (a refill yields 256 bytes; reseed follows the 4th).
	seq := randv2.NewChaCha8(seed)
	fmt.Printf("seq 3: %x\n", readAll(seq, 3))
	fmt.Printf("seq 5: %x\n", readAll(seq, 5))
	fmt.Printf("seq 1500: %x\n", readAll(seq, 1500))
	// The same sequential reads through the io.Reader interface:
	// this is the dispatch path crypto/ecdsa signLegacy uses.
	iseq := randv2.NewChaCha8(seed)
	fmt.Printf("iface 3: %x\n", readVia(iseq, 3))
	fmt.Printf("iface 5: %x\n", readVia(iseq, 5))
	fmt.Printf("iface 1500: %x\n", readVia(iseq, 1500))
	// Read and Uint64 on separate instances agree byte-for-byte:
	// Read's LE word emission must match the Uint64 stream.
	ru := randv2.NewChaCha8(seed)
	rr := randv2.NewChaCha8(seed)
	var words [4]uint64
	for i := range words {
		words[i] = ru.Uint64()
	}
	got := readAll(rr, 32)
	want := make([]byte, 0, 32)
	for _, w := range words {
		for i := 0; i < 8; i++ {
			want = append(want, byte(w>>(8*i)))
		}
	}
	fmt.Println("read==uint64:", bytes.Equal(got, want))
	// Seed after a partial read resets the readBuf: the stream
	// restarts exactly, tail included.
	partial := randv2.NewChaCha8(seed)
	_ = readAll(partial, 3)
	partial.Seed(seed)
	fresh := randv2.NewChaCha8(seed)
	fmt.Println("seed-resets-readbuf:", bytes.Equal(readAll(partial, 8), readAll(fresh, 8)))
	// Marshal/Unmarshal round-trip, plain and with a partial read
	// pending (the "readbuf:" prefix path).
	m0 := randv2.NewChaCha8(seed)
	enc0, err := m0.MarshalBinary()
	if err != nil {
		panic(err)
	}
	m0r := randv2.NewChaCha8(zseed)
	if err := m0r.UnmarshalBinary(enc0); err != nil {
		panic(err)
	}
	fmt.Println("marshal-plain:", bytes.Equal(readAll(m0, 64), readAll(m0r, 64)))
	mp := randv2.NewChaCha8(seed)
	_ = readAll(mp, 3)
	encp, err := mp.MarshalBinary()
	if err != nil {
		panic(err)
	}
	mpr := randv2.NewChaCha8(zseed)
	if err := mpr.UnmarshalBinary(encp); err != nil {
		panic(err)
	}
	fmt.Println("marshal-partial:", bytes.Equal(readAll(mp, 64), readAll(mpr, 64)))
	// AppendBinary with a nil base equals MarshalBinary on an
	// equal-state instance; UnmarshalBinary rejects garbage.
	ab, err := randv2.NewChaCha8(seed).AppendBinary(nil)
	if err != nil {
		panic(err)
	}
	mb, err := randv2.NewChaCha8(seed).MarshalBinary()
	if err != nil {
		panic(err)
	}
	fmt.Println("append==marshal:", bytes.Equal(ab, mb))
	bad := randv2.NewChaCha8(seed)
	fmt.Println("unmarshal-bad-err:", bad.UnmarshalBinary([]byte("bogus")) != nil)
	fmt.Println("chacha8 ok")
}
