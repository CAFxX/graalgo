package main

import (
	"crypto/x509"
	"fmt"
)

func main() {
	// Test x509.CertPool (now supported via synthetic crypto/x509)
	pool := x509.NewCertPool()
	fmt.Println("pool ok:", pool != nil)

	// Test AppendCertsFromPEM with empty data (should return false)
	ok := pool.AppendCertsFromPEM([]byte{})
	fmt.Println("empty pem:", !ok)

	// Test Subjects (should be empty)
	subjects := pool.Subjects()
	fmt.Println("subjects empty:", len(subjects) == 0)

	// Test Clone
	clone := pool.Clone()
	fmt.Println("clone ok:", clone != nil)
	fmt.Println("clone equal:", pool.Equal(clone))
}
