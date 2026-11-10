// Regression test for R22-F-6: real/imag of an untyped complex constant
// consumed as float32 crashed the backend.
//
// Root cause: the exporter never folded constant builtin calls, so
// real(3+4i) reached the backend as a live ComplexNode over a folded
// constant. The factory builds that UNTYPED_COMPLEX constant as a
// GoComplex64, while the ComplexNode's kind is the call's
// contextually-typed result (float32 for a float32 consumer, float64 by
// default -- go/types records the contextual type in info.Types), so a
// float32 consumer dispatched to ComplexNode.executeFloat, which cast
// unconditionally to GoComplex32 (ClassCastException). The conversion
// form float32(real(3+4i)) crashed the same way: the convert is
// float32->float32, which maybeConvert elides as transparent, leaving
// the ComplexNode itself as the println argument.
//
// Fix: (a) the exporter folds constant builtin calls (exportCall), and
// (b) ComplexNode.executeFloat/executeDouble dispatch on the argument's
// actual representation. Either half fixes the crash; both are kept.
//
// Pre-fix, TestRealConstFloat32 and TestRealImagConvertFloat32 both fail
// with ClassCastException (GoComplex64 cannot be cast to GoComplex32).
// The TestRealImagTypedControls tests pass before and after (typed paths
// were always clean) and pin them against regressions.
package r22f6complexrealimag

import "testing"

// TestRealConstFloat32 is the first one-line reproducer: an untyped
// complex constant's real/imag part assigned to a float32 variable.
func TestRealConstFloat32(t *testing.T) {
	var f float32 = real(3 + 4i)
	if f != 3 {
		t.Fatalf("var f float32 = real(3+4i): got %v, want 3", f)
	}
	var g float32 = imag(3 + 4i)
	if g != 4 {
		t.Fatalf("var g float32 = imag(3+4i): got %v, want 4", g)
	}
}

// TestRealImagConvertFloat32 is the second one-line reproducer: the
// explicit float32 conversion of real/imag of a constant.
func TestRealImagConvertFloat32(t *testing.T) {
	if got := float32(real(3 + 4i)); got != 3 {
		t.Fatalf("float32(real(3+4i)): got %v, want 3", got)
	}
	if got := float32(imag(3 + 4i)); got != 4 {
		t.Fatalf("float32(imag(3+4i)): got %v, want 4", got)
	}
}

// TestRealImagConstFloat64 covers the same constant forms consumed as
// float64 (the matching consumer kind, which never crashed, pinned here
// so the fold cannot silently change the value).
func TestRealImagConstFloat64(t *testing.T) {
	var f float64 = real(3 + 4i)
	if f != 3 {
		t.Fatalf("var f float64 = real(3+4i): got %v, want 3", f)
	}
	var g float64 = imag(3 + 4i)
	if g != 4 {
		t.Fatalf("var g float64 = imag(3+4i): got %v, want 4", g)
	}
	if got := float64(real(3 + 4i)); got != 3 {
		t.Fatalf("float64(real(3+4i)): got %v, want 3", got)
	}
	if got := float64(imag(3 + 4i)); got != 4 {
		t.Fatalf("float64(imag(3+4i)): got %v, want 4", got)
	}
}

// TestRealImagTypedControls pins the typed paths, which were clean before
// the fix: real/imag of complex64/complex128 variables, including a
// cross-width conversion.
func TestRealImagTypedControls(t *testing.T) {
	c64 := complex64(3 + 4i)
	if got := real(c64); got != 3 {
		t.Fatalf("real(complex64(3+4i)): got %v, want 3", got)
	}
	if got := imag(c64); got != 4 {
		t.Fatalf("imag(complex64(3+4i)): got %v, want 4", got)
	}
	if got := float64(real(c64)); got != 3 {
		t.Fatalf("float64(real(complex64(3+4i))): got %v, want 3", got)
	}
	c128 := complex(3, 4)
	if got := real(c128); got != 3 {
		t.Fatalf("real(complex(3, 4)): got %v, want 3", got)
	}
	if got := imag(c128); got != 4 {
		t.Fatalf("imag(complex(3, 4)): got %v, want 4", got)
	}
	if got := float32(real(c128)); got != 3 {
		t.Fatalf("float32(real(complex(3, 4))): got %v, want 3", got)
	}
}

// TestComplexConstFold pins the complex() side of the exporter fold:
// complex(1, 2) with constant arguments folds to a basiclit, so the
// assignment must carry the exact components.
func TestComplexConstFold(t *testing.T) {
	var c64 complex64 = complex(1, 2)
	if real(c64) != 1 || imag(c64) != 2 {
		t.Fatalf("var c64 complex64 = complex(1, 2): got (%v, %v), want (1, 2)", real(c64), imag(c64))
	}
	var c128 complex128 = complex(1, 2)
	if real(c128) != 1 || imag(c128) != 2 {
		t.Fatalf("var c128 complex128 = complex(1, 2): got (%v, %v), want (1, 2)", real(c128), imag(c128))
	}
}
