package main

// Disk-imported named type with String(): fmt + direct call + interface boxing.
import (
	"fmt"

	"github.com/google/uuid"
)

type Stringer interface{ String() string }

func main() {
	var v uuid.Version = 1
	fmt.Println(v)          // want VERSION_1
	fmt.Println(v.String()) // want VERSION_1
	var s Stringer = v
	fmt.Println(s) // want VERSION_1
	var vv uuid.Variant = uuid.RFC4122
	fmt.Println(vv) // want RFC4122
}
