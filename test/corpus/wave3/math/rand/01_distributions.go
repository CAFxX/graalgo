package main

import (
	"fmt"
	"math/rand"
)

func main() {
	// ExpFloat64 / NormFloat64 with fixed seeds (deterministic streams).
	r := rand.New(rand.NewSource(11))
	for i := 0; i < 5; i++ {
		fmt.Printf("%.17g\n", r.ExpFloat64())
	}
	for i := 0; i < 5; i++ {
		fmt.Printf("%.17g\n", r.NormFloat64())
	}
	// Zipf with fixed seed: Uint64 stream pins the rejection-inversion.
	z := rand.NewZipf(rand.New(rand.NewSource(13)), 1.07, 2.0, 100)
	if z == nil {
		fmt.Println("nil zipf")
		return
	}
	for i := 0; i < 8; i++ {
		fmt.Println(z.Uint64())
	}
	// Int63n edge: n=1 always 0; Intn panics on n<=0 are separate programs.
	r2 := rand.New(rand.NewSource(17))
	fmt.Println(r2.Int63n(1), r2.Int31n(1), r2.Intn(1))
}
