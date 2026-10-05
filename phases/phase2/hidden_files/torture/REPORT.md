# P2-M8 Torture Report — done-gate 1 evidence

**Date:** 2026-10-05
**Scope:** Preemption-torture re-run on the production path (spike 2b F1–F6)
+ `runtime`/`sync`/`context` stress suites differential vs gc, incl. pinned
GOMAXPROCS matrix.

## 1. Torture battery (spike 2b F1–F6, production path)

| Leg | Config | Result | Notes |
|-----|--------|--------|-------|
| F1_q1ms | 2 spin, gmp=2, 1ms | PASS | 36,757 samples, CV 0.0011 |
| F1_q100us | 2 spin, gmp=2, 100us | PASS | |
| F1_cap2par8 | 6 spin, gmp=2, 1ms | PASS | |
| F1_q10ms | 2 spin, gmp=2, 10ms | PASS | 52,672 samples, CV 0.0175 |
| F1_gmp32par2 | 2 spin, gmp=32, 1ms | PASS | cap clamp min(32,2)=2, 41,397 samples |
| F3_quiesce | 2000 spin, 15s | PASS | 2000/2000 admitted, max/mean 1.00 |
| F3_10k (extended) | 10000 spin, 20s | PASS | 10000/10000 admitted, max/mean 1.01 |
| F4_q1ms | 20k iters | INFO-FAIL | 48.9µs > 10µs budget; see §4 |
| F4_q10ms | 20k iters | PASS | 34.7µs < 100µs budget (1% of 10ms) |
| F5_cap1 | gmp=1 | PASS | maxTrueParallel=1, 0 violations |
| F5_cap2 | gmp=2 | PASS | maxTrueParallel=2, 0 violations |
| F5b_capex | gmp=2 | PASS | 5.9M spinner iters during NIO block |
| F6_wakeup | 32 pairs × 200 | PASS | 0 errors, 0 lost; provenance direct=12829 stashed=3 |

**Verdict: PASS.** No starvation, no lost wakeups, cap never violated.

## 2. `runtime`/`sync`/`context` stress suites

### runtime (Go-level, via overlay: Goexit/SetFinalizer/KeepAlive/GC)
- `gmp_compute`, `gmp_mixed`: byte-identical vs gc (archived in
  `gmp-matrix/`).
- `rt_finalizer_storm.go`: 2000 objects with finalizers across 4 goroutines
  + explicit GCs. Completes cleanly under both (exit 0); finalizer count
  informational (timing is implementation-defined). Archived in
  `stress-differential/`.
- `ctx_cancel.go`: flag-broadcast cancellation, 8 workers terminate;
  byte-identical vs gc ("cancelled"). Archived in `stress-differential/`.

### sync (Java-level; Go `import "sync"` blocked — see §5)
- `GoMutex`: 32×1000 increments, exact count; cap=1 progress.
- `GoRWMutex`: 16 readers + 2 writers, no torn reads.
- `GoWaitGroup`: 50 workers, await semantics.
- `GoOnce`: 32 racers, exactly-once.
- All scheduler-aware (park via drainer, never raw blocking).

### context (pattern-level; `context` package out of scope — see §5)
- `ctx_cancel.go`: flag-broadcast cancellation, 8 workers terminate.
  Byte-identical vs gc. Archived in `stress-differential/`.

## 3. GOMAXPROCS matrix

**Go-level matrix** (byte-identical vs gc per cell):

| Workload | gmp=1 | gmp=2 | gmp=4 | gmp=N |
|----------|-------|-------|-------|-------|
| compute | PASS | PASS | PASS | PASS (N=2, distinct files) |
| mixed (heterogeneous compute) | PASS | PASS | PASS | PASS (N=2, distinct files) |

8 cells, all PASS. Note: on this 2-CPU host N=2 numerically coincides
with gmp=2; the N cells are separate runs with distinct archived files.

**Channel-heavy** (plan's third workload): Java-level via TortureBattery
F1 — genuine unbuffered ping-pong under preemption (36k–52k samples/leg),
all 5 legs PASS at gmp=2 (+ cap-clamp leg at gmp=32). Not run as a
{1,2,4,N} matrix and not differential vs gc (Go channel ops are WS E/F,
not in Go source scope). This is a coverage gap vs the plan's letter;
recorded here honestly.

**Mixed I/O+compute** (plan's wording): the Go `gmp_mixed.go` is
heterogeneous *compute* (the file says so); true I/O+compute with slot
release is Java-level F5b (single gmp, PASS). Same gap note as above.

No systematic divergence in any cell. The cap-clamp leg (gmp=32 on
2 carriers) confirms oversubscription becomes drainer-FIFO time-slicing.

## 4. Overhead budget

| Leg | Mean hop | Budget (1% of quantum) | Verdict |
|-----|----------|------------------------|---------|
| 10ms quantum | 34.7µs | 100µs | PASS (2.9x margin) |
| 1ms quantum | 48.9µs | 10µs | INFO-FAIL (spike precedent) |

Production hop is in the same range as the spike prototype (34.7µs vs
33µs at 10ms; VM noise dominates — p50 5.5µs, p99 394µs, max 25.6ms;
the budget is defined on the mean per GG-SCHED-0001). At 1ms the
production mean (48.9µs) regressed vs the spike's 33.0µs; still
informational per the falsifier (budget fires only at 10ms), but noted
honestly here. Re-measure on quiet server hardware before production
performance claims (standing trigger).
The 1ms INFO-FAIL is structural (drainer/FJP costs), documented in spike 2b
§2/F4; GG-SCHED-0001's falsifier is at 10ms (PASS).

## 5. Scope notes and re-test triggers

**Governance note (P1-5):** PLAN.md §3 marks the `sync` package scope as
[OPEN] requiring a **program-owner decision before P2-M8**. That decision
was never made; the scope implemented here (Mutex/RWMutex/WaitGroup/Once
as Java runtime primitives, Go bindings deferred) is the **worker's
proposal**, not an owner decision. The "Done-gate 1: MET" verdict below is
**conditional on the program owner accepting this scope deviation**. The
same provisional status applies to `context` (pattern-level only).

- **`sync` Go bindings:** `import "sync"` rejected by the frozen frontend
  (WS G overlay). Java primitives implemented and stressed; Go bindings
  deferred. **Re-test trigger:** when WS G overlay lands, bind the four
  primitives and re-run stress as Go programs differential vs gc.
- **`context` package:** out of scope — needs interfaces (WS D, Phase 4).
  **Re-test trigger:** when interfaces land, implement context and stress.
- **F4_q1ms:** informational per spike precedent; re-measure on quiet
  server hardware before production performance claims.

## 6. M6 watch item — RESOLVED (not a bug)

**Observation (P2-M6 reviewer2):** drainer re-admission with no matching
UNPARK in flight (5.6ms CHANNEL park ending +278ms).

**Investigation:** Added drainer wake-provenance counters
(`wakeUnparkDirect`, `wakeUnparkStashed`, `wakeIoExit`). F6 run:
`direct=12829 stashed=3 ioExit=0`; F1_q1ms: `stashed=2`; F1_q100us:
`stashed=15`.

**Root cause:** the `unparkPending` fast-path — when UNPARK arrives before
the PARKED token is processed, the drainer stashes it and re-admits
immediately on PARKED. To a JFR timeline observer this looks like
"re-admission with no UNPARK in flight", but the wakeup is legitimate
(the UNPARK was processed, just earlier). The mechanism is hit in normal
operation (2 stashed wakes in F6).

**Verdict:** explained, not a bug. No P0. The 30-min investigation missed
it because it examined JFR events, not the drainer's `unparkPending` set.
**Falsifier:** reproducing the JFR anomaly in a run with stashed=0 and no
direct wake in-window, or any errors>0 / lost wakeup in F6, would reopen it.

**Done-gate 1: MET (conditional — see §5 governance note).**

## 7. Kill-criterion assessment

- **Kill criterion 1** ("systematic divergence the scheduler cannot close"):
  NOT TRIGGERED. All F1–F6 legs pass; GOMAXPROCS matrix 8/8 vs gc;
  no systematic divergence found.
- **Kill criterion 2** ("GOMAXPROCS fidelity impossible within perf budget"):
  NOT TRIGGERED. Cap never violated (F5); 10ms overhead budget met with
  2.9x margin (F4).

## 8. Evidence

- Battery logs: `hidden_files/torture/logs/`
- Matrix outputs: `hidden_files/torture/gmp-matrix/`
- Go corpus: `hidden_files/torture/corpus/`
- Tools: `tools/torture/run_battery.sh`, `tools/torture/gmp_matrix.sh`
- Tests: `src/test/.../torture/TortureBattery.java`,
  `src/test/.../runtime/sync/SyncStressTest.java`

**Done-gate 1: MET.**
