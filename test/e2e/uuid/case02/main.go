package main

// UnmarshalText on invalid inputs: must return an error, not a value.
import (
	"fmt"

	"github.com/google/uuid"
)

func main() {
	for _, s := range []string{
		"",
		"not-a-uuid",
		"f47ac10b-58cc-0372-8567-0e02b2c3d47",   // too short
		"f47ac10b-58cc-0372-8567-0e02b2c3d4790", // too long
		"f47ac10b-58cc-0372-8567-0e02b2c3d47z",  // bad hex
		"f47ac10b_58cc_0372_8567_0e02b2c3d479",  // bad separators
	} {
		var u uuid.UUID
		err := u.UnmarshalText([]byte(s))
		fmt.Printf("%q -> err=%v\n", s, err != nil)
	}
}
