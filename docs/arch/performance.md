# Peak performance: benchmarks, method, gap analysis

Status: living document of the performance lane (first written 2026-11-20,
lane `perf-peak`). It is the falsifying-benchmark record SPEC §2.6 asks for
(GG-0007): every "beyond gc" claim in the docs must name a benchmark here and
beat gc **with PGO** on it, or it is struck. Linked from
[RECOVERY.md](RECOVERY.md) (WP-POLLCOST, WP-PERF).
The goal these numbers serve — long-running programs faster than gc, with
its thresholds, workload tiers and benchmark portfolio — is
[north-star.md](north-star.md); its realistic programs are §8 below.

## 1. The suite

[test/bench/suite/](../../test/bench/suite/) — 28 `package main` programs,
one directory each, on one harness ([suite/h](../../test/bench/suite/h/h.go)):
the clock is `runtime.nanotime` by `//go:linkname` and output is `println`,
so the programs need nothing the new path cannot run yet. Each program runs
its workload for at least 4 s of rounds (at most 2,000; `--min-total S`
links a longer minimum into the harness, for long-running steady state) and prints one
`ROUND` line per round, the `RESULT` (identical every round, or the harness
panics) and the best round.

| Area | Benchmarks |
|---|---|
| calls, recursion | `fib`, `closures`, `spectralnorm` |
| integer / float loops | `loop`, `floatloop`, `mandelbrot`, `fannkuch` |
| slices and arrays | `slicesum`, `sliceappend`, `matmul` (`[][]float64`), `arraymat` (`[N][N]int` values) |
| structs and methods | `structs` (value/pointer receivers, `&s[i]`), `nbody` |
| interface dispatch | `ifacemono`, `ifacepoly` (3 types), `ifacemega` (8 types), `ifacesplit` (one helper, 5 callers × 5 types: splitting) |
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
inner counted loop) — done by lane perf-calls, §3.1.

### 3.1 Lane perf-calls (2026-11-20): loop shape, call overhead, splitting

Two interleaved A/B rounds over the whole suite (3 reps each, medians of
best rounds, ms; load 11–16). Round 1: 77f31b62 → strip-mining. Round 2:
strip-mining → leaf entry polls + splitting + bytealg kernels.

| Change | Benchmark | before | after | gc |
|---|---|---:|---:|---:|
| Strip-mined polls in counted loops | `loop` | 6.66 | 0.85 | 4.83 |
| | `slicesum` | 6.64 | 1.96 | 1.53 |
| | `matmul` | 1.46 | 0.62 | 0.39 |
| | `arraymat` | 1.34 | 0.66 | 0.77 |
| | `sliceappend` | 2.75 | 2.03 | 1.75 |
| No entry poll in call-free functions | `closures` | 2.17 | 0.90 | 0.73 |
| | `ifacemono` | 0.67 | 0.31 | 0.27 |
| | `ifacepoly` | 0.58 | 0.34 | 0.38 |
| | `spectralnorm` | 1.87 | 1.22 | 1.19 |
| | `structs` | 2.00 | 1.66 | 0.24 |
| Splitting (`cloneUninitialized` by GIR re-decode) | `ifacesplit` (new) | 0.64 | 0.06 | 0.05 |
| Word-at-a-time `IndexByte`/`Count`/`Index` | `bytesops` | 6.76 | 5.38 | 2.12 |
| Interface calls share the data word with read-only value receivers | `ifacemega` | 3.30 | 2.31 | 0.33 |

Geomean graalgo/gc over the suite: 2.27 → 1.85 (round 1), 1.96 → 1.58
(round 2; the two rounds' "before" columns differ by host load).
The receiver change has its own A/B (interface benchmarks only, load 11).

**Strip-mining.** A counted loop — `for …; i < X; i++` over a signed integer
the body never assigns, X built from constants, unassigned locals and `len`
of unassigned slice/string/array locals; range over a slice, array or signed
int — runs in compiled code as strips of at most 1024 iterations (at most
2^15 node executions) of an inner loop tested only at its head, then polls
once ([Ctl.java](../../internal/backend/truffle/src/main/java/graalgo/go/nodes/Ctl.java)
`Repeat.strip`). The inner loop has no call and no volatile load, so Graal
hoists guards and vectorizes it: `loop` is now 5.7× faster than gc. The
interpreter keeps the per-iteration poll (OSR counts stay exact).
[preempt_spin.go](../../test/m1r/programs/preempt_spin.go) has counted-loop
spinners and times out with the strip's poll removed.

**Leaf entry polls.** A function whose body contains no call skips its entry
poll; loops poll themselves and every recursion cycle passes a calling
function's entry. The win is mostly closures and interface methods inlined
into hot loops, where the inlined entry poll was the loop's only memory
barrier.

**Splitting.** `ifacesplit` is SPEC §2.6.1's helper case: one function
ranges over `[]Shape` and calls `Area`, five callers pass slices of five
types. Without splitting the helper's call site is megamorphic (0.63 ms with
`-Dpolyglot.engine.Splitting=false`); with it `engine.TraceSplitting` shows
one copy of `total` per caller and each inlines its one `Area` (0.06 ms, at
gc). `sort` does not move: its helpers are reached through one call site
each.

**Negative results** (measured, not landed):

- An inner strip loop tested at its *end* (`do … while`): 2–3× slower than
  the per-iteration poll it replaced on `slicesum`/`matmul` — Graal treats
  only head-tested loops as counted. Two Truffle `LoopNode`s nested (strip
  loop as its own `LoopNode`): slower still. An int countdown instead of the
  `long` index as the strip's induction variable: no change.
- Polling when `idx & (strip-1) == 0` inside the single loop: the slow-path
  call stays in the loop body; `slicesum` 4.0, `matmul` 1.04 ms — barely
  better than polling every iteration.
- Strip-mining only innermost loops: no difference.
- `matmul` stays 1.5× gc: a poll-free build (no back-edge poll at all) runs
  it in 0.21 ms (vectorized), the strip-mined one in 0.58 ms; the Java inner
  loop inside the `LoopNode` body is optimized less well than a `LoopNode`
  loop. Not understood yet (needs an IGV look at the vectorizer's decision).
- Truffle's recursive inlining depth (`compiler.InliningRecursionDepth`,
  default 2): 4 takes `fib` 3.27 → 2.70 ms and `bintrees` −10 %, 6 takes
  `fib` to 2.24 (gc; gc+PGO 1.20) — but `bintrees`' `main.work` then
  compiles in 1.2–1.4 s instead of 0.16 s with 5–7× the code size, and
  `sort`/`fannkuch` do not move. Not adopted; a per-function policy (deeper
  only for small self-recursive bodies) would be the way to take `fib`.
- Megamorphic interface calls: with the receiver copy gone, what remains of
  `ifacemega` (7× gc) is the `IndirectCallNode` with its argument array and
  the itab load; not attacked here.

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

Best rounds, ms; one rep each (`make bench-suite BENCH_ARGS="--pgo --reps 1"`,
load average 10 at the start, 13 at the end). "before" is the previous
table's graalgo column (77f31b62, one rep, load 13–21), "after" lane
perf-calls' head (strip-mining, leaf entry polls, splitting, read-only
receivers, bytealg kernels, merged with master). `gc+PGO` profiles a whole
run of the same program. Single-rep ratios move by up to ~25 % with host
load; §3 and §3.1 hold the interleaved A/B numbers.

† `chanpingpong` re-measured on this lane merged with master's WP-SCHED handoff (§3.1; the
rest of the table was measured before the merge; before the merge its "after" was 10.81 ms, 35.1×).

| Benchmark | gc | gc+PGO | before | after | before/gc | after/gc | after/gc+PGO | time to peak (ms) |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `arraymat` | 0.77 | 0.78 | 1.34 | 0.66 | 1.7× | 0.9× | 0.8× | 274 |
| `bintrees` | 30.85 | 27.86 | 21.58 | 16.84 | 0.7× | 0.5× | 0.6× | 392 |
| `bytesops` | 2.21 | 1.98 | 7.61 | 5.37 | 3.5× | 2.4× | 2.7× | 362 |
| `chanpingpong` | 0.31 | 0.27 | 8.19 | 0.94† | 26.6× | 3.0×† | 3.3×† | 622 |
| `closures` | 0.73 | 0.73 | 2.31 | 0.90 | 3.1× | 1.2× | 1.2× | 208 |
| `fannkuch` | 12.85 | 12.61 | 25.50 | 24.09 | 2.0× | 1.9× | 1.9× | 683 |
| `fib` | 2.23 | 1.19 | 3.35 | 3.42 | 1.5× | 1.5× | 2.9× | 1159 |
| `floatloop` | 7.97 | 8.06 | 7.91 | 8.01 | 1.0× | 1.0× | 1.0× | 138 |
| `ifacemega` | 0.33 | 0.33 | 4.54 | 2.34 | 13.9× | 7.1× | 7.2× | 451 |
| `ifacemono` | 0.27 | 0.24 | 0.82 | 0.31 | 3.0× | 1.1× | 1.3× | 163 |
| `ifacepoly` | 0.38 | 0.33 | 0.74 | 0.30 | 1.9× | 0.8× | 0.9× | 129 |
| `ifacesplit` | 0.05 | 0.05 | — | 0.06 | — | 1.3× | 1.3× | 126 |
| `loop` | 4.55 | 4.35 | 7.17 | 0.88 | 1.6× | 0.2× | 0.2× | 216 |
| `mandelbrot` | 2.11 | 2.17 | 2.81 | 2.65 | 1.3× | 1.3× | 1.2× | 143 |
| `mapint` | 6.96 | 7.00 | 3.51 | 3.47 | 0.5× | 0.5× | 0.5× | 928 |
| `mapstr` | 0.88 | 0.88 | 1.28 | 1.29 | 1.5× | 1.5× | 1.5× | 183 |
| `matmul` | 0.39 | 0.50 | 1.45 | 0.59 | 3.8× | 1.5× | 1.2× | 400 |
| `mutex` | 0.60 | 0.60 | 0.77 | 0.75 | 1.3× | 1.3× | 1.3× | 263 |
| `nbody` | 4.77 | 3.94 | 11.86 | 10.47 | 2.5× | 2.2× | 2.7× | 1111 |
| `sliceappend` | 1.84 | 1.83 | 2.82 | 1.93 | 1.5× | 1.0× | 1.1× | 379 |
| `slicesum` | 1.52 | 1.52 | 6.66 | 1.91 | 4.4× | 1.3× | 1.3× | 168 |
| `sort` | 7.78 | 7.83 | 15.70 | 14.84 | 2.0× | 1.9× | 1.9× | 1051 |
| `spectralnorm` | 1.19 | 1.20 | 1.61 | 1.22 | 1.4× | 1.0× | 1.0× | 157 |
| `strbuild` | 0.51 | 0.35 | 0.78 | 0.75 | 1.5× | 1.5× | 2.1× | 368 |
| `strconv` | 1.13 | 1.07 | 2.05 | 2.12 | 1.8× | 1.9× | 2.0× | 980 |
| `striter` | 1.33 | 1.21 | 5.41 | 5.26 | 4.1× | 3.9× | 4.4× | 421 |
| `structs` | 0.24 | 0.29 | 1.97 | 1.69 | 8.2× | 7.0× | 5.9× | 224 |
| `workerpool` | 0.51 | 0.50 | 0.84 | 0.72 | 1.7× | 1.4× | 1.4× | 253 |

Geometric mean graalgo/gc: **2.26× before, 1.42× after** (1.50× against
gc+PGO; median 1.3×; on the 27 benchmarks of the previous table 2.28× →
1.43× in this run). graalgo is now ahead of gc+PGO on `loop` (0.2×,
vectorized), `arraymat`, `mapint`, `bintrees` and at parity on `ifacepoly`,
`floatloop`, `spectralnorm`, `ifacemono`, `sliceappend`; `ifacesplit` is
within 1.3× (splitting; 13× without it). PGO matters to gc only on `fib`
(2.23 → 1.19 ms), `nbody` and `strbuild` here. Process wall time is 1–5 s;
time to peak 0.1–1.2 s.

## 5. Gap analysis, ranked by expected leverage

1. **Scheduler hop** (`chanpingpong` 3.6× after the WP-SCHED handoff, §3.1;
   `workerpool` 1.7×, `mutex` 1.3×): a handoff hop still costs a virtual-thread
   unpark/park when the 5 µs spin misses, and every hop still sends one token
   to the drainer. Next: let the drainer learn handoffs lazily (batch the
   bookkeeping) and keep `workerpool`'s buffered-channel wakes on the handoff
   path (its producer rarely parks within the window). BUG-0038 is fixed.
2. **Value structs** (`structs` ~7×, `nbody` ~2×): struct values are
   `Flat` objects with `long[]`/`Object[]` regions; copies that escape
   analysis cannot scalar-replace (non-inlined calls, stores into slices)
   allocate twice. Candidates: callee-side copies only when the callee
   writes its parameter (the receiver analysis of §3.1 generalized to all
   struct parameters), and single-region layouts for all-scalar structs.
3. **Recursion and call overhead** (`fib` 1.5×, 2.7× gc+PGO): the
   remaining cost is Truffle's recursion inlining depth (§3.1 negative
   result: a global depth of 6 reaches gc at 5–7× code size) and the
   boxed `Object[]` calling convention of non-inlined calls. Next: a
   per-function inlining policy for small self-recursive bodies.
4. **Strings** (`striter` ~4×, `bytesops` 2.5×, `strconv` 1.8×): range over
   a string decodes rune by rune through a node; `for i := 0; i < len(g)`
   over a *global* is not strip-mined (globals are not provably invariant);
   `strings.Fields`/`Split` are Go loops over `Str` windows.
5. **Megamorphic dispatch** (`ifacemega` 7×): `IndirectCallNode` plus an
   argument array per call; gc's is an indirect call with register
   arguments. A per-site cache of more than 3 entries keyed by type id
   (8 types here) would turn it back into direct calls.
6. **Loop vectorization parity** (`matmul` 1.5×): the strip-mined inner
   loop is a Java loop in the `LoopNode` body and vectorizes less well than
   a poll-free `LoopNode` loop (0.58 vs 0.21 ms); needs an IGV look.
7. **Splitting in real programs**: on, with Truffle's default budget;
   `ifacesplit` shows the cascade. `sort` reaches each helper through one
   call site, so nothing splits there; an `fmt`/`io.Reader` benchmark is
   the next falsifier (§7).

## 6. Where beating gc is plausible, and where it is not

Already ahead (steady state): `mapint` (~0.5×: the JVM's allocation and
`GoMap` beat gc's swiss table on this mix), `bintrees` (~0.7×: TLAB
allocation and a generational GC beat gc's non-moving collector on
allocation-heavy trees), `loop` (0.2× gc+PGO: Graal vectorizes the strip-mined inner loop, gc does not vectorize) and `arraymat` (0.8×). `loop` is the first SPEC §2.6-style win (a JIT optimization gc lacks) and beats gc+PGO on its named benchmark; the others are runtime-library effects, real but not the SPEC §2.6 thesis.

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
  First numbers with the real `fmt` (lane fmt-path, 2026-11-20, one run
  each, shared host, `-Xmx2g -XX:ActiveProcessorCount=4`): hello world
  through `fmt.Println` 0.82 s wall, 242 MB RSS (the `println` hello
  world 0.58 s, 189 MB; gc < 0.01 s, 2 MB) — the bundle carries `reflect`,
  `os`, `internal/poll` and `syscall` (4.9 MB) and their initializers run;
  200,000 `fmt.Println("line", i)` to a file 1.1 s, to a pipe 0.7 s (gc
  0.06 s and 0.17 s). Before the executor asked `poll(2)` for readiness
  and ran ready transfers inline, the same loop took 24 s: every `write`
  released the scheduler slot and crossed to a platform thread.
- Native-image / AOT modes (P-D, P-F): no numbers yet; GG-0050's
  "performant without the JIT" is open.
- PGO-guided inlining parity, escape analysis across non-inlined calls,
  vectorization (Graal community has little; the Oracle GraalVM compiler
  in the toolchain may do more — not yet measured).
- JVM flags: `-XX:+UseCompactObjectHeaders` and the GC choice (G1 default)
  are untested on the suite; `engine.Mode=throughput` likewise.

## 8. Realistic programs (north star)

[test/bench/realistic/](../../test/bench/realistic/), `make
bench-realistic` (the suite runner with `--dir`, gc at `GOMAXPROCS=4`,
peak RSS and CPU per leg). Full tables, the GC matrix, goroutine scaling
and the first compilation evidence: [north-star.md](north-star.md) §4.

### 8.1 Lane perf-pe (2026-11-23)

Before: master `d61f219b`; after: `wt/perf/pe`; both trees built side by
side and run interleaved (base, new, base, new …) on the shared host, load
average 17–33; graalgo `-Xmx2g -XX:ActiveProcessorCount=4` under `nice -n
19`, gc at `GOMAXPROCS=4`. Each ratio is against the gc leg of the same
interleaved pair. **Long-running** is `--min-total 20` (at least 20 s of
rounds per leg, `BENCH_MIN_TOTAL`), 2 reps; **default** is the runner's
4 s, 3 reps. Steady = median of the last half of the rounds.

| Program | long ×gc before | long ×gc after | default ×gc before | default ×gc after | time to peak after (long) | compile failures before → after |
|---|---:|---:|---:|---:|---:|---|
| `jsonrt` | 11.1× | **4.4×** | 25.1× | 13.3× | 10.2 s | 1 → 0 |
| `regexscan` | 90.6× | **4.5×** | 92.3× | 4.4× | 1.6 s | 1 → 0 |
| `textproc` | 15.2× | **1.6×** | 17.4× | 6.1× | 3.1 s | 0 → 0 |
| `tmplrender` | 20.6× | **4.5×** | 24.6× | 29.7× | 6.3 s | 1 → 0 |
| `reqpipe` | 22.0× | **4.0×** | 40.2× | 12.5× | 5.1 s | 2 → 0 |
| `deflate` | 15.6× | **4.5×** | 26.5× | 10.0× | 4.0 s | 3 → 0 |
| geomean (steady) | 21.8× | **3.7×** | 31.9× | 10.5× | | 8 → 0 |
| geomean (best round) | 22.6× | 4.5× | 39.2× | 9.8× | | |

The micro-benchmark suite (§1, 3 interleaved reps): geomean 1.41× → 1.39×
steady, 1.48× → 1.47× best — no change beyond noise (`strconv` and
`workerpool` move ±30 % between reps of either tree). `allocmix` and
`goroutines` were not rerun (no compile failure, not on these paths).

What changed (truffle-design §15 rows *Loops* and *Pointers*):

1. **Zero failed compilations** (`make pe-report`,
   [pe-report.sh](../../test/bench/realistic/pe-report.sh)): `goto`
   dispatch with a constant program counter per label branch (regexp's
   backtracker, jsonwire's number scanner, flate's decoder); `GoType`
   layout computed once behind a boundary (PE inlined the recursion
   through defined types: "Too deep inlining" in flate); the strip-mined
   loop's `limit` node specialized in the interpreter (it ran only in
   compiled code and deopt-looped `text/template`). Gate: `make
   pe-check-m1a` runs [pe_std.sh](../../test/m1a/pe_std.sh) (~17 s, a
   planted failure must fail it).
2. **uintptr round trips** (JFR, 1 ms sampling): the handle table was
   O(live ranges) per 4,096 conversions (`textproc` round times grew with
   the heap); now amortized O(1). `unsafe.Pointer(uintptr(p) ± y)` is
   `unsafe.Add`; `abi.NoEscape`'s shape keeps the pointer in the local.
3. **Checked conversions**: `(*T)(unsafe.Pointer)` caches four identity
   locations inline; `coincides` has an identity front cache; `unsafe.String`
   over a byte backing store skips the leaf walks.

Remaining gaps, by evidence: (a) **warmup** — the default 4 s window is
10.5× because hot functions reach tier 2 only after 2–10 s
(`tmplrender` is no faster there); background compilation competes with
the shared host, and a 20 s steady state is 3.7×. (b) **calls that stay
calls**: in `jsonrt` at steady state ~45 % of samples are the Truffle call
sequence (`doInvoke`, `profiledPERoot`, entry polls) — json v2's arshalers
are func values (indirect, megamorphic) and the boxed `Object[]` calling
convention (truffle-design H8) costs every non-inlined call. (c) **reflect
over the pointer model**: `leafAt`/`gcOffOfLeaf`/`extent` walks behind
views and conversions are still ~15 % of `tmplrender`. (d) **footprint**:
peak RSS 1.1–1.5 GB at `-Xmx2g` against gc's 18 MB.

### 8.2 Lane perf-boundaries (2026-11-25): map keys and interface `==` without boundaries

WP-TR-AUDIT-4 ([truffle-audit.md](truffle-audit.md) F-6.3, F-7.1,
F-9.3). Before: master `e577971d`; after: `wt/perf/boundaries`; the same
tree measured before and after the change on the shared host (load 15–30),
`make bench-suite BENCH_ARGS="--reps 3 --pgo"` and `make bench-realistic
BENCH_ARGS="--reps 3"` (default 4 s window), graalgo `-Xmx2g
-XX:ActiveProcessorCount=4` under `nice -n 19`. Ratios are steady medians
against gc (and gc+PGO) of the same run.

| Benchmark | ×gc before | ×gc after | ×gc+PGO after | CPU ×gc before → after |
|---|---:|---:|---:|---:|
| `mapstr` | 1.63× | **0.54×** | 0.58× | 2.54 → 1.44 |
| `mapint` | 0.51× | **0.29×** | 0.28× | 1.61 → 1.65 |
| suite geomean (28) | 1.47× | **1.40×** | 1.47× (was 1.57×) | |
| `reqpipe` | 4.81× | 4.15× | | 7.55 → 7.93 |
| `textproc` | 1.53× | 1.56× | | 3.27 → 3.30 |
| realistic geomean (8) | 5.48× | 5.36× | | |

Everything else in the suite moves within the ±15 % the host gives a
single run (`ifacesplit` 1.37→1.72× and `nbody` 2.62→2.31× in opposite
directions, neither on these paths). `textproc` does not move: its map
work was already a small share (the remaining time is elsewhere; the
WP-TR-AUDIT-12 study, ADR-0021, measures its string search kernels).

What changed:

1. **Key-kind nodes** (`MapKeys`): every non-integer map get/put/delete,
   `v, ok := m[k]` (which was entirely behind a boundary, integer keys
   included), interface `==` and struct/array `==` run PE-visible nodes
   chosen by the static type — strings, boxed scalars, pointers, channels,
   struct/array types whose leaves are scalars/strings/pointers (`KeyPlan`:
   merged runs, exploded, ≤ 8 entries) and interfaces (limit-3 cache on the
   dynamic type, each entry the key node of that type); the generic
   `Values` code behind a boundary is the megamorphic and reflection path
   and computes the same hash (unit-tested pairwise, mutation-checked).
   Map range iteration (`Iter`, `next`) and `delete` lost their boundaries;
   only table growth keeps one.
2. **String hash cached in `Str`** (an `int` in the object's padding; the
   WP-TR-AUDIT-12 study measured re-hash 6.4 → 0.9 ns) and keyed per
   process.
3. **Seeded slots** (F-9.3): the slot is the top bits of `hash × m`, `m` a
   random odd multiplier per map (multiply-shift: a universal family, so
   keys found colliding in one map do not collide in another). The first
   attempt (xor-seed, fold, multiply, low bits) made `mapint` 1.5× slower
   than master: arithmetic key sequences lost Fibonacci hashing's spread
   and the linear probes clustered; multiply-shift keeps that spread
   (0.27–0.29× gc over 6 runs, each with its own multiplier). Range order
   is unchanged: insertion order from a random start (now a per-map LCG,
   no boundary).

PE evidence: the boundary call sites of every compiled root (the
`TruffleBoundary` speculations in `-Djdk.graal.Dump=Truffle:1` graphs) —
`mapstr`: `GoMap.find(Object)`, `GoMap.put(Object)`,
`MapGetOk.lookup` → only `GoMap.grow()`; `textproc`: `find`, `put`,
`Iter.<init>`, `Iter.next`, 3× `Values.ifaceEqual` → `grow()`; `reqpipe`:
2× `find`, `put`, 4× `ifaceEqual` → `grow()` (`compiler.InstrumentBoundaries`
is unusable here: it fails compilations with a `ClassCastException` in
the compiler). `TracePerformanceWarnings=all`: 0 on `mapstr`, `mapint`,
`structs`, `ifacepoly`, `ifacemega`, `textproc`, `reqpipe`, `jsonrt`; 0
failed compilations. A key-kind program run with `CompileImmediately`
(int → +string → +struct/float → megamorphic interface keys, NaN and ±0
struct keys, `err == sentinel`, an unhashable key's panic) matched gc
and compiled its hot root 4 times across the 4 phases (2 deopts, no
cycle). Open: reflect's `mapaccess_faststr`/`mapassign_faststr0`
intrinsics in `reqpipe` are still boundary calls; NaN keys hash alike
(gc randomizes them), so many NaN keys in one map probe linearly.

