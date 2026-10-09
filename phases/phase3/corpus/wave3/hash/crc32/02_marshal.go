// hash/crc32 02: marshal round-trip — mid-stream state save/restore,
// AppendBinary, Clone, and the exact error strings for corrupt blobs
// (bad magic, bad size, table mismatch).
package main

import (
	"encoding"
	"fmt"
	"hash"
	"hash/crc32"
)

func main() {
	h := crc32.NewIEEE()
	h.Write([]byte("hello "))

	m, err := h.(encoding.BinaryMarshaler).MarshalBinary()
	fmt.Println(err == nil, len(m)) // true 12 (4 magic + 4 tablesum + 4 crc)
	fmt.Printf("%x\n", m[:4])       // 63726301 ("crc\x01")

	h2 := crc32.NewIEEE()
	err = h2.(encoding.BinaryUnmarshaler).UnmarshalBinary(m)
	fmt.Println(err == nil) // true
	h.Write([]byte("world"))
	h2.Write([]byte("world"))
	fmt.Println(h.(hash.Hash32).Sum32() == h2.(hash.Hash32).Sum32()) // true

	// AppendBinary appends to the given slice.
	h3 := crc32.New(crc32.MakeTable(crc32.Koopman))
	a, err := h3.(encoding.BinaryAppender).AppendBinary([]byte{0xFF})
	fmt.Println(err == nil, len(a) == 13, a[0] == 0xFF) // true true true

	// Clone continues independently.
	h4 := crc32.NewIEEE()
	h4.Write([]byte("abc"))
	c, err := h4.(hash.Cloner).Clone()
	fmt.Println(err == nil) // true
	ch := c.(hash.Hash32)
	ch.Write([]byte("d"))
	h4.Write([]byte("e"))
	fmt.Println(ch.Sum32() != h4.(hash.Hash32).Sum32()) // true
	fmt.Println(ch.Sum32() == crc32.ChecksumIEEE([]byte("abcd"))) // true

	// Corrupt blobs: exact error strings.
	err = h2.(encoding.BinaryUnmarshaler).UnmarshalBinary([]byte{0, 0, 0, 0})
	fmt.Println(err.Error()) // hash/crc32: invalid hash state identifier
	err = h2.(encoding.BinaryUnmarshaler).UnmarshalBinary(m[:11])
	fmt.Println(err.Error()) // hash/crc32: invalid hash state size
	err = h2.(encoding.BinaryUnmarshaler).UnmarshalBinary(append(m, 0))
	fmt.Println(err.Error()) // hash/crc32: invalid hash state size

	// Table mismatch: state saved with IEEE, restored into Koopman digest.
	hk := crc32.New(crc32.MakeTable(crc32.Koopman))
	err = hk.(encoding.BinaryUnmarshaler).UnmarshalBinary(m)
	fmt.Println(err.Error()) // hash/crc32: tables do not match
}
