package main

import "math/rand"

// Intn panics on n <= 0.
func main() {
	r := rand.New(rand.NewSource(1))
	r.Intn(0)
}
