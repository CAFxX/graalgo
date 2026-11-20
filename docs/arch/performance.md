# Peak performance: benchmarks, method, gap analysis

Status: living document of the performance lane (first written 2026-11-20,
lane `perf-peak`). It is the falsifying-benchmark record SPEC §2.6 asks for
(GG-0007): every "beyond gc" claim in the docs must name a benchmark here and
beat gc **with PGO** on it, or it is struck. Linked from
[RECOVERY.md](RECOVERY.md) (WP-POLLCOST, WP-PERF).

## 1. The suite

[test/bench/suite/](../../test/bench/suite/) — 27 `package main` programs,
one directory each, on one harness ([suite/h](../../test/bench/suite/h/h.go)):
the clock is `runtime.nanotime` by `//go:linkname` and output is `println`,
so the programs need nothing the new path cannot run yet. Each program runs
its workload for at least 4 s of rounds (at most 2,000) and prints one
`ROUND` line per round, the `RESULT` (identical every round, or the harness
panics) and the best round.

| Area | Benchmarks |
|---|---|
| calls, recursion | `fib`, `closures`, `spectralnorm` |
| integer / float loops | `loop`, `floatloop`, `mandelbrot`, `fannkuch` |
| slices and arrays | `slicesum`, `sliceappend`, `matmul` (`[][]float64`), `arraymat` (`[N][N]int` values) |
| structs and methods | `structs` (value/pointer receivers, `&s[i]`), `nbody` |
| interface dispatch | `ifacemono`, `ifacepoly` (3 types), `ifacemega` (8 types) |
| maps | `mapint`, `mapstr` |
| strings and bytes | `strbuild`, `striter`, `strconv`, `bytesops`, `sort` |
| scheduler | `chanpingpong`, `workerpool`, `mutex` |
| allocation | `bintrees` |

[suite.sh](../../test/bench/suite.sh) / [suite.py](../../test/bench/suite.py)
(`make bench-suite BENCH_ARGS=...`) build every program three ways — gc, gc
with PGO (`--pgo`: a `benchpgo`-tagged build writes a CPU profile of a whole
run, then `go build -pgo=`), and graalgo (`go build -toolexec graalgo-tool`,
the node package `graalgo.go.*`) — run them interleaved `--reps` times,
fail on any RESULT that differs from gc's, and report:

- **best** — the best round; **steady median** — the median of the last
  half of the rounds (both: median over reps);
- the graalgo **warmup curve** (first ten rounds), **time to peak**
  (cumulative round time until the first round within 1.10× of the steady
  median) and **process wall time** (JVM start-up and compilation included);
- `--trace`: compilations, failures, deoptimizations and invalidations from
  `engine.TraceCompilation`; `--json FILE` for tracking.

## 2. Method

- Host: shared 16-core x86-64 (WSL2), other lanes' JVMs running. Every
  table states the load average. graalgo legs run under `nice -n 19` with
  `-Xmx2g -XX:ActiveProcessorCount=4` (the lane rules) — gc gets the whole
  machine, so scheduler and GC numbers are conservative for graalgo.
- Before/after claims are **interleaved A/B runs** (two worktrees built from
  the two commits, A and B alternating, ≥ 3 reps, medians of best rounds),
  never a run against a remembered number.
- gc's numbers are stable to ~2 % run to run; graalgo's steady medians move
  10–40 % with host load (compilation threads compete), best rounds less.
  Ratios below 1.2× are within the noise.

## 3. What landed (2026-11-19/20)

Interleaved A/B, medians of best rounds (ms):

| Change | Benchmark | before | after | gc |
|---|---|---:|---:|---:|
| WP-POLLCOST: the poll is one load of a shared poll epoch (ticker ≤ 1 ms) | `loop` | 14.81 | 6.11 | 4.51 |
| | `slicesum` | 11.88 | 6.52 | 1.52 |
| | `fib` | 4.98 | 3.29 | 2.24 |
| Drainer spins 50 µs before blocking | `chanpingpong` | 187.6 | 19.0 | 0.31 |
| | `workerpool` | 5.70 | 0.81 | 0.50 |
| `math.sqrt`, `math/bits` as gc intrinsifies them | `nbody` | 259.2 | 12.4 | 4.8 |
| Boundary-free id-indexed itab cache | `ifacemega` | 4.26 | 3.47 | 0.33 |
| Fixed-size struct copies (scalar-replaceable) | `structs` | 3.34 | 1.98 | 0.24 |
| | `closures` | 3.18 | 2.21 | 0.73 |
| Value-receiver copy with constant counts at interface call sites | `ifacemono` | 1.84 | 0.82 | 0.27 |
| | `ifacepoly` | 1.50 | 0.75 | 0.38 |
| WP-SCHED direct handoff (gc runnext) + bounded handoff spin (§3.1) | `chanpingpong` | 10.54 | 1.13 | 0.31 |
| | `workerpool` | 0.80 | 0.81 | 0.50 |
| | `mutex` | 0.74 | 0.76 | 0.60 |

**WP-POLLCOST design.** The poll's fast path was a thread-slot read, a
volatile `exitEpoch` read, a countdown decrement **and store**, and two
compares. Neither RECOVERY prototype was needed: the fast path is now one
volatile load of `SchedulerCore.pollEpoch` compared with the epoch the
thread saw at its last full poll (in `GoThreadSlot`), with no store. A
daemon `graalgo-poll-ticker` bumps the epoch every quarter of the smallest
quantum (clamped 50 µs–1 ms); process exit bumps it too, so the exit freeze
still happens at the next poll. Quantum expiry is noticed at most one tick
late. The seeded gate
[preempt_spin.go](../../test/m1r/programs/preempt_spin.go) times out when the
back-edge poll is removed. Remaining cost on `loop`: the poll keeps a call
(the full poll) inside the loop, which blocks loop-invariant motion of
non-final loads and vectorization — P-B measured a poll-free `loop` at
1.0 ms. Closing that needs the poll out of counted inner loops with a
provable trip bound (strip-mining: poll once per 2^k iterations of an
inner counted loop), a later item.

### 3.1 Direct handoff (WP-SCHED, [ADR-0016](../dev/adr/ADR-0016-direct-handoff.md))

Every park/unpark used to be two tokens through the drainer thread plus a
virtual-thread unpark issued from that platform thread (an external
submission to the carrier pool and, usually, a carrier wake-up). Now a
goroutine's wake of another is deferred as its `runnext`; when the waker
parks within 20 µs it hands the woken goroutine its in-flight slot and
quantum directly (one HANDOFF token books it; the waker sets the target's
handoff flag and unparks it from its own virtual thread), and then spins up
to 5 µs for the answer before unmounting — only while the drainer saw idle
slots, never more spinners than idle slots. Measured with the A/B switch
`-Dgraalgo.sched.handoff=false` on one build (A = off, B = on, alternating,
3 reps, load 13–15): `chanpingpong` best 10.5/9.3/22.0 → 1.24/1.02/1.13 ms,
steady median 54.6/35.2/54.3 → 2.04/1.97/1.92 ms; `workerpool` and `mutex`
unchanged (within noise). The handoff alone (no spin) gave 6.7/5.1/3.6 ms:
the rest is the virtual-thread unmount/remount the spin avoids. A ping-pong
pair at GOMAXPROCS 1 still yields the slot at the quantum (a handoff inherits
the quantum deadline): a co-scheduled spinner gets 0.47 of the CPU
(`WakeupRaceStressTest.testHandoffPairDoesNotStarveSpinner`). Remaining gap
(3.6× gc): the Loom unpark/park of the partner on every hop when the spin
misses, and the per-hop token through the drainer that keeps its books.

## 4. Current standing vs gc

### 4.1 Whole suite

Best rounds, ms; one rep each (load average 17–21 at the start, 13 at the
end); "before" is master 545a5ab3 (2026-11-19), "after" this lane's head.
`gc+PGO` profiles a whole run of the same program. Single-rep ratios move
by up to ~25 % with host load (`ifacemega` here; its interleaved A/B in §3
is the reliable number).

| Benchmark | gc | gc+PGO | before | after | before/gc | after/gc | after/gc+PGO | time to peak (ms) |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `arraymat` | 0.78 | 0.78 | 1.87 | 1.34 | 2.4× | 1.7× | 1.7× | 159 |
| `bintrees` | 38.76 | 37.32 | 19.59 | 21.58 | 0.7× | 0.6× | 0.6× | 501 |
| `bytesops` | 2.23 | 2.09 | 10.10 | 7.61 | 5.0× | 3.4× | 3.6× | 563 |
| `chanpingpong` | 0.31 | 0.27 | 106.61 | 1.13† | 342.8× | 3.6×† | 4.2×† | — |
| `closures` | 0.73 | 0.73 | 7.37 | 2.31 | 10.0× | 3.1× | 3.1× | 240 |
| `fannkuch` | 13.49 | 13.37 | 37.38 | 25.50 | 2.8× | 1.9× | 1.9× | 636 |
| `fib` | 2.25 | 1.20 | 4.67 | 3.35 | 2.1× | 1.5× | 2.8× | 206 |
| `floatloop` | 7.99 | 7.97 | 8.72 | 7.91 | 1.1× | 1.0× | 1.0× | 318 |
| `ifacemega` | 0.33 | 0.33 | 4.13 | 4.54 | 12.7× | 13.9× | 13.9× | 258 |
| `ifacemono` | 0.27 | 0.25 | 2.23 | 0.82 | 8.2× | 3.0× | 3.3× | 410 |
| `ifacepoly` | 0.38 | 0.33 | 1.80 | 0.74 | 4.7× | 1.9× | 2.3× | 178 |
| `loop` | 4.84 | 4.36 | 14.58 | 7.17 | 3.2× | 1.5× | 1.6× | 218 |
| `mandelbrot` | 2.14 | 2.18 | 5.32 | 2.81 | 2.5× | 1.3× | 1.3× | 179 |
| `mapint` | 7.59 | 7.03 | 3.63 | 3.51 | 0.5× | 0.5× | 0.5× | 376 |
| `mapstr` | 0.88 | 0.87 | 1.39 | 1.28 | 1.6× | 1.5× | 1.5× | 212 |
| `matmul` | 0.39 | 0.39 | 2.42 | 1.45 | 6.3× | 3.7× | 3.7× | 287 |
| `mutex` | 0.60 | 0.61 | 0.98 | 0.77 | 1.6× | 1.3× | 1.3× | 327 |
| `nbody` | 4.79 | 4.12 | 236.92 | 11.86 | 49.6× | 2.5× | 2.9× | 501 |
| `sliceappend` | 2.05 | 2.15 | 3.79 | 2.82 | 1.6× | 1.4× | 1.3× | 393 |
| `slicesum` | 1.53 | 1.52 | 11.58 | 6.66 | 7.6× | 4.4× | 4.4× | 179 |
| `sort` | 7.87 | 7.80 | 20.98 | 15.70 | 2.7× | 2.0× | 2.0× | 1104 |
| `spectralnorm` | 1.19 | 1.20 | 5.01 | 1.61 | 4.2× | 1.4× | 1.3× | 124 |
| `strbuild` | 0.34 | 0.32 | 0.99 | 0.78 | 2.3× | 2.3× | 2.4× | 318 |
| `strconv` | 1.14 | 1.25 | 3.45 | 2.05 | 3.0× | 1.8× | 1.6× | 927 |
| `striter` | 1.37 | 1.22 | 9.08 | 5.41 | 6.7× | 3.9× | 4.4× | 447 |
| `structs` | 0.24 | 0.29 | 3.95 | 1.97 | 16.4× | 8.2× | 6.9× | 290 |
| `workerpool` | 0.49 | 0.50 | 7.10 | 0.84 | 13.1× | 1.7× | 1.7× | 297 |

† updated 2026-11-20 from the WP-SCHED A/B (§3.1; was 8.19 ms, 26.6×); the means below predate it.

Geometric mean graalgo/gc: **4.49× before, 2.26× after** (2.37× against
gc+PGO); median 3.2× → 1.9×. Process wall time is 4–6 s for every
benchmark (4 s of rounds plus ~0.5–1 s of JVM start, Truffle warmup and
compilation); time to peak is 0.1–1.1 s. PGO matters to gc only on `fib`
(2.25 → 1.20 ms: profile-guided inlining of the recursion) and a little on
`nbody`, `loop`, `chanpingpong` — elsewhere it is within noise, so "beat
gc+PGO" and "beat gc" are the same bar on this suite today. `--trace` on the slowest eight shows
no failed compilation and at most 5 deoptimizations per run: the remaining
gaps are code quality, not deopt loops.

## 5. Gap analysis, ranked by expected leverage

1. **Scheduler hop** (`chanpingpong` 3.6× after the WP-SCHED handoff, §3.1;
   `workerpool` 1.7×, `mutex` 1.3×): a handoff hop still costs a virtual-thread
   unpark/park when the 5 µs spin misses, and every hop still sends one token
   to the drainer. Next: let the drainer learn handoffs lazily (batch the
   bookkeeping) and keep `workerpool`'s buffered-channel wakes on the handoff
   path (its producer rarely parks within the window). BUG-0038 is fixed.

2. **Call overhead** (`fib`, `closures`, `spectralnorm`, `structs`): an
   `Object[]` argument array, boxed scalars and a boxed result per
   non-inlined call, plus the entry poll. Specialized fixed-arity call
   nodes and an unboxed return slot, and eliding the entry poll of
   functions without call sites (every call cycle still contains a polling
   entry), are the next items.
3. **Loop shape** (`slicesum` ~4×, `loop`, `matmul` ~4×): the poll's slow
   call in every iteration (above), and `Slice` headers re-read through the
   frame; strip-mined polls and hoisting of the immutable header fields.
4. **Splitting** (`sort` 2×, every library helper taking a func or an
   interface): `isCloningAllowed` is false today, so a helper's inline
   caches are shared by all callers and go megamorphic in real programs.
   The design (truffle-design §4.7) wants `cloneUninitialized` by GIR
   re-decode; that is the key enabler of SPEC §2.6.1's devirtualization
   cascade beyond micro-benchmarks.
5. **Megamorphic dispatch** (`ifacemega` ~10×): the itab is now two loads,
   but the call is an `IndirectCallNode` with an argument array; gc's is an
   indirect call with register arguments.
6. **Strings/bytes** (`bytesops` 5×, `striter`): `Str` windows and
   per-byte loops over `byte[]`; the `internal/bytealg` kernels are Go
   loops where gc has SIMD assembly — JDK-intrinsic replacements
   (`Arrays.mismatch`, `indexOf`) are the cheap wins.

## 6. Where beating gc is plausible, and where it is not

Already ahead (steady state): `mapint` (~0.5×: the JVM's allocation and
`GoMap` beat gc's swiss table on this mix), `bintrees` (~0.7×: TLAB
allocation and a generational GC beat gc's non-moving collector on
allocation-heavy trees). These are real but not the SPEC §2.6 thesis.

**Plausible** (no evidence yet — each needs its falsifying benchmark to beat
gc+PGO): long-running servers whose hot paths go through interfaces
(SPEC §2.6.1 — needs splitting, item 4, and a benchmark that is not a
micro-benchmark: an `io.Reader` pipeline, `sort.Interface`, `http.Handler`);
speculative devirtualization of func values passed to library helpers;
JIT-time bounds-check elimination with range profiles; value-based
specialization of hot constants; profile-guided layout. Truffle can do these
because profiles are always on; gc+PGO gets only inlining and devirtualizing
of hot calls from one recorded profile.

**Not plausible**: start-up and short runs (JVM start plus warmup: every
graalgo leg needs 0.1–1 s of rounds before its first compiled round, gc
none); tight scalar loops where gc's code is already near the hardware
(`floatloop` is at parity, nothing to win); the scheduler hop while
goroutines are virtual threads behind one drainer (item 1 halves it at best).

## 7. Waits for later milestones

- `fmt` and real programs (M1d onward): the suite avoids `fmt`/`os`; a
  realistic interface-heavy benchmark (SPEC §2.6.1's falsifier) needs them.
- Native-image / AOT modes (P-D, P-F): no numbers yet; GG-0050's
  "performant without the JIT" is open.
- PGO-guided inlining parity, escape analysis across non-inlined calls,
  vectorization (Graal community has little; the Oracle GraalVM compiler
  in the toolchain may do more — not yet measured).
- JVM flags: `-XX:+UseCompactObjectHeaders` and the GC choice (G1 default)
  are untested on the suite; `engine.Mode=throughput` likewise.
