// Regression tests for B-N1 (REVIEW-2026-11-06-15): &s[i]/&arr[i] had no
// bounds check in NodeFactory.buildAddrOf — and the (int) cast truncated
// the index first, so a huge out-of-range index wrapped to an in-range
// int and silently produced a wrong pointer instead of gc's
// "index out of range" panic.
//
// Every panic test must FAIL pre-fix ("got no panic") and PASS post-fix;
// the two write-through tests are positive controls proving the fix did
// not break in-bounds element pointers (including the slice-offset path).
package addrofbounds

import (
	"fmt"
	"strings"
	"testing"
)

// expectIndexPanic runs f and requires it to panic with a message
// containing "index out of range" (gc's runtime error text).
func expectIndexPanic(t *testing.T, name string, f func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("%s: got no panic, want index out of range", name)
		}
		if !strings.Contains(fmt.Sprint(r), "index out of range") {
			t.Fatalf("%s: panic %q, want it to contain \"index out of range\"", name, r)
		}
	}()
	f()
}

func TestSliceIndexAtLen(t *testing.T) {
	s := []int{1, 2, 3}
	i := len(s) // non-constant: a constant 3 is rejected at compile time
	expectIndexPanic(t, "&s[len]", func() { _ = &s[i] })
}

func TestSliceIndexAboveLen(t *testing.T) {
	s := []int{1, 2, 3}
	i := len(s) + 1 // non-constant: a constant 4 is rejected at compile time
	expectIndexPanic(t, "&s[len+1]", func() { _ = &s[i] })
}

func TestSliceNegativeIndex(t *testing.T) {
	s := []int{1, 2, 3}
	i := -1 // non-constant: a constant -1 is rejected at compile time
	expectIndexPanic(t, "&s[-1]", func() { _ = &s[i] })
}

func TestNilSliceIndex(t *testing.T) {
	var s []int
	expectIndexPanic(t, "&nil[0]", func() { _ = &s[0] })
}

func TestArrayIndexAtLen(t *testing.T) {
	a := [3]int{1, 2, 3}
	i := len(a) // non-constant: a constant 3 is rejected at compile time
	expectIndexPanic(t, "&a[len]", func() { _ = &a[i] })
}

func TestArrayNegativeIndex(t *testing.T) {
	a := [3]int{1, 2, 3}
	i := -1 // non-constant: a constant -1 is rejected at compile time
	expectIndexPanic(t, "&a[-1]", func() { _ = &a[i] })
}

func TestSubSliceIndexAtLen(t *testing.T) {
	s := []int{1, 2, 3, 4}
	s2 := s[1:]
	// len(s2) == 3; index 3 is out of range for the sub-slice even
	// though 3 + offset 1 == 4 is inside the backing array — the
	// check must run against the slice length, before the offset.
	i := len(s2) // non-constant: a constant 3 is rejected at compile time
	expectIndexPanic(t, "&s2[len]", func() { _ = &s2[i] })
}

func TestPointerToArrayIndexAtLen(t *testing.T) {
	a := [3]int{1, 2, 3}
	p := &a
	i := len(a) // non-constant: a constant 3 is rejected at compile time
	expectIndexPanic(t, "&p[len]", func() { _ = &p[i] })
}

func TestHugeIndexWrapsIntCast(t *testing.T) {
	s := []int{1, 2, 3}
	// 1<<40 truncates to 0 under the old (int) cast, so pre-fix this
	// produced a pointer to s[0] instead of panicking.
	i := int64(1) << 40
	expectIndexPanic(t, "&s[1<<40]", func() { _ = &s[i] })
}

func TestHugeUnsignedIndex(t *testing.T) {
	s := []int{1, 2, 3}
	// 1<<63 as uint64 is Long.MIN_VALUE in two's complement; the old
	// (int) cast truncated it to 0, so pre-fix this produced a
	// pointer to s[0] instead of panicking.
	var u uint64 = 1 << 63
	expectIndexPanic(t, "&s[1<<63 uint64]", func() { _ = &s[u] })
}

func TestAddrOfWritesThrough(t *testing.T) {
	s := []int{1, 2, 3}
	p := &s[1]
	*p = 99
	if s[1] != 99 {
		t.Fatalf("write through &s[1]: s[1] = %d, want 99", s[1])
	}
}

func TestSubSliceAddrOfWritesThroughOffset(t *testing.T) {
	s := []int{1, 2, 3, 4}
	s2 := s[1:]
	p := &s2[1]
	*p = 7
	if s[2] != 7 {
		t.Fatalf("write through &s2[1]: s[2] = %d, want 7", s[2])
	}
	if s2[1] != 7 {
		t.Fatalf("write through &s2[1]: s2[1] = %d, want 7", s2[1])
	}
}
