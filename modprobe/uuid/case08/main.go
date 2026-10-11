package main

// Must panics on error: exercises the panic path via a failing UnmarshalText.
import (
	"fmt"

	"github.com/google/uuid"
)

func main() {
	var u uuid.UUID
	err := u.UnmarshalText([]byte("bogus"))
	fmt.Println("err:", err != nil)
	_ = uuid.Must(u, err)
}
