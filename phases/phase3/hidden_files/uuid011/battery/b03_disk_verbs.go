package main

// Other fmt verbs on a disk-imported Stringer.
import (
	"fmt"

	"github.com/google/uuid"
)

func main() {
	var v uuid.Version = 1
	fmt.Printf("%v\n", v)   // want VERSION_1
	fmt.Printf("%s\n", v)   // want VERSION_1
	fmt.Printf("%T\n", v)   // want uuid.Version
	fmt.Printf("%#v\n", v)  // want VERSION_1 (GoStringer absent -> String)
	fmt.Printf("%q\n", v)   // want "VERSION_1"
}
