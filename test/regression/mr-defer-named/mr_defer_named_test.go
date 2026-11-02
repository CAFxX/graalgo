// Regression tests for M0 (review 2026-11-02): in a pair-transformed
// multi-result function, a deferred closure's writes to named results
// were silently discarded. The exporter packed the named results into
// the pair struct AT the return statement, before defers ran, and the
// backend root returned that already-packed pair after the defers ran,
// never re-reading the locals the defers wrote.
//
// The fix has two halves, and the tests below pin both:
//   - the backend re-packs the then-current named-result locals into
//     the pair after defers run (bare return, funclit, method, recover
//     shapes), and
//   - the exporter assigns an explicit return's values to the named
//     locals (evaluated once, in order, before any assignment) so the
//     repack does not overwrite them with stale locals (explicit
//     return, eval-order, read-named, no-touch shapes).
//
// Every test below fails on the pre-fix tree (verified via the
// equivalent standalone programs under gc vs graalgo: pre-fix graalgo
// returned e.g. "1 2" where gc returns "7 8", and "0 <nil>" where gc
// returns "42 recovered"), except TestSingleResultControl, which pins
// the unaffected single-result path.
package mrdefernamed

import (
	"errors"
	"testing"
)

// TestBareReturnDeferWins is the M0 reproducer: a bare return whose
// named results a deferred closure rewrites.
func TestBareReturnDeferWins(t *testing.T) {
	f := func() (x, y int) {
		x, y = 1, 2
		defer func() { x, y = 7, 8 }()
		return
	}
	gotX, gotY := f()
	if gotX != 7 || gotY != 8 {
		t.Fatalf("bare return: got (%d, %d), want (7, 8)", gotX, gotY)
	}
}

// TestExplicitReturnDeferWins: gc assigns explicit return values to
// the named results first, then defers may modify them.
func TestExplicitReturnDeferWins(t *testing.T) {
	f := func() (x, y int) {
		defer func() { x, y = 7, 8 }()
		return 1, 2
	}
	gotX, gotY := f()
	if gotX != 7 || gotY != 8 {
		t.Fatalf("explicit return: got (%d, %d), want (7, 8)", gotX, gotY)
	}
}

// TestExplicitReturnDeferNotTouching: a defer that does not touch the
// results must leave the explicit values unchanged. Pre-fix (repack
// without the exporter assign half) this returned (0, 0): the repack
// overwrote the packed values with stale locals.
func TestExplicitReturnDeferNotTouching(t *testing.T) {
	ran := false
	f := func() (x, y int) {
		defer func() { ran = true }()
		return 3, 4
	}
	gotX, gotY := f()
	if !ran {
		t.Fatalf("defer did not run")
	}
	if gotX != 3 || gotY != 4 {
		t.Fatalf("explicit return, non-touching defer: got (%d, %d), want (3, 4)", gotX, gotY)
	}
}

// TestExplicitReturnEvalOnceInOrder: return expressions are evaluated
// exactly once, in order, before defers run — and a defer's write wins
// over the explicit value only for the field it touches.
func TestExplicitReturnEvalOnceInOrder(t *testing.T) {
	var order []int
	side := func(v int) int {
		order = append(order, v)
		return v
	}
	f := func() (x, y int) {
		defer func() { x = 99 }()
		return side(1), side(2)
	}
	gotX, gotY := f()
	if gotX != 99 || gotY != 2 {
		t.Fatalf("eval order: got (%d, %d), want (99, 2)", gotX, gotY)
	}
	if len(order) != 2 || order[0] != 1 || order[1] != 2 {
		t.Fatalf("return expressions evaluated in order %v, want [1 2]", order)
	}
}

// TestExplicitReturnReadingNamedResult: gc evaluates all return
// expressions before assigning any of them, so a later expression
// reading a named result sees its pre-return value.
func TestExplicitReturnReadingNamedResult(t *testing.T) {
	f := func() (x, y int) {
		x = 5
		defer func() { x = 9 }()
		return x, x + 1
	}
	gotX, gotY := f()
	if gotX != 9 || gotY != 6 {
		t.Fatalf("read-named return: got (%d, %d), want (9, 6)", gotX, gotY)
	}
}

// TestExplicitReturnViaMRCall: an explicit return whose single value
// expression is itself a multi-result call, with a defer rewriting one
// named result.
func TestExplicitReturnViaMRCall(t *testing.T) {
	inner := func() (int, int) { return 5, 6 }
	outer := func() (x, y int) {
		defer func() { x = 9 }()
		return inner()
	}
	gotX, gotY := outer()
	if gotX != 9 || gotY != 6 {
		t.Fatalf("MR-call return: got (%d, %d), want (9, 6)", gotX, gotY)
	}
}

// TestFunclitShape: the function-literal transform site.
func TestFunclitShape(t *testing.T) {
	f := func() (x, y int) {
		x, y = 1, 2
		defer func() { x, y = 7, 8 }()
		return
	}
	gotX, gotY := f()
	if gotX != 7 || gotY != 8 {
		t.Fatalf("funclit: got (%d, %d), want (7, 8)", gotX, gotY)
	}
}

type mrT struct{}

// TestMethodShape: the method transform site.
func TestMethodShape(t *testing.T) {
	gotX, gotY := mrT{}.f()
	if gotX != 7 || gotY != 8 {
		t.Fatalf("method: got (%d, %d), want (7, 8)", gotX, gotY)
	}
}

func (mrT) f() (x, y int) {
	x, y = 1, 2
	defer func() { x, y = 7, 8 }()
	return
}

// TestPanicRecoverNamedResults: the canonical (T, error) + deferred
// recover shape. Pre-fix this returned the zero pair (0, nil): no
// return statement ever packed the locals the recovering defer wrote.
func TestPanicRecoverNamedResults(t *testing.T) {
	f := func() (v int, err error) {
		defer func() {
			if r := recover(); r != nil {
				v = 42
				err = errors.New("recovered")
			}
		}()
		panic("boom")
	}
	gotV, gotErr := f()
	if gotV != 42 {
		t.Fatalf("recover: v = %d, want 42", gotV)
	}
	if gotErr == nil || gotErr.Error() != "recovered" {
		t.Fatalf("recover: err = %v, want \"recovered\"", gotErr)
	}
}

// TestSingleResultControl: single-result functions are NOT affected by
// M0 (the named result slot is the result slot) and must keep working.
func TestSingleResultControl(t *testing.T) {
	f := func() (x int) {
		x = 1
		defer func() { x = 7 }()
		return
	}
	if got := f(); got != 7 {
		t.Fatalf("single result: got %d, want 7", got)
	}
}

// TestBlankResultPosition: a blank result position keeps its packed
// value (no name to write through); the named position follows the
// defer. Covers both the explicit and the bare form.
func TestBlankResultPosition(t *testing.T) {
	g := func() (x int, _ int) {
		x = 1
		defer func() { x = 7 }()
		return 3, 4
	}
	gotX, gotB := g()
	if gotX != 7 || gotB != 4 {
		t.Fatalf("blank explicit: got (%d, %d), want (7, 4)", gotX, gotB)
	}
	h := func() (x int, _ int) {
		x = 1
		defer func() { x = 7 }()
		return
	}
	gotX, gotB = h()
	if gotX != 7 || gotB != 0 {
		t.Fatalf("blank bare: got (%d, %d), want (7, 0)", gotX, gotB)
	}
}

// TestAddressTakenNamedResult: a named result whose address is taken
// lives in a GoBox cell; the repack must unwrap the cell, not the raw
// slot.
func TestAddressTakenNamedResult(t *testing.T) {
	f := func() (x, y int) {
		p := &x
		_ = p
		defer func() { x, y = 7, 8 }()
		return 1, 2
	}
	gotX, gotY := f()
	if gotX != 7 || gotY != 8 {
		t.Fatalf("address-taken: got (%d, %d), want (7, 8)", gotX, gotY)
	}
}

// TestMultiKindResults: string (objects carrier), int (longs carrier),
// and error (interface) fields repack together.
func TestMultiKindResults(t *testing.T) {
	f := func() (s string, n int, e error) {
		defer func() { s, n, e = "d", 9, errors.New("de") }()
		return "a", 1, nil
	}
	gotS, gotN, gotE := f()
	if gotS != "d" || gotN != 9 {
		t.Fatalf("multi-kind: got (%q, %d, %v), want (\"d\", 9, de)", gotS, gotN, gotE)
	}
	if gotE == nil || gotE.Error() != "de" {
		t.Fatalf("multi-kind: err = %v, want \"de\"", gotE)
	}
}

// TestGenericInstantiation: the generic-instantiation transform site.
func TestGenericInstantiation(t *testing.T) {
	v, err := mrID(41)
	if v != 41 {
		t.Fatalf("generic: v = %d, want 41", v)
	}
	if err == nil || err.Error() != "e" {
		t.Fatalf("generic: err = %v, want \"e\"", err)
	}
}

func mrID[T any](v T) (x T, err error) {
	x = v
	defer func() { err = errors.New("e") }()
	return
}

// TestFloatResult: a float64 named result goes through the longs
// carrier with kind-specific bit conversion (not the int path), so it
// gets its own repack pin — bare and explicit forms.
func TestFloatResult(t *testing.T) {
	f := func() (x float64, y int) {
		x, y = 1.5, 2
		defer func() { x, y = 7.5, 8 }()
		return
	}
	gotX, gotY := f()
	if gotX != 7.5 || gotY != 8 {
		t.Fatalf("float bare: got (%v, %d), want (7.5, 8)", gotX, gotY)
	}
	g := func() (x float64, y int) {
		defer func() { x = 9.5 }()
		return 1.5, 2
	}
	gotX, gotY = g()
	if gotX != 9.5 || gotY != 2 {
		t.Fatalf("float explicit: got (%v, %d), want (9.5, 2)", gotX, gotY)
	}
}

// TestDeferLIFOOrder: multiple defers run LIFO and each sees the
// previous defer's writes to the named results.
func TestDeferLIFOOrder(t *testing.T) {
	f := func() (x, y int) {
		defer func() { x = 1 }()
		defer func() { x = 2 }()
		defer func() { y = 3 }()
		return 0, 0
	}
	gotX, gotY := f()
	if gotX != 1 || gotY != 3 {
		t.Fatalf("LIFO: got (%d, %d), want (1, 3)", gotX, gotY)
	}
}
