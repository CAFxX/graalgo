package main

import (
	"fmt"
	"math/rand"
)

func main() {
	// Intn/Int63n/Int31n with large n (rejection sampling paths).
	r := rand.New(rand.NewSource(23))
	fmt.Println(r.Intn(1 << 62))
	fmt.Println(r.Int63n(1 << 62))
	fmt.Println(r.Int31n(1<<31 - 1))
	// Float64 range: [0,1).
	for i := 0; i < 100; i++ {
		f := r.Float64()
		if f < 0 || f >= 1 {
			fmt.Println("Float64 out of range")
		}
	}
	fmt.Println("range ok")
	// Uint64 full-width: top bit set sometimes (prints as hex).
	r2 := rand.New(rand.NewSource(29))
	for i := 0; i < 4; i++ {
		fmt.Printf("%x\n", r2.Uint64())
	}
	// Two Rands with the same seed interleave identically.
	a := rand.New(rand.NewSource(31))
	b := rand.New(rand.NewSource(31))
	match := true
	for i := 0; i < 1000; i++ {
		if a.Int63() != b.Int63() {
			match = false
		}
	}
	fmt.Println(match)
	// Seed(0) is a valid seed (not a no-op reset).
	c := rand.New(rand.NewSource(0))
	fmt.Println(c.Int63())
}
