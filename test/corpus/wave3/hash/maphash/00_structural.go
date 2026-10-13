// hash/maphash 00: structural properties (seed-agnostic, so the
// MakeSeed randomness cannot leak into the output). No exact hash
// values are pinned: the synthetic uses a P3-specific seeded hash,
// not gc's runtime memhash.
package main

import (
	"fmt"
	"hash/maphash"
)

func main() {
	seed := maphash.MakeSeed()
	data := []byte("The quick brown fox jumps over the lazy dog")

	// Chunking invariance: one Write vs splits vs WriteByte loop.
	h1 := new(maphash.Hash)
	h1.SetSeed(seed)
	h1.Write(data)

	h2 := new(maphash.Hash)
	h2.SetSeed(seed)
	h2.Write(data[:10])
	h2.Write(data[10:40])
	h2.Write(data[40:])

	h3 := new(maphash.Hash)
	h3.SetSeed(seed)
	for _, b := range data {
		h3.WriteByte(b)
	}

	h4 := new(maphash.Hash)
	h4.SetSeed(seed)
	h4.WriteString(string(data))

	s1, s2, s3, s4 := h1.Sum64(), h2.Sum64(), h3.Sum64(), h4.Sum64()
	fmt.Println(s1 == s2 && s2 == s3 && s3 == s4) // true

	// Bytes/String one-shots agree with the streaming Hash.
	fmt.Println(maphash.Bytes(seed, data) == s1)         // true
	fmt.Println(maphash.String(seed, string(data)) == s1) // true
	fmt.Println(maphash.Bytes(seed, data) == maphash.String(seed, string(data))) // true

	// Seed round-trips through Seed().
	h5 := new(maphash.Hash)
	h5.SetSeed(seed)
	fmt.Println(h5.Seed() == seed) // true

	// Reset restores the initial state.
	h1.Write([]byte("more"))
	h1.Reset()
	fmt.Println(h1.Sum64() == s1) // true

	// Clone: independent but equal continuation.
	h6 := new(maphash.Hash)
	h6.SetSeed(seed)
	h6.Write(data[:20])
	c, err := h6.Clone()
	fmt.Println(err == nil) // true
	ch := c.(*maphash.Hash)
	ch.Write(data[20:])
	h6.Write(data[20:])
	fmt.Println(ch.Sum64() == h6.Sum64()) // true

	// Seed sensitivity: different seeds (almost surely) differ.
	seed2 := maphash.MakeSeed()
	ha, hb := new(maphash.Hash), new(maphash.Hash)
	ha.SetSeed(seed)
	hb.SetSeed(seed2)
	ha.Write(data)
	hb.Write(data)
	fmt.Println(ha.Sum64() != hb.Sum64()) // true
	fmt.Println(seed != seed2)            // true

	// Sum appends BE of Sum64 and does not mutate.
	h7 := new(maphash.Hash)
	h7.SetSeed(seed)
	h7.Write(data)
	sum := h7.Sum([]byte{0xAA})
	s64 := h7.Sum64()
	be := uint64(sum[1])<<56 | uint64(sum[2])<<48 | uint64(sum[3])<<40 | uint64(sum[4])<<32 |
		uint64(sum[5])<<24 | uint64(sum[6])<<16 | uint64(sum[7])<<8 | uint64(sum[8])
	fmt.Println(len(sum) == 9, sum[0] == 0xAA, be == s64) // true true false (gc Sum is LE)
	fmt.Println(h7.Sum64() == s64)                        // true (no mutation)

	// Zero Hash is usable (random seed chosen lazily).
	hz := new(maphash.Hash)
	hz.Write(data)
	fmt.Println(hz.Sum64() != 0) // true (2^-64 false-negative class)

	// Size/BlockSize.
	fmt.Println(h7.Size(), h7.BlockSize()) // 8 128

	// Empty input is well-defined and chunking-invariant.
	e1, e2 := new(maphash.Hash), new(maphash.Hash)
	e1.SetSeed(seed)
	e2.SetSeed(seed)
	e2.Write(nil)
	fmt.Println(e1.Sum64() == e2.Sum64()) // true
	fmt.Println(maphash.Bytes(seed, nil) == e1.Sum64()) // true
}
