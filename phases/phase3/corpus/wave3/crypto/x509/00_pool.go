package main

import (
	"crypto/x509"
	"fmt"
)

func main() {
	// x509 is scope-limited in frontend v1 (no synthetic source).
	// This program validates under gc only.
	pool := x509.NewCertPool()
	fmt.Println("pool ok:", pool != nil)
}
