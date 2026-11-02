package pkg049

// Regression tests for DEFER-TIMEOP (independent codebase review
// 2026-11-02, finding D0, P1): `defer`/`go` of a call that lowers to a
// non-`call` wire node failed the AST build.
//
// Timer/Ticker Stop/Reset lower to the "timeop" wire node (no "fun"
// field), while buildDefer and GoStatementNode.build unconditionally
// demanded call.fun, so the idiomatic cleanup shapes
// `defer tmr.Stop()`, `defer ticker.Stop()`, `defer tmr.Reset(d)` and
// `go tmr.Stop()` died with
// "defer.fields.call.fields: missing required key \"fun\"" (and the
// gostmt equivalent). Bare-statement `tmr.Stop()` always worked, because
// timeop is registered as an expression kind.
//
// The fix wraps timeop in a funclit in exportDeferStmt/exportGoStmt.
// These tests pin the semantics that distinguish a correct wrap from a
// naive one: the receiver (and, for Reset, the duration) must be
// evaluated at defer/go-statement time, NOT at return/goroutine time.
// A naive wrap that puts the raw timeop node in the funclit body would
// read a reassigned variable at return time and fail the
// *Reassigned tests below; the pre-fix code fails every test here at
// build time with the "fun" error, so no test here is tautological.
//
// Timing margins are deliberately wide (50ms vs multi-second windows)
// so the tests assert mechanism, not scheduler latency: a stopped
// timer/ticker never fires, an unstopped one fires within its period.

import (
	"testing"
	"time"
)

// deferStopTimers runs the deferred-Stop shape and returns both timers:
// the deferred Stop runs before the caller receives them, so the caller
// can observe which timer stopped (Stop on an already-stopped timer
// returns false). If reassign is true, the deferred Stop's target
// variable is reassigned to t2 after the defer statement: Go semantics
// require the Stop to still land on t1.
//
// NOTE: the results are deliberately NOT reported via named returns
// modified by a deferred closure: graalgo does not propagate such
// writes (gc: deferred closure setting named returns wins; graalgo:
// the return-statement values win — verified with a standalone
// program, 2026-11-02). That is a separate pre-existing limitation,
// outside this lane's scope; these tests observe the timers directly
// instead so they test the timeop wrap, not that limitation.
func deferStopTimers(reassign bool) (t1, t2 *time.Timer) {
	t1 = time.NewTimer(10 * time.Second)
	t2 = time.NewTimer(10 * time.Second)
	t := t1
	defer t.Stop()
	if reassign {
		t = t2
	}
	return t1, t2
}

// assertDeferredStop checks the timers returned by deferStopTimers:
// t1 must already be stopped, t2 must still be active (its Stop here
// also cleans it up).
func assertDeferredStop(t *testing.T, t1, t2 *time.Timer, what string) {
	t.Helper()
	if t1.Stop() {
		t.Fatalf("%s: deferred Timer.Stop did not stop the defer-time receiver", what)
	}
	if !t2.Stop() {
		t.Fatalf("%s: deferred Timer.Stop stopped the wrong timer", what)
	}
}

func TestDeferTimerStop(t *testing.T) {
	t1, t2 := deferStopTimers(false)
	assertDeferredStop(t, t1, t2, "defer Timer.Stop")
}

func TestDeferTimerStopReceiverReassigned(t *testing.T) {
	t1, t2 := deferStopTimers(true)
	assertDeferredStop(t, t1, t2, "defer Timer.Stop after receiver reassignment")
}

// deferTickerStop returns a ticker whose Stop was deferred in the
// helper: the deferred Stop runs before the caller receives the ticker.
func deferTickerStop() *time.Ticker {
	tk := time.NewTicker(50 * time.Millisecond)
	defer tk.Stop()
	return tk
}

func assertNoTick(t *testing.T, tk *time.Ticker) {
	t.Helper()
	// Drain any tick that queued before the Stop ran.
	select {
	case <-tk.C:
	default:
	}
	// A stopped ticker never ticks; a live 50ms ticker ticks several
	// times inside 300ms. The window is 6x the period.
	timeout := time.After(300 * time.Millisecond)
	select {
	case <-tk.C:
		t.Fatalf("ticker ticked after Stop")
	case <-timeout:
	}
}

func TestDeferTickerStop(t *testing.T) {
	assertNoTick(t, deferTickerStop())
}

// deferResetFired runs the deferred-Reset shape and returns the timer:
// the Reset duration is 50ms at defer time, reassigned to 10s before
// return when reassign is true. Go semantics require the 50ms Reset,
// so the timer must fire promptly after return.
func deferResetFired(reassign bool) *time.Timer {
	tmr := time.NewTimer(10 * time.Second)
	d := 50 * time.Millisecond
	defer tmr.Reset(d)
	if reassign {
		d = 10 * time.Second
	}
	return tmr
}

func assertFiresSoon(t *testing.T, tmr *time.Timer) {
	t.Helper()
	timeout := time.After(2 * time.Second)
	select {
	case <-tmr.C:
	case <-timeout:
		t.Fatalf("timer did not fire after deferred Reset (Reset dropped, or its duration evaluated at return time)")
	}
}

func TestDeferTimerReset(t *testing.T) {
	assertFiresSoon(t, deferResetFired(false))
}

func TestDeferTimerResetDurationReassigned(t *testing.T) {
	assertFiresSoon(t, deferResetFired(true))
}

// goStopStopped mirrors deferStopStopped for the go statement: the
// function value (receiver) is evaluated at go-statement time, so a
// reassignment racing the new goroutine must not redirect the Stop.
// The sleep gives the spawned goroutine ample time to run before the
// checks observe which timer stopped.
func goStopStopped(reassign bool) (t1Stopped, t2Stopped bool) {
	t1 := time.NewTimer(10 * time.Second)
	t2 := time.NewTimer(10 * time.Second)
	t := t1
	go t.Stop()
	if reassign {
		t = t2
	}
	time.Sleep(500 * time.Millisecond)
	t1Stopped = !t1.Stop()
	t2Stopped = !t2.Stop()
	return
}

func TestGoTimerStop(t *testing.T) {
	t1Stopped, t2Stopped := goStopStopped(false)
	if !t1Stopped {
		t.Fatalf("go Timer.Stop did not stop the timer")
	}
	if t2Stopped {
		t.Fatalf("go Timer.Stop stopped an unrelated timer")
	}
}

func TestGoTimerStopReceiverReassigned(t *testing.T) {
	t1Stopped, t2Stopped := goStopStopped(true)
	if !t1Stopped {
		t.Fatalf("go Timer.Stop followed the reassigned variable (receiver evaluated at goroutine time, not go time)")
	}
	if t2Stopped {
		t.Fatalf("go Timer.Stop stopped the reassignment target instead of the go-time receiver")
	}
}

func TestGoTickerStop(t *testing.T) {
	tk := time.NewTicker(50 * time.Millisecond)
	go tk.Stop()
	time.Sleep(500 * time.Millisecond)
	assertNoTick(t, tk)
}

// goResetFired mirrors deferResetFired for the go statement: the Reset
// duration is evaluated at go-statement time, so reassigning it before
// the goroutine runs must not change the Reset to 10s.
func goResetFired() *time.Timer {
	tmr := time.NewTimer(10 * time.Second)
	d := 50 * time.Millisecond
	go tmr.Reset(d)
	d = 10 * time.Second
	time.Sleep(500 * time.Millisecond)
	return tmr
}

func TestGoTimerResetDurationReassigned(t *testing.T) {
	assertFiresSoon(t, goResetFired())
}
