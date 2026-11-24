// Package h is the harness of the benchmark suite (test/bench/suite.py).
//
// It imports only syscall (BENCH_MIN_TOTAL) and no time, fmt or os: the clock
// is runtime.nanotime through go:linkname (an intrinsic under graalgo,
// toolexec D9) and the output goes through println (stderr):
//
//	ROUND <name> <i> <ns>     one line per round, in order
//	RESULT <name> <value>     the workload's result (identical every round)
//	BENCH <name> <best ns>    the best round
//
// A workload whose result differs between rounds panics: the runner compares
// RESULT with gc's, so a nondeterministic workload would hide a divergence.
package h

import (
	"syscall"
	_ "unsafe" // for go:linkname
)

//go:linkname nanotime runtime.nanotime
func nanotime() int64

// Now is a monotonic clock in nanoseconds.
func Now() int64 { return nanotime() }

// MinTotal is the least total time Run spends in rounds, in nanoseconds:
// enough for the JIT to reach steady state (the runner's steady median is the
// median of the last half of the rounds) — 4 s, or BENCH_MIN_TOTAL seconds
// (suite.py --min-total, for long-running steady state).
func MinTotal() int64 {
	v, _ := syscall.Getenv("BENCH_MIN_TOTAL")
	n := int64(0)
	for _, c := range v {
		if c < '0' || c > '9' {
			return 4e9
		}
		n = n*10 + int64(c-'0')
	}
	if n == 0 {
		return 4e9
	}
	return n * 1e9
}

// MaxRounds bounds the rounds of a fast workload.
const MaxRounds = 2000

// Run times calls of work — at least minRounds, and more until MinTotal has
// passed (at most MaxRounds) — and reports them.
func Run(name string, minRounds int, work func() int) {
	best := int64(1 << 62)
	var first int
	total := int64(0)
	minTotal := MinTotal()
	for i := 0; i < MaxRounds && (i < minRounds || total < minTotal); i++ {
		t0 := nanotime()
		r := work()
		d := nanotime() - t0
		total += d
		if i == 0 {
			first = r
		} else if r != first {
			println("RESULT", name, "nondeterministic:", first, r)
			panic("benchmark result differs between rounds")
		}
		if d < best {
			best = d
		}
		println("ROUND", name, i, d)
	}
	println("RESULT", name, first)
	println("BENCH", name, best)
	finish()
}
