// hash/fnv 02: marshal round-trips for all six hashes, exact error
// strings, marshal-then-continue equivalence.
package main

import (
	"bytes"
	"encoding"
	"fmt"
	"hash"
	"hash/fnv"
)

func newHash(name string) hash.Hash {
	switch name {
	case "32":
		return fnv.New32()
	case "32a":
		return fnv.New32a()
	case "64":
		return fnv.New64()
	case "64a":
		return fnv.New64a()
	case "128":
		return fnv.New128()
	case "128a":
		return fnv.New128a()
	}
	return nil
}

func main() {
	magics := map[string]string{
		"32": "666e7601", "32a": "666e7602", "64": "666e7603",
		"64a": "666e7604", "128": "666e7605", "128a": "666e7606",
	}
	sizes := map[string]int{"32": 8, "32a": 8, "64": 12, "64a": 12, "128": 20, "128a": 20}
	names := []string{"32", "32a", "64", "64a", "128", "128a"}
	for _, name := range names {
		h := newHash(name)
		h.Write([]byte("hello "))
		m, err := h.(encoding.BinaryMarshaler).MarshalBinary()
		fmt.Println(name, err == nil, len(m) == sizes[name]) // true x6
		fmt.Println(name, fmt.Sprintf("%x", m[:4]) == magics[name]) // true x6

		// Restore and continue: identical to uninterrupted hashing.
		h2 := newHash(name)
		if err := h2.(encoding.BinaryUnmarshaler).UnmarshalBinary(m); err != nil {
			fmt.Println(name, "unmarshal failed")
			continue
		}
		h.Write([]byte("world"))
		h2.Write([]byte("world"))
		fmt.Println(name, bytes.Equal(h.Sum(nil), h2.Sum(nil))) // true x6

		// Corrupt blobs: exact error strings.
		err = h2.(encoding.BinaryUnmarshaler).UnmarshalBinary([]byte{1, 2, 3, 4})
		fmt.Println(name, err.Error()) // hash/fnv: invalid hash state identifier
		short := m[:len(m)-1]
		err = h2.(encoding.BinaryUnmarshaler).UnmarshalBinary(short)
		fmt.Println(name, err.Error()) // hash/fnv: invalid hash state size
	}

	// Cross-magic: 32-state into a 32a hash is rejected by magic.
	h32 := fnv.New32()
	m32, _ := h32.(encoding.BinaryMarshaler).MarshalBinary()
	h32a := fnv.New32a()
	err := h32a.(encoding.BinaryUnmarshaler).UnmarshalBinary(m32)
	fmt.Println(err.Error()) // hash/fnv: invalid hash state identifier
}
