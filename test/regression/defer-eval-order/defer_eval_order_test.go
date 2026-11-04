// Regression tests for REVIEW-2026-11-04-11 NEW-P1-4 / NEW-P1-5 (R11-DEFER):
// `defer` evaluation order and nil func values.
//
// NEW-P1-5 (nil func defer) is pinned end to end here: gc pushes a nil
// func value like any other deferred call and panics with the
// nil-dereference error only when the call runs at unwind, so
// statements after the defer still execute and defers pushed later
// still run first (LIFO). Pre-fix, graalgo panicked at the defer
// statement itself: code after the defer never ran and later defers
// were never pushed.
//
// NEW-P1-4 (function value evaluated before arguments) is pinned at
// the node level by DeferNodeTest (truffle module): the Go-level shape
// that would expose it end to end, `defer sideFn()(sideArg())`, is
// rejected by the frontend before it reaches DeferNode ("cannot defer
// call to call (WS D)"), and every defer shape the frontend does
// accept evaluates the function value without a user-visible side
// effect. What IS observable end to end — the method-receiver order,
// which the reorder must not disturb — is pinned here by
// TestDeferMethodReceiverBeforeArgs (receiver, then arguments, then
// body, then the deferred body, matching gc).
package deferevalorder

import (
	"fmt"
	"strings"
	"testing"
)

// Shared observation log. Tests reset it and never run in parallel.
var log []string

type orderT struct{}

func (orderT) M(i int) { log = append(log, "deferred") }

func recvFn() orderT {
	log = append(log, "recv")
	return orderT{}
}

func argFn() int {
	log = append(log, "arg")
	return 7
}

func checkLog(t *testing.T, want []string) {
	t.Helper()
	if len(log) != len(want) {
		t.Fatalf("log = %q, want %q", log, want)
	}
	for i := range want {
		if log[i] != want[i] {
			t.Fatalf("log = %q, want %q", log, want)
		}
	}
}

func checkNilPanic(t *testing.T, rec any) {
	t.Helper()
	if rec == nil {
		t.Fatalf("recovered nil, want nil-dereference panic")
	}
	if !strings.Contains(fmt.Sprint(rec), "invalid memory address or nil pointer dereference") {
		t.Fatalf("recovered = %v, want nil-dereference panic", rec)
	}
}

// NEW-P1-4 (observable part): a deferred method call evaluates the
// receiver, then the arguments, then the body runs, then the deferred
// body — the reorder in DeferNode must not disturb this order.
func TestDeferMethodReceiverBeforeArgs(t *testing.T) {
	log = nil
	func() {
		defer recvFn().M(argFn())
		log = append(log, "body")
	}()
	checkLog(t, []string{"recv", "arg", "body", "deferred"})
}

// Helper for the nil tests: no recover of its own, so the nil panic
// propagates to the caller after the later defer has run.
func nilDeferHelper() {
	var f func()
	defer f()
	log = append(log, "after-defer-stmt")
	defer func() { log = append(log, "later-defer") }()
	log = append(log, "body-end")
}

// NEW-P1-5: a nil defer in the same function as its recover defer:
// statements after the defer run, the later defer runs first at
// unwind, then the nil call panics, then the recover defer recovers it.
func TestDeferNilRecoveredInSameFunction(t *testing.T) {
	log = nil
	var rec any
	func() {
		defer func() {
			rec = recover()
			log = append(log, "recover-defer")
		}()
		var f func()
		defer f()
		log = append(log, "after-defer-stmt")
		defer func() { log = append(log, "later-defer") }()
		log = append(log, "body-end")
	}()
	checkLog(t, []string{"after-defer-stmt", "body-end", "later-defer", "recover-defer"})
	checkNilPanic(t, rec)
}

// NEW-P1-5: a nil defer with no recover in its own function: the
// panic propagates to the caller, but only after the later defer
// (pushed after the nil defer) has run.
func TestDeferNilPropagatesToCallerAfterLaterDefers(t *testing.T) {
	log = nil
	var rec any
	func() {
		defer func() { rec = recover() }()
		nilDeferHelper()
	}()
	checkLog(t, []string{"after-defer-stmt", "body-end", "later-defer"})
	checkNilPanic(t, rec)
}
