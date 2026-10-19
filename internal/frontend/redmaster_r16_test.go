package main

import (
	"testing"
)

// TestNoEscapeLowersToIdentity: a call to internal/abi.NoEscape must be
// lowered to the identity — no call node for NoEscape may appear in the
// exported wire document.
//
// gc documents NoEscape as a compile intrinsic ("compiles down to
// nothing"): it returns its argument and only affects escape analysis. Its
// real body does uintptr(p) followed by unsafe.Pointer(x^0), which traps
// under graalgo's D5 forged-pointer rule, so the frontend lowers calls to
// the identity (P3-M16, fix-redmaster-r16).
//
// Without the lowering, the call exports as a regular call node (as on the
// pre-fix base), which the backend would execute as the real body and trap.
func TestNoEscapeLowersToIdentity(t *testing.T) {
	src := `package main

import (
	"internal/abi"
	"unsafe"
)

func main() {
	var p unsafe.Pointer
	q := abi.NoEscape(p)
	_ = q
}
`
	doc := exportSrc(t, src)
	calls := findNodes(t, doc, "call")
	for _, c := range calls {
		fun, ok := nodeFields(t, c)["fun"].(map[string]any)
		if !ok {
			continue
		}
		funFields, ok := fun["fields"].(map[string]any)
		if !ok {
			continue
		}
		name, _ := funFields["name"].(string)
		if name == "NoEscape" {
			t.Fatalf("found call to NoEscape in wire document: must be lowered to the identity")
		}
	}
}
