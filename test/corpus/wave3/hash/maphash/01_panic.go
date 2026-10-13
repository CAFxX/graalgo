// hash/maphash 01: zero-Seed use panics with the exact stdlib message.
package main

import (
	"hash/maphash"
)

func main() {
	// Bytes with the zero Seed panics before hashing anything.
	_ = maphash.Bytes(maphash.Seed{}, []byte("x"))
}
