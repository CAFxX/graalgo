package main

import (
	"fmt"
	"unsafe"
)

// 08: Offsetof through 20 nested structs. go/types folds the offset to a
// constant (no depth cap in the type-checker), so this is differential.
type L20 struct{ x L19 }
type L19 struct{ x L18 }
type L18 struct{ x L17 }
type L17 struct{ x L16 }
type L16 struct{ x L15 }
type L15 struct{ x L14 }
type L14 struct{ x L13 }
type L13 struct{ x L12 }
type L12 struct{ x L11 }
type L11 struct{ x L10 }
type L10 struct{ x L9 }
type L9 struct{ x L8 }
type L8 struct{ x L7 }
type L7 struct{ x L6 }
type L6 struct{ x L5 }
type L5 struct{ x L4 }
type L4 struct{ x L3 }
type L3 struct{ x L2 }
type L2 struct{ x L1 }
type L1 struct{ v int64 }

func main() {
	fmt.Println(unsafe.Offsetof(L20{}.x.x.x.x.x.x.x.x.x.x.x.x.x.x.x.x.x.x.x.v))
}
