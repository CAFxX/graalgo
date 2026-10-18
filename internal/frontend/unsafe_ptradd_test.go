package main

// Regression tests for the unsafeptradd wire node (fix-redmaster-exitcode-drift,
// P1-2 remediation: matcher adopted from r6's splitPtrAdd).
//
// The standard Go unsafe pointer-arithmetic idiom
//   unsafe.Pointer(uintptr(p) + k*unsafe.Sizeof(T))
// must lower to the "unsafeptradd" wire node (element-indexed backend
// arithmetic), while a uintptr->unsafe.Pointer conversion whose offset is
// NOT a whole number of T-sized elements must keep the D5 trap
// ("unsafefromuint").
//
// The matcher (splitPtrAdd) handles: ADD in both operand orders, SUB
// (uintptr(p) - k*Sizeof), MUL in both orders, a bare unsafe.Sizeof(T)
// (k=1), and the constant 0 (identity). Sizeof is resolved via go/types
// (*types.Builtin), so shadowing cannot fool it.

import (
	"encoding/json"
	"strings"
	"testing"
)

// ptraddNodeKinds exports src and returns the kinds of all wire nodes whose
// kind is "unsafeptradd" or "unsafefromuint".
func ptraddNodeKinds(t *testing.T, src string) []string {
	t.Helper()
	out := exportSrc(t, src)
	raw := map[string]any{}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatalf("unmarshal doc: %v", err)
	}
	var kinds []string
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			if k, ok := v["kind"].(string); ok && (k == "unsafeptradd" || k == "unsafefromuint") {
				kinds = append(kinds, k)
			}
			for _, c := range v {
				walk(c)
			}
		case []any:
			for _, c := range v {
				walk(c)
			}
		}
	}
	walk(raw)
	return kinds
}

const ptraddProg = `package main

import "unsafe"

func f(p unsafe.Pointer, k uintptr) unsafe.Pointer {
	return %s
}

func main() {
	arr := [5]int64{10, 20, 30, 40, 50}
	p := unsafe.Pointer(&arr[0])
	_ = f(p, 2)
}
`

// TestUnsafePtrAddPositive: the canonical idiom lowers to unsafeptradd.
func TestUnsafePtrAddPositive(t *testing.T) {
	src := strings.Replace(ptraddProg, "%s",
		`unsafe.Pointer(uintptr(p) + k*unsafe.Sizeof(int64(0)))`, 1)
	kinds := ptraddNodeKinds(t, src)
	if len(kinds) != 1 || kinds[0] != "unsafeptradd" {
		t.Fatalf("want exactly one unsafeptradd node, got %v", kinds)
	}
}

// TestUnsafePtrAddReversedAdd: k*Sizeof + uintptr(p) (ADD operands swapped).
func TestUnsafePtrAddReversedAdd(t *testing.T) {
	src := strings.Replace(ptraddProg, "%s",
		`unsafe.Pointer(k*unsafe.Sizeof(int64(0)) + uintptr(p))`, 1)
	kinds := ptraddNodeKinds(t, src)
	if len(kinds) != 1 || kinds[0] != "unsafeptradd" {
		t.Fatalf("want exactly one unsafeptradd node, got %v", kinds)
	}
}

// TestUnsafePtrAddReversedMul: uintptr(p) + Sizeof*k (MUL operands swapped).
func TestUnsafePtrAddReversedMul(t *testing.T) {
	src := strings.Replace(ptraddProg, "%s",
		`unsafe.Pointer(uintptr(p) + unsafe.Sizeof(int64(0))*k)`, 1)
	kinds := ptraddNodeKinds(t, src)
	if len(kinds) != 1 || kinds[0] != "unsafeptradd" {
		t.Fatalf("want exactly one unsafeptradd node, got %v", kinds)
	}
}

// TestUnsafePtrAddSub: uintptr(p) - k*Sizeof lowers to unsafeptradd
// (the backend negates the index).
func TestUnsafePtrAddSub(t *testing.T) {
	src := strings.Replace(ptraddProg, "%s",
		`unsafe.Pointer(uintptr(p) - k*unsafe.Sizeof(int64(0)))`, 1)
	kinds := ptraddNodeKinds(t, src)
	if len(kinds) != 1 || kinds[0] != "unsafeptradd" {
		t.Fatalf("want exactly one unsafeptradd node, got %v", kinds)
	}
}

// TestUnsafePtrAddBareSizeof: uintptr(p) + unsafe.Sizeof(T) (k=1).
func TestUnsafePtrAddBareSizeof(t *testing.T) {
	src := strings.Replace(ptraddProg, "%s",
		`unsafe.Pointer(uintptr(p) + unsafe.Sizeof(int64(0)))`, 1)
	kinds := ptraddNodeKinds(t, src)
	if len(kinds) != 1 || kinds[0] != "unsafeptradd" {
		t.Fatalf("want exactly one unsafeptradd node, got %v", kinds)
	}
}

// TestUnsafePtrAddNegativeByteArithmetic: byte arithmetic WITHOUT a Sizeof
// factor must NOT match — it keeps the D5 trap (unsafefromuint).
// This is the fail-capable negative test: if the matcher ever becomes too
// permissive, this fails.
func TestUnsafePtrAddNegativeByteArithmetic(t *testing.T) {
	src := strings.Replace(ptraddProg, "%s",
		`unsafe.Pointer(uintptr(p) + 16)`, 1)
	kinds := ptraddNodeKinds(t, src)
	if len(kinds) != 1 || kinds[0] != "unsafefromuint" {
		t.Fatalf("want exactly one unsafefromuint node (D5 trap), got %v", kinds)
	}
}

// TestUnsafePtrAddNegativeNonSizeofMul: k*8 is not k*Sizeof — must trap.
func TestUnsafePtrAddNegativeNonSizeofMul(t *testing.T) {
	src := strings.Replace(ptraddProg, "%s",
		`unsafe.Pointer(uintptr(p) + k*8)`, 1)
	kinds := ptraddNodeKinds(t, src)
	if len(kinds) != 1 || kinds[0] != "unsafefromuint" {
		t.Fatalf("want exactly one unsafefromuint node (D5 trap), got %v", kinds)
	}
}