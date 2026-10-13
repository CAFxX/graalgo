// hash/fnv 03: adversarial — 128-bit vs 64-bit avalanche on 1-bit
// input changes, large input chunking, FNV-1 vs FNV-1a divergence on
// multi-byte input, all-256 byte values.
package main

import (
	"bytes"
	"fmt"
	"hash"
	"hash/fnv"
)

func newHash(name string) hash.Hash {
	switch name {
	case "32":
		return fnv.New32()
	case "32a":
		return fnv.New32a()
	case "64":
		return fnv.New64()
	case "64a":
		return fnv.New64a()
	case "128":
		return fnv.New128()
	case "128a":
		return fnv.New128a()
	}
	return nil
}

func main() {
	// 1-bit input change avalanches all six hashes.
	names := []string{"32", "32a", "64", "64a", "128", "128a"}
	for _, name := range names {
		a := newHash(name)
		a.Write([]byte{0x00})
		b := newHash(name)
		b.Write([]byte{0x01})
		fmt.Println(name, !bytes.Equal(a.Sum(nil), b.Sum(nil))) // true x6
	}

	// FNV-1 and FNV-1a agree on empty input but diverge on "a".
	// (Empty agreement is pinned in 00_vectors; divergence here.)
	e1, e1a := fnv.New32(), fnv.New32a()
	fmt.Println(bytes.Equal(e1.Sum(nil), e1a.Sum(nil))) // true
	e1.Write([]byte("a"))
	e1a.Write([]byte("a"))
	fmt.Println(!bytes.Equal(e1.Sum(nil), e1a.Sum(nil))) // true

	// All 256 byte values, single bulk write vs byte-at-a-time, 128-bit.
	all := make([]byte, 256)
	for i := range all {
		all[i] = byte(i)
	}
	bulk := fnv.New128a()
	bulk.Write(all)
	incr := fnv.New128a()
	for _, b := range all {
		incr.Write([]byte{b})
	}
	fmt.Println(bytes.Equal(bulk.Sum(nil), incr.Sum(nil))) // true

	// 1MB chunked hashing, 64-bit.
	big := bytes.Repeat([]byte{0x77}, 1<<20)
	h := fnv.New64a()
	for i := 0; i < len(big); i += 8192 {
		h.Write(big[i : i+8192])
	}
	one := fnv.New64a()
	one.Write(big)
	fmt.Println(bytes.Equal(h.Sum(nil), one.Sum(nil))) // true
	fmt.Printf("%016x\n", one.Sum(nil))
}
