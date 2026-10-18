// Regression test for RV22-P0-1: SelectOp dropped a send-on-closed
// observation made in the block-phase sweep when other cases had already
// enqueued, then parked instead of panicking.
//
// A channel that closes between a select's poll and its block-phase sweep
// is observed by the sweep as a poisoned send case. gc treats that case as
// never-blocking and panics with "send on closed channel". Pre-fix,
// graalgo only panicked when no other case had enqueued (mine.isEmpty());
// otherwise the poison was silently dropped and the select parked on the
// remaining open channels -- blocking forever.
//
// This test hammers the race window: many goroutines block in select over
// unbuffered send cases while the test closes half the channels underneath
// them, sweeping the close-burst delay across iterations so that some
// iteration lands the burst inside a select's poll-to-sweep window. The
// other half of the channels stays open forever, so a select that drops
// the poison parks on them forever instead of panicking: pre-fix this test
// hangs (caught by the `timeout 600` on the test-regression Makefile
// target; no in-test watchdog is possible because timers do not fire under
// graalgo, PKG-048). Every non-buggy interleaving panics: a close before
// the poll panics in the poll phase, and a close after enqueue poisons via
// GoChan.close's own waiter path.
//
// The test deliberately imports only testing: at this tree state the
// synthetic fmt mis-exports (bad assign node for a 3-result ReadRune) and
// importing strings or os trips a pre-existing GoStruct NPE, both
// unrelated to this fix.
package p0selectpoison

import "testing"

// panicMessage renders a recovered panic value as a string without fmt.
// It works for both gc (runtime.plainError, via error) and graalgo (Go
// string).
func panicMessage(r any) string {
	switch v := r.(type) {
	case string:
		return v
	case error:
		return v.Error()
	default:
		return ""
	}
}

// spin burns CPU; used to sweep the close-burst delay across iterations.
func spin(n int) {
	x := 0
	for i := 0; i < n; i++ {
		x += i
	}
	if x == -1 {
		panic("unreachable")
	}
}

func TestSelectSendOnClosedDuringSweep(t *testing.T) {
	const cases = 64
	const closedCases = 32 // closed each iteration; the rest stay open forever
	const selects = 16
	const iters = 400
	for iter := 0; iter < iters; iter++ {
		chs := make([]chan int, cases)
		for i := range chs {
			chs[i] = make(chan int)
		}
		done := make(chan string, selects)
		for s := 0; s < selects; s++ {
			go func() {
				defer func() {
					r := recover()
					if r == nil {
						done <- "no panic"
						return
					}
					done <- panicMessage(r)
				}()
				// All sends are unbuffered with no receivers: no case can
				// ever complete. Any return without panic is a failure.
				select {
				case chs[0] <- 1:
				case chs[1] <- 1:
				case chs[2] <- 1:
				case chs[3] <- 1:
				case chs[4] <- 1:
				case chs[5] <- 1:
				case chs[6] <- 1:
				case chs[7] <- 1:
				case chs[8] <- 1:
				case chs[9] <- 1:
				case chs[10] <- 1:
				case chs[11] <- 1:
				case chs[12] <- 1:
				case chs[13] <- 1:
				case chs[14] <- 1:
				case chs[15] <- 1:
				case chs[16] <- 1:
				case chs[17] <- 1:
				case chs[18] <- 1:
				case chs[19] <- 1:
				case chs[20] <- 1:
				case chs[21] <- 1:
				case chs[22] <- 1:
				case chs[23] <- 1:
				case chs[24] <- 1:
				case chs[25] <- 1:
				case chs[26] <- 1:
				case chs[27] <- 1:
				case chs[28] <- 1:
				case chs[29] <- 1:
				case chs[30] <- 1:
				case chs[31] <- 1:
				case chs[32] <- 1:
				case chs[33] <- 1:
				case chs[34] <- 1:
				case chs[35] <- 1:
				case chs[36] <- 1:
				case chs[37] <- 1:
				case chs[38] <- 1:
				case chs[39] <- 1:
				case chs[40] <- 1:
				case chs[41] <- 1:
				case chs[42] <- 1:
				case chs[43] <- 1:
				case chs[44] <- 1:
				case chs[45] <- 1:
				case chs[46] <- 1:
				case chs[47] <- 1:
				case chs[48] <- 1:
				case chs[49] <- 1:
				case chs[50] <- 1:
				case chs[51] <- 1:
				case chs[52] <- 1:
				case chs[53] <- 1:
				case chs[54] <- 1:
				case chs[55] <- 1:
				case chs[56] <- 1:
				case chs[57] <- 1:
				case chs[58] <- 1:
				case chs[59] <- 1:
				case chs[60] <- 1:
				case chs[61] <- 1:
				case chs[62] <- 1:
				case chs[63] <- 1:
				}
			}()
		}
		// Sweep the burst delay: some iteration lands the closes inside a
		// select's poll-to-sweep window.
		spin(iter * 2500)
		for k := 0; k < closedCases; k++ {
			close(chs[k])
		}
		for s := 0; s < selects; s++ {
			msg := <-done
			if msg != "send on closed channel" {
				t.Fatalf("iter %d select %d: got panic %q, want %q",
					iter, s, msg, "send on closed channel")
			}
		}
	}
}
