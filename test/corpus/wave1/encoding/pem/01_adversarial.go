package main

import (
	"encoding/pem"
	"fmt"
)

func main() {
	// No PEM block at all: p is nil, rest is the whole input.
	data := []byte("this is not pem")
	p, rest := pem.Decode(data)
	fmt.Println(p == nil, string(rest) == string(data))

	// Empty input.
	p2, rest2 := pem.Decode(nil)
	fmt.Println(p2 == nil, len(rest2) == 0)

	// Leading garbage is skipped.
	block := &pem.Block{Type: "X", Bytes: []byte{1, 2, 3}}
	enc := pem.EncodeToMemory(block)
	padded := append([]byte("junk junk junk\n"), enc...)
	p3, rest3 := pem.Decode(padded)
	fmt.Println(p3 != nil, p3.Type, len(rest3) == 0)

	// Block without headers.
	bare := &pem.Block{Type: "BARE", Bytes: []byte("data")}
	e2 := pem.EncodeToMemory(bare)
	p4, _ := pem.Decode(e2)
	fmt.Println(p4.Type, len(p4.Headers) == 0, string(p4.Bytes))

	// EncodeToMemory of a block with empty bytes.
	empty := &pem.Block{Type: "EMPTY"}
	fmt.Println(len(pem.EncodeToMemory(empty)) > 0)
}
