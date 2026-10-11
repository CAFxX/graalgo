package main

// Text round-trip: String -> UnmarshalText -> String, plus Equal.
import (
	"fmt"

	"github.com/google/uuid"
)

func main() {
	var u uuid.UUID
	if err := u.UnmarshalText([]byte("f47ac10b-58cc-0372-8567-0e02b2c3d479")); err != nil {
		panic(err)
	}
	s := u.String()
	fmt.Println(s)
	var u2 uuid.UUID
	if err := u2.UnmarshalText([]byte(s)); err != nil {
		panic(err)
	}
	fmt.Println(u2 == u)
	fmt.Println(u2.String() == s)
}
