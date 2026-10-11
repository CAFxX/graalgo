package main

// Parse a fixed UUID string via UnmarshalText and round-trip through String().
import (
	"fmt"

	"github.com/google/uuid"
)

func main() {
	var u uuid.UUID
	if err := u.UnmarshalText([]byte("f47ac10b-58cc-0372-8567-0e02b2c3d479")); err != nil {
		panic(err)
	}
	fmt.Println(u.String())
	fmt.Println(u.String() == "f47ac10b-58cc-0372-8567-0e02b2c3d479")
}
