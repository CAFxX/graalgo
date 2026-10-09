package main

import (
	"crypto/tls"
	"fmt"
)

func main() {
	// crypto/tls is scope-limited in frontend v1 (no synthetic source).
	// This program validates under gc only.
	cfg := &tls.Config{InsecureSkipVerify: true}
	fmt.Println("config ok:", cfg != nil)
}
