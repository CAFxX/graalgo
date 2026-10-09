package main

import (
	"crypto/ecdh"
	"crypto/rand"
	"fmt"
)

func main() {
	// X25519 key exchange
	priv1, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	priv2, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	secret1, err := priv1.ECDH(priv2.PublicKey())
	if err != nil {
		panic(err)
	}
	secret2, err := priv2.ECDH(priv1.PublicKey())
	if err != nil {
		panic(err)
	}
	fmt.Printf("secret1=%x\n", secret1)
	fmt.Printf("secret2=%x\n", secret2)
	fmt.Printf("equal=%v\n", string(secret1) == string(secret2))
}
