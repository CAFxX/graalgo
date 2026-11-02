// Regression tests for PKG-048 (REVIEW-2026-11-02-16, finding T0):
// the tree once claimed that select on time.After hangs forever under
// graalgo ("timer channels do not fire under graalgo"). That claim was
// verified stale on 2026-11-02 on HEAD 06d19598: the repro shapes below
// fire under graalgo exactly as under gc. These tests pin the claim so
// it can never silently go stale again in either direction — if timers
// stop firing, the tests fail (or hang and are killed by the `timeout
// 600` in the test-regression Makefile target) instead of a comment
// asserting behaviour nobody re-checks.
//
// Pre-fix behaviour (had the claim been true): every test below would
// hang forever — the select would never take the time.After branch and
// the watchdog test would never observe its timeout. The tests are
// therefore capable of failing, not tautological: the fire branch is
// distinguished from the guard branch by the output assertion, and the
// guard durations (5s) are 25-50x the fire durations so a slow-but-live
// timer cannot masquerade as a dead one.
package pkg048

import (
	"testing"
	"time"
)

// TestSelectTimeAfterFires is the T0 repro verbatim: select on a 100ms
// time.After against a 5s time.After guard must take the 100ms branch.
func TestSelectTimeAfterFires(t *testing.T) {
	select {
	case <-time.After(100 * time.Millisecond):
		// Expected: the short timer fires.
	case <-time.After(5 * time.Second):
		t.Fatal("select took the 5s time.After branch instead of the 100ms one: short timer did not fire")
	}
}

// TestReceiveTimeAfterFires pins the non-select shape: a plain receive
// on time.After must unblock when the timer fires.
func TestReceiveTimeAfterFires(t *testing.T) {
	done := make(chan struct{})
	go func() {
		<-time.After(100 * time.Millisecond)
		close(done)
	}()
	select {
	case <-done:
		// Expected: the receive unblocked.
	case <-time.After(5 * time.Second):
		t.Fatal("plain receive on time.After did not unblock within 5s")
	}
}

// TestNewTimerSelectFires pins the time.NewTimer shape: select on the
// timer's channel must fire, not hang.
func TestNewTimerSelectFires(t *testing.T) {
	tmr := time.NewTimer(100 * time.Millisecond)
	// No defer tmr.Stop(): a deferred method-value call is not
	// representable in the frontend yet (defer.fields.call.fields:
	// missing required key "fun"), and the stop is unnecessary here
	// — the timer fires and the test ends.
	select {
	case <-tmr.C:
		// Expected: the timer channel fires.
	case <-time.After(5 * time.Second):
		t.Fatal("select on time.NewTimer channel did not fire within 5s")
	}
}

// TestWatchdogTimeAfterFires is the goroutine-watchdog shape from the
// pkg031 history: a worker that never reports must be caught by a
// time.After watchdog in main. Under the stale PKG-048 claim this test
// hangs forever instead of failing — the watchdog itself never fires.
func TestWatchdogTimeAfterFires(t *testing.T) {
	never := make(chan struct{})
	quit := make(chan struct{})
	go func() {
		// Simulate a hung worker: it parks until released and never
		// reports on `never`. (An earlier draft parked it in an empty
		// `select {}`, which never returns; the test binary then never
		// reached program-end quiescence under graalgo. Parking on
		// `quit` keeps the same "never reports within the window"
		// property while letting the test shut down cleanly.)
		<-quit
	}()
	select {
	case <-never:
		t.Fatal("hung worker unexpectedly reported")
	case <-time.After(200 * time.Millisecond):
		// Expected: the watchdog fires. (In a real hung-worker test
		// this branch would call t.Fatal; here the watchdog firing
		// IS the assertion, so reaching it passes.)
	}
	close(quit)
}
