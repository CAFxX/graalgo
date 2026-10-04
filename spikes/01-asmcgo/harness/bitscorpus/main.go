// bitscorpus: emits a corpus of uint64 values (edge cases + random) as
// hex, one per line, for the math/bits <-> java.lang.Long differential.
package main

import (
	"fmt"
	"math/rand"
	"os"
)

func main() {
	f, err := os.Create(os.Args[1])
	if err != nil {
		panic(err)
	}
	defer f.Close()
	edges := []uint64{0, 1, 2, 0x7FFFFFFFFFFFFFFF, 0x8000000000000000, 0xFFFFFFFFFFFFFFFF,
		0xAAAAAAAAAAAAAAAA, 0x5555555555555555, 0x0F0F0F0F0F0F0F0F, 0x00FF00FF00FF00FF,
		1 << 63, (1 << 63) | 1, 0x0102030405060708}
	for _, e := range edges {
		fmt.Fprintf(f, "%016x\n", e)
	}
	rng := rand.New(rand.NewSource(20261003))
	for i := 0; i < 100000; i++ {
		fmt.Fprintf(f, "%016x\n", rng.Uint64())
	}
}
