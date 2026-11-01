// Regression tests for review finding R5: a D5 unsafe trap must surface
// as a Go panic — recoverable, carrying the documented message — never
// as a scheduler "fatal internal error" crash.
//
// INTENTIONAL DIVERGENCE: gc converts uintptr -> unsafe.Pointer freely
// and computes unsafe.Add(nil, n) as an address; graalgo's handle model
// cannot materialize a handle from an integer (D5), so the unprovable
// conversion and the nil-base derivation panic instead. These tests run
// under graalgo only (make test-regression); they are not gc behavior
// and must not be run against gc as a differential comparison.
//
// Pre-fix, TestUintptrConversionTrapIsRecoverable and
// TestNilBaseAddTrapIsRecoverable failed: the trap escaped as
// UnsafeEscapeException, the scheduler recorded "fatal internal error"
// and exited 1, and no deferred recover ever ran.
//
// NOTE (pre-existing limitation, not this fix): recoverTrap deliberately
// has a SINGLE named return. A deferred closure assigning to the named
// returns of a two-named-return function loses the assignment under
// graalgo — verified with plain panic("boom") as well as with the trap,
// so it is not trap-specific: the deferred closure runs and recover()
// returns the value, but the caller's (msg, ok) still read ("", false).
// A single-named-return helper recovers the message correctly (also
// verified by direct deferred recover in a main program). The
// multi-named-return deferred-assignment gap belongs to a separate lane;
// this test avoids it rather than asserting through it.
package unsafetrap

import (
	"fmt"
	"strings"
	"testing"
	"unsafe"
)

// recoverTrap runs f and returns the recovered panic rendered as a
// string, or "" when f returns normally. Rendering via %v keeps the test
// independent of how the panic value is boxed for recover().
func recoverTrap(f func()) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			msg = fmt.Sprintf("%v", r)
		}
	}()
	f()
	return ""
}

func TestUintptrConversionTrapIsRecoverable(t *testing.T) {
	msg := recoverTrap(func() {
		var u uintptr = 0x1000
		_ = unsafe.Pointer(u)
	})
	if msg == "" {
		t.Fatalf("uintptr -> unsafe.Pointer: expected a recoverable Go panic, got none")
	}
	if !strings.Contains(msg, "uintptr -> unsafe.Pointer") || !strings.Contains(msg, "D5") {
		t.Fatalf("uintptr -> unsafe.Pointer: panic message %q lacks the documented trap text", msg)
	}
}

func TestNilBaseAddTrapIsRecoverable(t *testing.T) {
	msg := recoverTrap(func() {
		var p unsafe.Pointer
		_ = unsafe.Add(p, 8)
	})
	if msg == "" {
		t.Fatalf("unsafe.Add(nil, 8): expected a recoverable Go panic, got none")
	}
	if !strings.Contains(msg, "nil") {
		t.Fatalf("unsafe.Add(nil, 8): panic message %q does not name the nil base", msg)
	}
}

func TestProvenanceDerivationStillWorks(t *testing.T) {
	arr := []int{10, 20, 30, 40}
	base := unsafe.Pointer(&arr[0])
	// Exact round trip: provenance-proven, must not trap.
	if got := unsafe.Pointer(uintptr(base)); got != base {
		t.Fatalf("round trip: unsafe.Pointer(uintptr(base)) != base")
	}
	// Provenance-proven derivation: base + one element, via the
	// unsafederive node, must read the second element.
	elemSize := unsafe.Sizeof(arr[0])
	derived := unsafe.Pointer(uintptr(base) + uintptr(1)*elemSize)
	if got := *(*int)(derived); got != 20 {
		t.Fatalf("derived pointer: *(*int)(derived) = %d, want 20", got)
	}
	// The same derivation through unsafe.Add must agree.
	added := unsafe.Add(base, int(elemSize))
	if got := *(*int)(added); got != 20 {
		t.Fatalf("unsafe.Add: *(*int)(added) = %d, want 20", got)
	}
	if added != derived {
		t.Fatalf("unsafe.Add and unsafederive disagree: %v != %v", added, derived)
	}
	// Writes through the derived pointer must reach the slice.
	*(*int)(derived) = 21
	if arr[1] != 21 {
		t.Fatalf("write through derived pointer: arr[1] = %d, want 21", arr[1])
	}
}
