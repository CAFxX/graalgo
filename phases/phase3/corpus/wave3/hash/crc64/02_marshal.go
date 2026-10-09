// hash/crc64 02: marshal round-trip — mid-stream state save/restore,
// AppendBinary, Clone, exact error strings (bad magic, bad size, table
// mismatch).
package main

import (
	"encoding"
	"fmt"
	"hash"
	"hash/crc64"
)

func main() {
	h := crc64.New(crc64.MakeTable(crc64.ECMA))
	h.Write([]byte("hello "))

	m, err := h.(encoding.BinaryMarshaler).MarshalBinary()
	fmt.Println(err == nil, len(m)) // true 20 (4 magic + 8 tablesum + 8 crc)
	fmt.Printf("%x\n", m[:4])       // 63726302 ("crc\x02")

	h2 := crc64.New(crc64.MakeTable(crc64.ECMA))
	err = h2.(encoding.BinaryUnmarshaler).UnmarshalBinary(m)
	fmt.Println(err == nil) // true
	h.Write([]byte("world"))
	h2.Write([]byte("world"))
	fmt.Println(h.(hash.Hash64).Sum64() == h2.(hash.Hash64).Sum64()) // true

	a, err := h.(encoding.BinaryAppender).AppendBinary([]byte{0xFF})
	fmt.Println(err == nil, len(a) == 21, a[0] == 0xFF) // true true true

	h4 := crc64.New(crc64.MakeTable(crc64.ISO))
	h4.Write([]byte("abc"))
	c, err := h4.(hash.Cloner).Clone()
	fmt.Println(err == nil) // true
	ch := c.(hash.Hash64)
	ch.Write([]byte("d"))
	h4.Write([]byte("e"))
	fmt.Println(ch.Sum64() != h4.(hash.Hash64).Sum64()) // true
	fmt.Println(ch.Sum64() == crc64.Checksum([]byte("abcd"), crc64.MakeTable(crc64.ISO))) // true

	err = h2.(encoding.BinaryUnmarshaler).UnmarshalBinary([]byte{0, 0, 0, 0})
	fmt.Println(err.Error()) // hash/crc64: invalid hash state identifier
	err = h2.(encoding.BinaryUnmarshaler).UnmarshalBinary(m[:19])
	fmt.Println(err.Error()) // hash/crc64: invalid hash state size
	err = h2.(encoding.BinaryUnmarshaler).UnmarshalBinary(append(m, 0))
	fmt.Println(err.Error()) // hash/crc64: invalid hash state size

	// Table mismatch: ECMA state restored into an ISO digest.
	hi := crc64.New(crc64.MakeTable(crc64.ISO))
	err = hi.(encoding.BinaryUnmarshaler).UnmarshalBinary(m)
	fmt.Println(err.Error()) // hash/crc64: tables do not match
}
