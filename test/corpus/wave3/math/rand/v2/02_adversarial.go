package main

import (
	"fmt"
	randv2 "math/rand/v2"
)

func main() {
	// Uint64N/Int64N/Uint32N with large n (rejection paths).
	r := randv2.New(randv2.NewPCG(0xDEADBEEF, 0xCAFEBABE))
	fmt.Println(r.Uint64N(1 << 63))
	fmt.Println(r.Int64N(1 << 62))
	fmt.Println(r.Uint32N(1 << 31))
	fmt.Println(r.IntN(1 << 30))
	// N=1 always 0.
	fmt.Println(r.Uint64N(1), r.Int64N(1), r.Uint32N(1), r.IntN(1))
	// Float64/Float32 in [0,1).
	for i := 0; i < 100; i++ {
		if f := r.Float64(); f < 0 || f >= 1 {
			fmt.Println("Float64 out of range")
		}
		if f := r.Float32(); f < 0 || f >= 1 {
			fmt.Println("Float32 out of range")
		}
	}
	fmt.Println("range ok")
	// Shuffle/Perm.
	fmt.Println(r.Perm(8))
	xs := []int{1, 2, 3, 4, 5, 6}
	r.Shuffle(len(xs), func(i, j int) { xs[i], xs[j] = xs[j], xs[i] })
	fmt.Println(xs)
	// Int/Uint full width.
	fmt.Printf("%x %x\n", r.Int(), r.Uint())
	// Two Rands, same PCG seeds, interleave identically.
	a := randv2.New(randv2.NewPCG(5, 6))
	b := randv2.New(randv2.NewPCG(5, 6))
	match := true
	for i := 0; i < 500; i++ {
		if a.Uint64() != b.Uint64() {
			match = false
		}
	}
	fmt.Println(match)
}
