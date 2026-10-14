package main

// Name-based UUIDs (v3/v5): deterministic generators.
import (
	"fmt"

	"github.com/google/uuid"
)

func main() {
	u3 := uuid.NewMD5(uuid.NameSpaceDNS, []byte("example.com"))
	fmt.Println(u3.String(), u3.Version())
	u5 := uuid.NewSHA1(uuid.NameSpaceDNS, []byte("example.com"))
	fmt.Println(u5.String(), u5.Version())
	// Determinism: same input, same output.
	fmt.Println(u3 == uuid.NewMD5(uuid.NameSpaceDNS, []byte("example.com")))
	fmt.Println(u5 == uuid.NewSHA1(uuid.NameSpaceDNS, []byte("example.com")))
}
