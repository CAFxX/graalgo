package main

// Pointer to disk-imported named type: value-receiver String promotes.
import (
	"fmt"

	"github.com/google/uuid"
)

func main() {
	v := uuid.Version(4)
	pv := &v
	fmt.Println(pv) // want VERSION_4 (via promoted String)
}
