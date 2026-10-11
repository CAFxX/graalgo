package main

// Direct method call + interface satisfaction on disk types.
import (
	"fmt"

	"github.com/google/uuid"
)

type Stringer interface{ String() string }

func stringify(s Stringer) string { return s.String() }

func main() {
	v := uuid.Version(5)
	fmt.Println(v.String())  // want VERSION_5
	fmt.Println(stringify(v)) // want VERSION_5
	// interface satisfaction check
	var _ Stringer = v
	fmt.Println("ok")
}
