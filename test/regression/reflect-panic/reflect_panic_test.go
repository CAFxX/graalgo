package reflectpanic

import (
	"reflect"
	"testing"
)

// Regression tests for the graalgo independent codebase review P1-1
// (rev1103-reflect-panic): reflect misuse must surface as a recoverable
// Go panic with gc's message and exit code 2 when uncaught — never as a
// raw host error that bypasses recover() and dies with exit 1.
//
// NOTE on scope (verified against gc 1.27.1 and graalgo at the lane base
// b458673f): guest programs use real reflect from GOROOT, not the retired
// synthetic reflect package, so these tests pin the guest-visible
// behavior. The lane's ReflectOpNode conversions cover the same error
// sites on the synthetic path (defense in depth); the live defect it
// actually fixed was the reflect.growslice linkname arity (2, should be
// 3), which made every real Value.Grow that grew die with a host error.
//
// "Missing field" has two gc behaviors, both pinned here: FieldByName on
// a missing name does NOT panic (it returns the zero Value), while
// Field(i) out of range panics with "reflect: Field index out of range".

// recoverCase runs f, requires it to panic with the exact string value
// want, and requires execution to continue afterwards (the continued flag
// is set only if the deferred recover ran and control returned).
func recoverCase(t *testing.T, name string, f func(), want string) {
	t.Helper()
	continued := false
	func() {
		defer func() {
			r := recover()
			if r == nil {
				t.Fatalf("%s: expected panic %q, got none", name, want)
			}
			s, ok := r.(string)
			if !ok {
				t.Fatalf("%s: panic value type %T, want string", name, r)
			}
			if s != want {
				t.Fatalf("%s: panic = %q, want %q", name, s, want)
			}
			continued = true
		}()
		f()
	}()
	if !continued {
		t.Fatalf("%s: execution did not continue after recover", name)
	}
}

func TestRecoverSetLenOutOfRange(t *testing.T) {
	s := []int{1, 2, 3}
	recoverCase(t, "SetLen(-1)", func() {
		reflect.ValueOf(&s).Elem().SetLen(-1)
	}, "reflect: slice length out of range in SetLen")
	recoverCase(t, "SetLen(99)", func() {
		reflect.ValueOf(&s).Elem().SetLen(99)
	}, "reflect: slice length out of range in SetLen")
	// Continued execution: the slice is untouched and still usable.
	if len(s) != 3 {
		t.Fatalf("len(s) = %d after recovered SetLen panics, want 3", len(s))
	}
}

func TestRecoverSetCapOutOfRange(t *testing.T) {
	s := []int{1, 2, 3}
	recoverCase(t, "SetCap(1) below len", func() {
		reflect.ValueOf(&s).Elem().SetCap(1)
	}, "reflect: slice capacity out of range in SetCap")
	recoverCase(t, "SetCap(99) above cap", func() {
		reflect.ValueOf(&s).Elem().SetCap(99)
	}, "reflect: slice capacity out of range in SetCap")
	if cap(s) != 3 {
		t.Fatalf("cap(s) = %d after recovered SetCap panics, want 3", cap(s))
	}
}

func TestRecoverGrowNegative(t *testing.T) {
	s := []int{1, 2, 3}
	recoverCase(t, "Grow(-3)", func() {
		reflect.ValueOf(&s).Elem().Grow(-3)
	}, "reflect.Value.Grow: negative len")
	if len(s) != 3 {
		t.Fatalf("len(s) = %d after recovered Grow panic, want 3", len(s))
	}
}

func TestMissingField(t *testing.T) {
	type T struct {
		B []byte
	}
	// FieldByName on a missing name does not panic: it returns the
	// zero Value, and execution continues.
	v := reflect.ValueOf(T{}).FieldByName("Nope")
	if v.IsValid() {
		t.Fatalf("FieldByName(missing).IsValid() = true, want false")
	}
	// Field(i) out of range panics, recoverably.
	recoverCase(t, "Field(5)", func() {
		reflect.ValueOf(T{}).Field(5)
	}, "reflect: Field index out of range")
}

func TestGrowDoesNotDieWithHostError(t *testing.T) {
	// Pins the growslice linkname arity fix: before it, any Grow that
	// actually grew died with a host FrontendException ("takes 2
	// arguments, got 3"), killing the test binary instead of growing.
	// This test asserts only that Grow runs to completion without a
	// host error; it deliberately does NOT assert the cap effect:
	// slice-header writeback through Elem pointers is still a no-op
	// under graalgo (follow-up lane; see the lane report).
	s := []int{1, 2, 3}
	continued := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Grow(5): unexpected panic %v", r)
			}
			continued = true
		}()
		reflect.ValueOf(&s).Elem().Grow(5)
	}()
	if !continued {
		t.Fatalf("Grow(5): execution did not continue")
	}
}
