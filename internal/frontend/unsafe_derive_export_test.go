package main

// fix-redmaster-r39: exporter tests for the provenance-preserving
// derivation lowering (DESIGN.md in hidden_files/red-master-r39).
//
// A uintptr -> unsafe.Pointer conversion whose operand is PROVEN at
// export time to be one pointer-derived additive term plus pure
// integer terms lowers to the "unsafederive" wire node; every other
// shape keeps the D5 "unsafefromuint" trap node. These tests fail on
// the unfixed frontend: the positive cases emit unsafefromuint there
// (no unsafederive kind exists), and the elemSize field is absent.

import (
	"strings"
	"testing"
)

func exportUnsafeProg(t *testing.T, src string) string {
	t.Helper()
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	return out
}

// Positive: the sync.Pool indexLocal shape — base pointer converted
// to uintptr plus an integer byte offset.
func TestUnsafeDeriveIndexLocalShape(t *testing.T) {
	src := `package main

import "unsafe"

type local struct{ a, b uintptr }

func idx(l unsafe.Pointer, i uintptr) unsafe.Pointer {
	return unsafe.Pointer(uintptr(l) + i*unsafe.Sizeof(local{}))
}

func main() {
	arr := [4]local{}
	p := idx(unsafe.Pointer(&arr[0]), 2)
	_ = p
}
`
	out := exportUnsafeProg(t, src)
	if !strings.Contains(out, `"unsafederive"`) {
		t.Errorf("indexLocal shape did not lower to unsafederive")
	}
}

// Positive: the zero-offset round trip unsafe.Pointer(uintptr(p)).
func TestUnsafeDeriveZeroOffsetRoundTrip(t *testing.T) {
	src := `package main

import "unsafe"

func main() {
	x := new(int)
	q := unsafe.Pointer(uintptr(unsafe.Pointer(x)))
	_ = q
}
`
	out := exportUnsafeProg(t, src)
	if !strings.Contains(out, `"unsafederive"`) {
		t.Errorf("zero-offset round trip did not lower to unsafederive")
	}
}

// Positive: subtraction of an integer term is still a derivation.
func TestUnsafeDeriveSubtraction(t *testing.T) {
	src := `package main

import "unsafe"

func main() {
	arr := [4]int64{}
	p := unsafe.Pointer(&arr[3])
	q := unsafe.Pointer(uintptr(p) - 16)
	_ = q
}
`
	out := exportUnsafeProg(t, src)
	if !strings.Contains(out, `"unsafederive"`) {
		t.Errorf("pointer-minus-integer did not lower to unsafederive")
	}
}

// Negative: a uintptr that round-trips through a variable has no
// static provenance proof and must keep the D5 trap node.
func TestUnsafeDeriveStoredUintptrTraps(t *testing.T) {
	src := `package main

import "unsafe"

func main() {
	x := new(int)
	var stored uintptr = uintptr(unsafe.Pointer(x))
	q := unsafe.Pointer(stored)
	_ = q
}
`
	out := exportUnsafeProg(t, src)
	if strings.Contains(out, `"unsafederive"`) {
		t.Errorf("stored uintptr must not lower to unsafederive")
	}
	if !strings.Contains(out, `"unsafefromuint"`) {
		t.Errorf("stored uintptr should keep the unsafefromuint trap node")
	}
}

// Negative: arithmetic over TWO pointers is not a derivation.
func TestUnsafeDeriveTwoPointersTraps(t *testing.T) {
	src := `package main

import "unsafe"

func main() {
	a := new(int)
	b := new(int)
	q := unsafe.Pointer(uintptr(unsafe.Pointer(a)) + uintptr(unsafe.Pointer(b)))
	_ = q
}
`
	out := exportUnsafeProg(t, src)
	if strings.Contains(out, `"unsafederive"`) {
		t.Errorf("two-pointer arithmetic must not lower to unsafederive")
	}
	if !strings.Contains(out, `"unsafefromuint"`) {
		t.Errorf("two-pointer arithmetic should keep the unsafefromuint trap node")
	}
}

// Negative: a pointer term scaled by multiplication is not a plain
// additive derivation.
func TestUnsafeDeriveScaledPointerTraps(t *testing.T) {
	src := `package main

import "unsafe"

func main() {
	a := new(int)
	q := unsafe.Pointer(uintptr(unsafe.Pointer(a)) * 2)
	_ = q
}
`
	out := exportUnsafeProg(t, src)
	if strings.Contains(out, `"unsafederive"`) {
		t.Errorf("scaled pointer term must not lower to unsafederive")
	}
}

// Positive (wire field): a direct unsafe.Pointer -> *T conversion
// carries the element size from go/types Sizes so the backend can
// validate derived pointers against element boundaries.
func TestUnsafeToPtrCarriesElemSize(t *testing.T) {
	src := `package main

import "unsafe"

func main() {
	arr := [4]int64{}
	p := (*int64)(unsafe.Pointer(&arr[0]))
	_ = p
}
`
	out := exportUnsafeProg(t, src)
	if !strings.Contains(out, `"elemSize"`) {
		t.Errorf("unsafetooptr conversion missing elemSize field")
	}
}
