package main

// Version and Variant of known v1/v4 UUIDs.
import (
	"fmt"

	"github.com/google/uuid"
)

func mustText(s string) uuid.UUID {
	var u uuid.UUID
	if err := u.UnmarshalText([]byte(s)); err != nil {
		panic(err)
	}
	return u
}

func main() {
	v1 := mustText("6ba7b810-9dad-11d1-80b4-00c04fd430c8")
	fmt.Println(v1.Version(), v1.Variant())
	v4 := mustText("f47ac10b-58cc-4372-a567-0e02b2c3d479")
	fmt.Println(v4.Version(), v4.Variant())
}
