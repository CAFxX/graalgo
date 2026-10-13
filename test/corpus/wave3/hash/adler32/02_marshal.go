// hash/adler32 02: marshal round-trip — MarshalBinary/UnmarshalBinary
// state save/restore, AppendBinary, Clone, and the exact error strings
// for corrupt state blobs.
package main

import (
	"encoding"
	"fmt"
	"hash"
	"hash/adler32"
)

func main() {
	h := adler32.New()
	h.Write([]byte("hello "))

	// Marshal the mid-stream state.
	m, err := h.(encoding.BinaryMarshaler).MarshalBinary()
	fmt.Println(err == nil, len(m)) // true 8 (4 magic + 4 state)
	fmt.Printf("%x\n", m[:4])       // 61646c01 ("adl\x01")

	// Restore into a fresh hash and continue identically.
	h2 := adler32.New()
	err = h2.(encoding.BinaryUnmarshaler).UnmarshalBinary(m)
	fmt.Println(err == nil) // true
	h.Write([]byte("world"))
	h2.Write([]byte("world"))
	fmt.Println(h.Sum32() == h2.Sum32()) // true

	// AppendBinary appends to the given slice.
	h3 := adler32.New()
	a, err := h3.(encoding.BinaryAppender).AppendBinary([]byte{0xFF})
	fmt.Println(err == nil) // true
	fmt.Println(len(a) == 9, a[0] == 0xFF) // true true

	// Clone continues independently.
	h4 := adler32.New()
	h4.Write([]byte("abc"))
	c, err := h4.(hash.Cloner).Clone()
	fmt.Println(err == nil) // true
	ch := c.(hash.Hash32)
	ch.Write([]byte("d"))
	h4.Write([]byte("e"))
	fmt.Println(ch.Sum32() != h4.Sum32())              // true
	fmt.Printf("%08x\n", ch.Sum32())                   // == Checksum("abcd")
	fmt.Println(ch.Sum32() == adler32.Checksum([]byte("abcd"))) // true

	// Corrupt blobs: exact error strings.
	badMagic := []byte{0, 0, 0, 0, 0, 0, 0, 0}
	err = h2.(encoding.BinaryUnmarshaler).UnmarshalBinary(badMagic)
	fmt.Println(err.Error()) // hash/adler32: invalid hash state identifier
	err = h2.(encoding.BinaryUnmarshaler).UnmarshalBinary(m[:7])
	fmt.Println(err.Error()) // hash/adler32: invalid hash state identifier
	err = h2.(encoding.BinaryUnmarshaler).UnmarshalBinary(append(m, 0))
	fmt.Println(err.Error()) // hash/adler32: invalid hash state size
}
