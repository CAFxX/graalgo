package main

// fix-redmaster-r39: exporter tests for the provenance-preserving
// derivation lowering (DESIGN.md in hidden_files/red-master-r39).
//
// A uintptr -> unsafe.Pointer conversion whose operand is PROVEN at
// export time to be one pointer-derived additive term plus pure
// integer terms lowers to the "unsafederive" wire node; every other
// shape keeps the D5 "unsafefromuint" node (which panics at runtime
// with the documented message, review-20261031 R-06). These tests fail
// on the unfixed frontend: the positive cases emit unsafefromuint there
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
// static provenance proof and must keep the D5 node (which panics at
// runtime with the documented message, review-20261031 R-06 — proving
// a stored value needs a flow-sensitive dominance proof, see
// unsafe_derive.go; a syntactic scan would be unsound).
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

// Positive: a pointer-derived term through value-preserving integer
// conversions (review-20261031 R-06) still lowers to unsafederive:
// each wrapper is same-or-wider than uintptr, so the value — and
// hence the base pointer — survives exactly.
func TestUnsafeDeriveNestedIntegerConversion(t *testing.T) {
	src := `package main

import "unsafe"

func main() {
	arr := [4]int64{}
	p := unsafe.Pointer(&arr[0])
	q := unsafe.Pointer(uintptr(uint64(uintptr(p))) + 16)
	_ = q
}
`
	out := exportUnsafeProg(t, src)
	if !strings.Contains(out, `"unsafederive"`) {
		t.Errorf("nested integer conversions did not lower to unsafederive")
	}
}

// countWireKind counts occurrences of a wire node kind in exported
// JSON. Programs importing reflect cannot use whole-output presence
// checks: reflect's own exported code already contains unsafederive
// and unsafefromuint nodes (31/2 in the baseline below), so the
// reflect tests below assert count DELTAS against an identical
// baseline instead — the only difference between the two programs is
// the conversion under test.
func countWireKind(out, kind string) int {
	return strings.Count(out, `"`+kind+`"`)
}

// Positive: reflect.Value.Pointer() wrapped in a uintptr conversion
// is the same provenance as the bare call.
func TestUnsafeDerivePointerCallWrapped(t *testing.T) {
	base := `package main

import (
	"reflect"
	"unsafe"
)

func main() {
	arr := [4]int64{}
	v := reflect.ValueOf(&arr[0])
	_ = v.Pointer()
	_ = unsafe.Sizeof(arr)
}
`
	src := `package main

import (
	"reflect"
	"unsafe"
)

func main() {
	arr := [4]int64{}
	v := reflect.ValueOf(&arr[0])
	q := unsafe.Pointer(uintptr(v.Pointer()) + 16)
	_ = q
}
`
	bout := exportUnsafeProg(t, base)
	out := exportUnsafeProg(t, src)
	if d := countWireKind(out, "unsafederive") - countWireKind(bout, "unsafederive"); d != 1 {
		t.Errorf("uintptr(v.Pointer()) should add exactly one unsafederive node, delta=%d", d)
	}
	if d := countWireKind(out, "unsafefromuint") - countWireKind(bout, "unsafefromuint"); d != 0 {
		t.Errorf("uintptr(v.Pointer()) should add no unsafefromuint node, delta=%d", d)
	}
}

// Positive: a named uintptr type converts exactly like uintptr.
func TestUnsafeDeriveNamedUintptr(t *testing.T) {
	src := `package main

import "unsafe"

type addr uintptr

func main() {
	arr := [4]int64{}
	p := unsafe.Pointer(&arr[0])
	q := unsafe.Pointer(addr(uintptr(p)) + 16)
	_ = q
}
`
	out := exportUnsafeProg(t, src)
	if !strings.Contains(out, `"unsafederive"`) {
		t.Errorf("named uintptr conversion did not lower to unsafederive")
	}
}

// Negative: a narrowing wrapper truncates the address on 64-bit, so
// it must NOT lower to unsafederive — deriving from the untruncated
// base would silently return a wrong pointer. It keeps the
// unsafefromuint panic node.
func TestUnsafeDeriveNarrowingConversionTraps(t *testing.T) {
	src := `package main

import "unsafe"

func main() {
	arr := [4]int64{}
	p := unsafe.Pointer(&arr[0])
	q := unsafe.Pointer(uintptr(uint32(uintptr(p))) + 16)
	_ = q
}
`
	out := exportUnsafeProg(t, src)
	if strings.Contains(out, `"unsafederive"`) {
		t.Errorf("narrowing conversion must not lower to unsafederive")
	}
	if !strings.Contains(out, `"unsafefromuint"`) {
		t.Errorf("narrowing conversion should keep the unsafefromuint node")
	}
}

// Negative: an offset inside the wrapper (uintptr(v.Pointer()+8)) is
// not a base: treating the wrapper as a base would silently drop the
// inner offset. It keeps the unsafefromuint panic node (delta-counted:
// see countWireKind — reflect's own code already contains both kinds).
func TestUnsafeDeriveInnerOffsetTraps(t *testing.T) {
	base := `package main

import (
	"reflect"
	"unsafe"
)

func main() {
	arr := [4]int64{}
	v := reflect.ValueOf(&arr[0])
	_ = v.Pointer()
	_ = unsafe.Sizeof(arr)
}
`
	src := `package main

import (
	"reflect"
	"unsafe"
)

func main() {
	arr := [4]int64{}
	v := reflect.ValueOf(&arr[0])
	q := unsafe.Pointer(uintptr(v.Pointer() + 8))
	_ = q
}
`
	bout := exportUnsafeProg(t, base)
	out := exportUnsafeProg(t, src)
	if d := countWireKind(out, "unsafederive") - countWireKind(bout, "unsafederive"); d != 0 {
		t.Errorf("wrapped arithmetic must not add an unsafederive node, delta=%d", d)
	}
	if d := countWireKind(out, "unsafefromuint") - countWireKind(bout, "unsafefromuint"); d != 1 {
		t.Errorf("wrapped arithmetic should add exactly one unsafefromuint node, delta=%d", d)
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
