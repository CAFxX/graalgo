package main

// Review 2026-10-30 P1-15: hash/maphash WriteComparable panicked at
// runtime for any comparable type outside a hardcoded type switch —
// in particular named basic types (whose dynamic type never matches a
// case) and structs, the common map-key case — because gc hashes them
// via the runtime typehash. The fix routes those values through a
// structural reflect walk (hashReflectValue), so WriteComparable never
// panics for a comparable value. These tests pin that:
//
//   - the synthetic source no longer contains the unsupported-type
//     panic and does contain the reflect fallback;
//   - a program calling WriteComparable with a struct, a named int
//     type, an array, an interface-held struct, and a pointer exports
//     without error (proving the reflect calls inside the generic
//     synthetic function are emittable).
//
// Runtime hash values are NOT pinned here (they differ from gc by
// design, per the synthetic package's documented VALUE DIVERGENCE);
// a JVM differential run must still confirm equal-values-hash-equally
// for the new shapes.

import (
	"strings"
	"testing"
)

func TestMaphashWriteComparableNoPanicTrap(t *testing.T) {
	if strings.Contains(syntheticMaphashSource, "unsupported type (needs runtime typehash)") {
		t.Errorf("synthetic maphash source still contains the WriteComparable unsupported-type panic")
	}
	if !strings.Contains(syntheticMaphashSource, "hashReflectValue(h, reflect.ValueOf(x))") {
		t.Errorf("synthetic maphash source missing the reflect fallback call in WriteComparable")
	}
	if !strings.Contains(syntheticMaphashSource, "func hashReflectValue(h *Hash, v reflect.Value)") {
		t.Errorf("synthetic maphash source missing hashReflectValue")
	}
}

func TestMaphashWriteComparableExports(t *testing.T) {
	src := `package main

import "hash/maphash"

type MyInt int

type Key struct {
	a int
	b string
}

func main() {
	var h maphash.Hash
	h.SetSeed(maphash.MakeSeed())
	maphash.WriteComparable(&h, Key{a: 1, b: "x"})
	maphash.WriteComparable(&h, MyInt(7))
	maphash.WriteComparable(&h, [2]string{"a", ""})
	maphash.WriteComparable(&h, any(Key{a: 2, b: "y"}))
	n := 3
	maphash.WriteComparable(&h, &n)
	_ = h.Sum64()
}
`
	if _, err := ExportFile(writeSyncProg(t, src)); err != nil {
		t.Fatalf("WriteComparable program should export, got: %v", err)
	}
}
