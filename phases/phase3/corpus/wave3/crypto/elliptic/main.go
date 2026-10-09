package main

import (
	"crypto/elliptic"
	"fmt"
)

func main() {
	c := elliptic.P256()
	// ScalarBaseMult with a fixed scalar
	scalar := []byte{1, 2, 3, 4, 5}
	x, y := c.ScalarBaseMult(scalar)
	fmt.Printf("x=%x\n", x)
	fmt.Printf("y=%x\n", y)
	// Params
	p := c.Params()
	fmt.Printf("name=%s bits=%d\n", p.Name, p.BitSize)
}
