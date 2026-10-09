package main

import (
	"fmt"
	"math/rand"
)

func main() {
	// NewSource with fixed seeds: Int63/Uint64 streams are deterministic
	// and must match gc bit-exactly (Mitchell-Reeds core).
	for _, seed := range []int64{1, 42, -1, 0, 1<<62} {
		src := rand.NewSource(seed)
		fmt.Println(src.Int63())
		fmt.Println(src.Int63())
	}
	// Source64: Uint64 uses the direct path.
	s64 := rand.NewSource(12345).(rand.Source64)
	fmt.Println(s64.Uint64())
	fmt.Println(s64.Uint64())
	// Seed determinism: same seed -> same stream; re-seed resets.
	a := rand.New(rand.NewSource(99))
	b := rand.New(rand.NewSource(99))
	fmt.Println(a.Int63() == b.Int63(), a.Uint64() == b.Uint64())
	a.Seed(7)
	b.Seed(7)
	fmt.Println(a.Int63() == b.Int63())
	// Uint32 / Int31 / Intn / Int63n / Int31n / Int / Float32 / Float64.
	r := rand.New(rand.NewSource(2024))
	fmt.Println(r.Uint32())
	fmt.Println(r.Int31())
	fmt.Println(r.Int63n(1000))
	fmt.Println(r.Int31n(1000))
	fmt.Println(r.Intn(1000))
	fmt.Println(r.Int())
	fmt.Printf("%.17g\n", r.Float64())
	// Shuffle / Perm with fixed seed.
	r2 := rand.New(rand.NewSource(5))
	fmt.Println(r2.Perm(10))
	xs := []int{1, 2, 3, 4, 5, 6, 7, 8}
	r2.Shuffle(len(xs), func(i, j int) { xs[i], xs[j] = xs[j], xs[i] })
	fmt.Println(xs)
}
