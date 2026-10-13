// hash/fnv 00: FNV test vectors for all six constructors.
// Values verified against gc 1.27.1 before pinning.
package main

import (
	"fmt"
	"hash"
	"hash/fnv"
)

type hash32 interface {
	Sum32() uint32
}

type hash64 interface {
	Sum64() uint64
}

func main() {
	// Empty inputs hit the offset basis.
	fmt.Printf("%08x\n", fnv.New32().(hash32).Sum32())   // 811c9dc5
	fmt.Printf("%08x\n", fnv.New32a().(hash32).Sum32())  // 811c9dc5
	fmt.Printf("%016x\n", fnv.New64().(hash64).Sum64())  // cbf29ce484222325
	fmt.Printf("%016x\n", fnv.New64a().(hash64).Sum64()) // cbf29ce484222325
	fmt.Printf("%x\n", fnv.New128().Sum(nil))           // 6c62272e07bb014262b821756295c58d
	fmt.Printf("%x\n", fnv.New128a().Sum(nil))          // 6c62272e07bb014262b821756295c58d

	w := func(h hash.Hash, s string) {
		h.Write([]byte(s))
	}
	h32 := fnv.New32()
	w(h32, "foobar")
	fmt.Printf("%08x\n", h32.(hash32).Sum32()) // 31f0b262
	h32a := fnv.New32a()
	w(h32a, "foobar")
	fmt.Printf("%08x\n", h32a.(hash32).Sum32()) // bf9cf968
	h64 := fnv.New64()
	w(h64, "foobar")
	fmt.Printf("%016x\n", h64.(hash64).Sum64()) // 340d8765a4dda9c2
	h64a := fnv.New64a()
	w(h64a, "foobar")
	fmt.Printf("%016x\n", h64a.(hash64).Sum64()) // 85944171f73967e8
	h128 := fnv.New128()
	w(h128, "foobar")
	fmt.Printf("%x\n", h128.Sum(nil)) // 7896bfea9c3c64bf6dc58353d2c293aa
	h128a := fnv.New128a()
	w(h128a, "foobar")
	fmt.Printf("%x\n", h128a.Sum(nil)) // 343e1662793c64bf6f0d3597ba446f18

	// Sizes and block sizes.
	fmt.Println(fnv.New32().Size(), fnv.New32().BlockSize())     // 4 1
	fmt.Println(fnv.New64().Size(), fnv.New64().BlockSize())     // 8 1
	fmt.Println(fnv.New128().Size(), fnv.New128().BlockSize())   // 16 1
	fmt.Println(fnv.New128a().Size(), fnv.New128a().BlockSize()) // 16 1
}
