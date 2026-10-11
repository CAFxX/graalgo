package main

// error interface on a disk-imported type.
import (
	"fmt"

	"github.com/google/uuid"
)

func main() {
	var u uuid.UUID
	err := u.UnmarshalText([]byte("not-a-uuid"))
	fmt.Println(err) // want invalid UUID length message via Error()
	var e error = err
	fmt.Println(e)
	fmt.Printf("%T\n", e)
}
