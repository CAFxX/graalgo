// hash/maphash 02: generic surface — Comparable/WriteComparable/
// ComparableHasher consistency for basic kinds. All assertions are
// structural (no gc-value pinning; see package divergence note).
package main

import (
	"fmt"
	"hash/maphash"
)

func main() {
	seed := maphash.MakeSeed()

	// Comparable is deterministic for a fixed seed.
	fmt.Println(maphash.Comparable(seed, "hello") == maphash.Comparable(seed, "hello")) // true
	fmt.Println(maphash.Comparable(seed, 42) == maphash.Comparable(seed, 42))           // true
	fmt.Println(maphash.Comparable(seed, -7) == maphash.Comparable(seed, -7))           // true

	// Distinct values (almost surely) hash distinctly per kind.
	fmt.Println(maphash.Comparable(seed, "a") != maphash.Comparable(seed, "b")) // true
	fmt.Println(maphash.Comparable(seed, 1) != maphash.Comparable(seed, 2))     // true

	// ComparableHasher.Hash agrees with Comparable.
	h1 := new(maphash.Hash)
	h1.SetSeed(seed)
	maphash.ComparableHasher[string]{}.Hash(h1, "hello")
	fmt.Println(h1.Sum64() == maphash.Comparable(seed, "hello")) // true

	h2 := new(maphash.Hash)
	h2.SetSeed(seed)
	maphash.ComparableHasher[int]{}.Hash(h2, 42)
	fmt.Println(h2.Sum64() == maphash.Comparable(seed, 42)) // true

	// WriteComparable agrees with Comparable.
	h3 := new(maphash.Hash)
	h3.SetSeed(seed)
	maphash.WriteComparable(h3, 3.5)
	fmt.Println(h3.Sum64() == maphash.Comparable(seed, 3.5)) // true

	// Equal is plain ==.
	fmt.Println(maphash.ComparableHasher[int]{}.Equal(1, 1))  // true
	fmt.Println(!maphash.ComparableHasher[int]{}.Equal(1, 2)) // true

	// All basic kinds hash without panicking; determinism holds.
	h4 := new(maphash.Hash)
	h4.SetSeed(seed)
	maphash.WriteComparable(h4, true)
	maphash.WriteComparable(h4, int8(-8))
	maphash.WriteComparable(h4, uint64(1<<64-1))
	maphash.WriteComparable(h4, float32(1.5))
	maphash.WriteComparable(h4, complex128(1+2i))
	h5 := new(maphash.Hash)
	h5.SetSeed(seed)
	maphash.WriteComparable(h5, true)
	maphash.WriteComparable(h5, int8(-8))
	maphash.WriteComparable(h5, uint64(1<<64-1))
	maphash.WriteComparable(h5, float32(1.5))
	maphash.WriteComparable(h5, complex128(1+2i))
	fmt.Println(h4.Sum64() == h5.Sum64()) // true

	// Kind tags separate int(1) from int64(1) encodings structurally:
	// (no cross-kind equality is asserted; just that both run).
	fmt.Println(maphash.Comparable(seed, int(1)) != 0)   // true
	fmt.Println(maphash.Comparable(seed, int64(1)) != 0) // true
}
