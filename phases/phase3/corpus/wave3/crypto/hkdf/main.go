package main

import (
	"crypto/hkdf"
	"crypto/sha256"
	"fmt"
)

func main() {
	key, err := hkdf.Key(sha256.New, []byte("secret"), []byte("salt"), "info", 32)
	if err != nil {
		panic(err)
	}
	fmt.Printf("key=%x\n", key)
}
