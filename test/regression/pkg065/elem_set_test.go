package pkg065

import (
	"reflect"
	"testing"
)

// TestElemSetThroughIndirectPointer is a regression test for PKG-065
// (graalgo test errors: TestAs fails with
// "graalgo-run: fatal internal error in goroutine:
//
//	java.lang.IllegalStateException: reflect: cannot store through
//	GoPointer{GoStruct{io/fs.PathError}}" at the clear-Set line
//	rtarget.Elem().Set(reflect.Zero(...)), wrap_test.go:204).
//
// Root cause: reflectElem's POINTER branch, given a cell pointer (&x where
// x is a GoCell-backed variable) whose current content is non-nil,
// interned the *content value* instead of the *cell storage*. The
// resulting Elem Value denoted the immutable value rather than the
// variable, so Set through it hit storeThroughRef's "cannot store through"
// throw. (The nil-content case was already handled by PKG-022, which is
// why the bug only fired once the variable held a non-nil pointer — in
// TestAs, at the case-3 clear, after case 2's errors.As had set errP to
// the non-nil errF.)
//
// The fix: a cell pointer's Elem always addresses the cell (the storage
// identity), whether the current content is nil or not — the same
// "storage identity, not dereferenced value" principle the unsafe
// interning path (P3-M10) already follows.
//
// Failability: pre-fix, the second clear-Set throws the exact
// IllegalStateException above (surfaced by the test binary as a fatal
// internal error). Post-fix, both clears succeed, the reflect-driven Set
// of a non-nil value writes through to the variable, and the read path
// observes it.
type point struct{ X, Y int }

func TestElemSetThroughIndirectPointer(t *testing.T) {
	var p *point
	// Route &p through an any, as wrap_test.go's `target any` table does:
	// this forces p onto the heap, so &p is a cell pointer (&x for a
	// GoCell-backed variable) — the shape the bug needs.
	var targets []any
	targets = append(targets, &p)

	clear := func(tc any) {
		rtarget := reflect.ValueOf(tc)
		rtarget.Elem().Set(reflect.Zero(reflect.TypeOf(tc).Elem()))
	}

	clear(targets[0]) // p is nil here: the PKG-022 (nil-content) path
	if p != nil {
		t.Fatalf("first clear: p = %v, want nil", p)
	}

	p = &point{X: 1, Y: 2} // non-nil, like errP after errors.As matched

	// PKG-065: pre-fix this throws
	// "reflect: cannot store through GoPointer{GoStruct{...}}".
	clear(targets[0])
	if p != nil {
		t.Fatalf("second clear: p = %v, want nil", p)
	}

	// The write goes through in the other direction too, and the read
	// path observes it through the same Elem handle.
	rtarget := reflect.ValueOf(targets[0])
	rtarget.Elem().Set(reflect.ValueOf(&point{X: 3}))
	if p == nil || p.X != 3 || p.Y != 0 {
		t.Fatalf("set non-nil: p = %+v, want &{X:3 Y:0}", p)
	}
	if got := rtarget.Elem().Interface(); got != any(p) {
		t.Fatalf("read back: got %#v, want %#v", got, p)
	}
}
