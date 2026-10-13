package main

import (
	"encoding/base64"
	"encoding/pem"
	"fmt"
)

func main() {
	// Build a block and encode it.
	block := &pem.Block{
		Type:    "TEST BLOCK",
		Headers: map[string]string{"X-Custom": "yes"},
		Bytes:   []byte("hello pem world"),
	}
	enc := pem.EncodeToMemory(block)
	fmt.Println(string(enc))

	// Decode it back (multi-result with non-error second result).
	p, rest := pem.Decode(enc)
	fmt.Println(p != nil, len(rest) == 0)
	fmt.Println(p.Type)
	fmt.Println(p.Headers["X-Custom"])
	fmt.Println(string(p.Bytes))

	// The payload is base64 of the raw bytes.
	fmt.Println(base64.StdEncoding.EncodeToString(block.Bytes))

	// Multiple blocks: Decode returns the first, rest holds the second.
	two := append(enc, enc...)
	p1, rest1 := pem.Decode(two)
	fmt.Println(p1 != nil, len(rest1) > 0)
	p2, rest2 := pem.Decode(rest1)
	fmt.Println(p2 != nil, len(rest2) == 0)
}
