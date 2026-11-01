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

// Positive: a uintptr stored in a single-assignment local keeps its
// provenance (P1-8 lane): u := uintptr(p) with p an unsafe.Pointer
// variable, then unsafe.Pointer(u + off) lowers to unsafederive.
func TestUnsafeDeriveStoredUintptrLocal(t *testing.T) {
	src := `package main

import "unsafe"

func main() {
	arr := [4]int64{10, 20, 30, 40}
	p := unsafe.Pointer(&arr[0])
	u := uintptr(p)
	q := unsafe.Pointer(u + 16)
	_ = q
}
`
	out := exportUnsafeProg(t, src)
	if !strings.Contains(out, `"unsafederive"`) {
		t.Errorf("stored-uintptr local did not lower to unsafederive")
	}
	if strings.Contains(out, `"unsafefromuint"`) {
		t.Errorf("stored-uintptr local kept the unsafefromuint trap node")
	}
}

// Positive: the stored-uintptr round trip (zero offset) also derives.
func TestUnsafeDeriveStoredUintptrRoundTrip(t *testing.T) {
	src := `package main

import "unsafe"

func main() {
	x := new(int)
	p := unsafe.Pointer(x)
	u := uintptr(p)
	q := unsafe.Pointer(u)
	_ = q
}
`
	out := exportUnsafeProg(t, src)
	if !strings.Contains(out, `"unsafederive"`) {
		t.Errorf("stored-uintptr round trip did not lower to unsafederive")
	}
}

// Positive: a var-spec definition (var u uintptr = uintptr(p)) counts
// as a stored-uintptr definition exactly like := does.
func TestUnsafeDeriveStoredUintptrVarSpec(t *testing.T) {
	src := `package main

import "unsafe"

func main() {
	arr := [4]int64{}
	p := unsafe.Pointer(&arr[0])
	var u uintptr = uintptr(p)
	q := unsafe.Pointer(u + 8)
	_ = q
}
`
	out := exportUnsafeProg(t, src)
	if !strings.Contains(out, `"unsafederive"`) {
		t.Errorf("var-spec stored uintptr did not lower to unsafederive")
	}
}

// Negative: reassigning the stored variable voids its provenance.
func TestUnsafeDeriveStoredUintptrReassignedTraps(t *testing.T) {
	src := `package main

import "unsafe"

func main() {
	arr := [4]int64{}
	p := unsafe.Pointer(&arr[0])
	u := uintptr(p)
	u = 0
	q := unsafe.Pointer(u + 8)
	_ = q
}
`
	out := exportUnsafeProg(t, src)
	if strings.Contains(out, `"unsafederive"`) {
		t.Errorf("reassigned stored uintptr must not lower to unsafederive")
	}
	if !strings.Contains(out, `"unsafefromuint"`) {
		t.Errorf("reassigned stored uintptr should keep the unsafefromuint trap node")
	}
}

// Negative: taking the stored variable's address voids its provenance
// (an alias could rewrite it after the definition).
func TestUnsafeDeriveStoredUintptrAddrTakenTraps(t *testing.T) {
	src := `package main

import "unsafe"

func main() {
	arr := [4]int64{}
	p := unsafe.Pointer(&arr[0])
	u := uintptr(p)
	_ = &u
	q := unsafe.Pointer(u + 8)
	_ = q
}
`
	out := exportUnsafeProg(t, src)
	if strings.Contains(out, `"unsafederive"`) {
		t.Errorf("address-taken stored uintptr must not lower to unsafederive")
	}
}

// Negative: a closure mentioning the stored variable voids its
// provenance (the closure could rewrite it after the definition).
func TestUnsafeDeriveStoredUintptrClosureTraps(t *testing.T) {
	src := `package main

import "unsafe"

func main() {
	arr := [4]int64{}
	p := unsafe.Pointer(&arr[0])
	u := uintptr(p)
	f := func() uintptr { return u }
	_ = f
	q := unsafe.Pointer(u + 8)
	_ = q
}
`
	out := exportUnsafeProg(t, src)
	if strings.Contains(out, `"unsafederive"`) {
		t.Errorf("closure-captured stored uintptr must not lower to unsafederive")
	}
}

// Negative: a definition nested inside an if block is not a top-level
// function-body definition, so its provenance is not tracked.
func TestUnsafeDeriveStoredUintptrNestedDefTraps(t *testing.T) {
	src := `package main

import "unsafe"

func main() {
	arr := [4]int64{}
	p := unsafe.Pointer(&arr[0])
	if true {
		u := uintptr(p)
		q := unsafe.Pointer(u + 8)
		_ = q
	}
}
`
	out := exportUnsafeProg(t, src)
	if strings.Contains(out, `"unsafederive"`) {
		t.Errorf("nested-block stored uintptr must not lower to unsafederive")
	}
}

// Negative: reassigning the base pointer after the definition voids
// the stored value's provenance (re-evaluating the base at the
// conversion site would read the new pointer).
func TestUnsafeDeriveStoredUintptrBaseReassignedTraps(t *testing.T) {
	src := `package main

import "unsafe"

func main() {
	arr := [4]int64{}
	other := [4]int64{}
	p := unsafe.Pointer(&arr[0])
	u := uintptr(p)
	p = unsafe.Pointer(&other[0])
	q := unsafe.Pointer(u + 8)
	_ = q
}
`
	out := exportUnsafeProg(t, src)
	if strings.Contains(out, `"unsafederive"`) {
		t.Errorf("stored uintptr with reassigned base must not lower to unsafederive")
	}
}

// Negative: taking the base pointer's address voids provenance (an
// alias could rewrite the base after the definition).
func TestUnsafeDeriveStoredUintptrBaseAddrTakenTraps(t *testing.T) {
	src := `package main

import "unsafe"

func main() {
	arr := [4]int64{}
	p := unsafe.Pointer(&arr[0])
	u := uintptr(p)
	_ = &p
	q := unsafe.Pointer(u + 8)
	_ = q
}
`
	out := exportUnsafeProg(t, src)
	if strings.Contains(out, `"unsafederive"`) {
		t.Errorf("stored uintptr with address-taken base must not lower to unsafederive")
	}
}

// Negative (V1 scope): a multi-assignment definition is not tracked.
func TestUnsafeDeriveStoredUintptrMultiAssignTraps(t *testing.T) {
	src := `package main

import "unsafe"

func main() {
	arr := [4]int64{}
	p := unsafe.Pointer(&arr[0])
	u, v := uintptr(p), 0
	_ = v
	q := unsafe.Pointer(u + 8)
	_ = q
}
`
	out := exportUnsafeProg(t, src)
	if strings.Contains(out, `"unsafederive"`) {
		t.Errorf("multi-assignment stored uintptr must not lower to unsafederive")
	}
}

// Negative (V1 scope): a non-identifier base expression is not tracked
// (re-evaluating it at the conversion site is not proven stable).
func TestUnsafeDeriveStoredUintptrNonIdentBaseTraps(t *testing.T) {
	src := `package main

import "unsafe"

func main() {
	arr := [4]int64{}
	u := uintptr(unsafe.Pointer(&arr[0]))
	q := unsafe.Pointer(u + 8)
	_ = q
}
`
	out := exportUnsafeProg(t, src)
	if strings.Contains(out, `"unsafederive"`) {
		t.Errorf("non-identifier-base stored uintptr must not lower to unsafederive")
	}
}

// Positive: a parameter base counts (parameters are function-local),
// and the proof installs for non-main functions too.
func TestUnsafeDeriveStoredUintptrParamBase(t *testing.T) {
	src := `package main

import "unsafe"

func idx(p unsafe.Pointer, i uintptr) unsafe.Pointer {
	u := uintptr(p)
	return unsafe.Pointer(u + i*8)
}

func main() {
	arr := [4]int64{}
	q := idx(unsafe.Pointer(&arr[0]), 2)
	_ = q
}
`
	out := exportUnsafeProg(t, src)
	if !strings.Contains(out, `"unsafederive"`) {
		t.Errorf("parameter-base stored uintptr did not lower to unsafederive")
	}
}

// Negative: a package-level base variable voids provenance — code
// outside this function body could reassign it after the definition.
func TestUnsafeDeriveStoredUintptrPackageBaseTraps(t *testing.T) {
	src := `package main

import "unsafe"

var p unsafe.Pointer

func main() {
	arr := [4]int64{}
	p = unsafe.Pointer(&arr[0])
	u := uintptr(p)
	q := unsafe.Pointer(u + 8)
	_ = q
}
`
	out := exportUnsafeProg(t, src)
	if strings.Contains(out, `"unsafederive"`) {
		t.Errorf("package-level-base stored uintptr must not lower to unsafederive")
	}
}

// Positive: a chain copy (v := u, where u is proven) inherits u's
// provenance, so unsafe.Pointer(v + off) derives from u's base.
func TestUnsafeDeriveStoredUintptrChainCopy(t *testing.T) {
	src := `package main

import "unsafe"

func main() {
	arr := [4]int64{}
	p := unsafe.Pointer(&arr[0])
	u := uintptr(p)
	v := u
	q := unsafe.Pointer(v + 8)
	_ = q
}
`
	out := exportUnsafeProg(t, src)
	if !strings.Contains(out, `"unsafederive"`) {
		t.Errorf("chain-copied stored uintptr did not lower to unsafederive")
	}
}

// Negative: a proven stored variable used as an OFFSET term is a
// known pointer-derived value (a handle number, not a byte count), so
// the whole conversion keeps the trap instead of adding it to a base.
func TestUnsafeDeriveStoredUintptrAsOffsetTraps(t *testing.T) {
	src := `package main

import "unsafe"

func main() {
	a := [4]int64{}
	b := [4]int64{}
	p := unsafe.Pointer(&a[0])
	pb := unsafe.Pointer(&b[0])
	u := uintptr(pb)
	q := unsafe.Pointer(uintptr(p) + u)
	_ = q
}
`
	out := exportUnsafeProg(t, src)
	if strings.Contains(out, `"unsafederive"`) {
		t.Errorf("stored uintptr as offset must not lower to unsafederive")
	}
	if !strings.Contains(out, `"unsafefromuint"`) {
		t.Errorf("stored uintptr as offset should keep the unsafefromuint trap node")
	}
}

// Negative (pin of pre-existing r39 behavior): a reflect.Value.Pointer()
// call used directly as an offset term is itself a base-shaped term,
// so the two-base rule already rejects the conversion — this test pins
// that, it is not evidence for the P1-8 hardening. The genuinely new
// hardening is covered by TestUnsafeDerivePointerCallNestedAsOffsetTraps.
//
// Assertion is a count delta, not a global absence check: importing
// reflect pulls real reflect bodies into the export, and those bodies
// legitimately contain unsafederive nodes. The control program differs
// from the test program only in the offset term (8 vs v.Pointer()),
// so a lowering test conversion would add exactly one unsafederive
// node; a trapped one adds none.
func TestUnsafeDerivePointerCallAsOffsetTraps(t *testing.T) {
	control := `package main

import (
	"reflect"
	"unsafe"
)

func main() {
	a := [4]int64{}
	p := unsafe.Pointer(&a[0])
	v := reflect.ValueOf(&a[0])
	_ = v
	q := unsafe.Pointer(uintptr(p) + 8)
	_ = q
}
`
	src := `package main

import (
	"reflect"
	"unsafe"
)

func main() {
	a := [4]int64{}
	p := unsafe.Pointer(&a[0])
	v := reflect.ValueOf(&a[0])
	q := unsafe.Pointer(uintptr(p) + v.Pointer())
	_ = q
}
`
	controlOut := exportUnsafeProg(t, control)
	out := exportUnsafeProg(t, src)
	if strings.Count(out, `"unsafederive"`) != strings.Count(controlOut, `"unsafederive"`)-1 {
		t.Errorf("Pointer() call as offset must not lower to unsafederive (control %d, test %d)",
			strings.Count(controlOut, `"unsafederive"`), strings.Count(out, `"unsafederive"`))
	}
}

// Negative (P1-8 hardening): a Pointer() call NESTED inside a larger
// offset term (here scaled by 8) is not a top-level term, so neither
// the two-base rule nor the old hidden-provenance check catches it —
// on the r39 prover this lowered, adding a handle number (not a byte
// count) to the base. The P1-8 exprHasPointerDerived check rejects it.
// Count-delta assertion as in TestUnsafeDerivePointerCallAsOffsetTraps:
// the control differs only in the offset term (64 vs v.Pointer()*8).
func TestUnsafeDerivePointerCallNestedAsOffsetTraps(t *testing.T) {
	control := `package main

import (
	"reflect"
	"unsafe"
)

func main() {
	a := [4]int64{}
	p := unsafe.Pointer(&a[0])
	v := reflect.ValueOf(&a[0])
	_ = v
	q := unsafe.Pointer(uintptr(p) + 64)
	_ = q
}
`
	src := `package main

import (
	"reflect"
	"unsafe"
)

func main() {
	a := [4]int64{}
	p := unsafe.Pointer(&a[0])
	v := reflect.ValueOf(&a[0])
	q := unsafe.Pointer(uintptr(p) + v.Pointer()*8)
	_ = q
}
`
	controlOut := exportUnsafeProg(t, control)
	out := exportUnsafeProg(t, src)
	if strings.Count(out, `"unsafederive"`) != strings.Count(controlOut, `"unsafederive"`)-1 {
		t.Errorf("nested Pointer() call as offset must not lower to unsafederive (control %d, test %d)",
			strings.Count(controlOut, `"unsafederive"`), strings.Count(out, `"unsafederive"`))
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
