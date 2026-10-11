package main

// Nil UUID, Equal, and Must (single-result wrapper).
import (
	"fmt"

	"github.com/google/uuid"
)

func mustText(s string) (uuid.UUID, error) {
	var u uuid.UUID
	err := u.UnmarshalText([]byte(s))
	return u, err
}

func main() {
	fmt.Println(uuid.Nil.String())
	a := uuid.Must(mustText("f47ac10b-58cc-0372-8567-0e02b2c3d479"))
	b := uuid.Must(mustText("f47ac10b-58cc-0372-8567-0e02b2c3d479"))
	c := uuid.Must(mustText("6ba7b810-9dad-11d1-80b4-00c04fd430c8"))
	fmt.Println(a == b, a == c, a == uuid.Nil)
}
