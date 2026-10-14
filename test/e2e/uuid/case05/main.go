package main

// New() is random: assert only its deterministic properties.
import (
	"fmt"

	"github.com/google/uuid"
)

func main() {
	for i := 0; i < 4; i++ {
		u := uuid.New()
		fmt.Println(u.Version() == 4 && u.Variant() == uuid.RFC4122)
	}
}
