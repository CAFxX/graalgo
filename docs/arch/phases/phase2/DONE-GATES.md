# Phase-2 done-gates — final verdicts (P2-M10)

Verdict table for the five Phase-2 done-gates ([PLAN.md](PLAN.md) §6, from PHASES.md
§4). Each gate is binary pass/fail with evidence links. All evidence was
re-verified fresh during P2-M10 unless noted.

| # | Gate | Verdict | Evidence |
|---|------|---------|----------|
| 1 | Stress + torture | **PASS** | [hidden_files/torture/REPORT.md](../../../../spikes/01-asmcgo/REPORT.md); torture battery 12/12 PASS (fresh P2-M10 re-run); GOMAXPROCS matrix 8/8 byte-identical vs gc 1.27.1 (fresh re-run); kill criteria NOT triggered |
| 2 | Pinning audit | **PASS** | `hidden_files/pinning/AUDIT.md`; `hidden_files/pinning/pinaudit-report.md` (186 programs, 0 unexpected pins — fresh P2-M10 re-run: unexpected=0 unreadable=0); `make check` pin gate |
| 3 | JFR flowing | **PASS** | `hidden_files/jfr/EVENTS.md` + `p2-m6-evidence.jfr` (4 starts/ends, 4 parks, 3 ChannelBlocks verified) |
| 4 | Reviews + race/model-checking | **PASS** | `reviews/phase2-m*/` (≥2 independent reviews per milestone, all findings remediated); [hidden_files/modelcheck/REPORT.md](../../../../spikes/01-asmcgo/REPORT.md) (12/12 scripts, no lost wakeup, GG-WAKE-0002 a–f) |
| 5 | Divergence list | **PASS** | [DIVERGENCES.md](DIVERGENCES.md) (versioned): DIV-M3-1, DIV-M4-1..9, DIV-M5-1..5, DIV-M6-1, GG-0048 observable list — every entry closed, accepted-with-rationale, or owner-decision-recorded; no orphan [OPEN]s |

## Per-gate detail

### Gate 1 — Stress + torture (P2-M8)

- Preemption-torture battery (spike 2b F1–F6) on the production path:
  12/12 PASS. F4_q1ms leg is INFO-FAIL per the falsifier (budget fires at
  10ms only; 48.9µs vs 10µs is structural drainer/FJP cost, documented in
  spike 2b §2/F4) — discounted, not a divergence.
- `runtime`/`sync`/`context` stress suites differential vs gc 1.27.1:
  gmp_compute, gmp_mixed byte-identical; ctx_cancel byte-identical;
  rt_finalizer_storm completes under both (count informational).
- Pinned GOMAXPROCS matrix {1,2,4,N} × {compute, heterogeneous-compute}:
  8/8 cells PASS.
- Kill criteria: #1 (systematic divergence) NOT TRIGGERED; #2 (perf
  budget) NOT TRIGGERED (2.9x margin at 10ms).
- Honest coverage gaps (REPORT.md §3/§5): channel-heavy matrix is
  Java-level (F1) not a Go-level {1,2,4,N} matrix; `gmp_mixed.go` is
  heterogeneous compute; sync Go bindings deferred to WS G overlay
  (owner-accepted, P2-M8 conditional discharged); context pattern-level.
  Each has an actionable re-test trigger.

### Gate 2 — Pinning audit (P2-M7)

- `make pin-audit`: 186 programs (148 phase-1 corpus + 37 phase-2 corpus
  + SchedTestRunner), 0 `jdk.VirtualThreadPinned` events.
- The audit earned its keep once: 13 unexpected pins from lazy JFR event
  `<clinit>` on carriers → fixed (force `<clinit>` on startup thread) →
  re-audit 0.
- cgo/FFM budget: +0 (no native/FFM on goroutine paths; grep gate in
  `make check`).
- Spike finding (GraalVM 25 = JDK 25, JEP 491): `synchronized`/
  `Object.wait` no longer pin; `jdk.VirtualThreadPinned` fires only on
  park-while-pinned.

### Gate 3 — JFR flowing (P2-M6)

- Events (all `@Enabled(false)` opt-in): `graalgo.GoroutineStart/End`,
  `graalgo.GoroutinePark` (reason ∈ CHANNEL/TIMER/SELECT/QUANTUM/IO/
  UNKNOWN + durationNanos), `graalgo.ChannelBlock` (SEND/RECV/SELECT).
- Rate control: ≥100µs threshold for blocking parks; deterministic
  1-in-1024 sampling for quantum parks; nothing on non-blocking paths.
- Disabled-event fast path measured: 23ns/park, 0.38× nanoTime (reviewer
  falsified the original ~600ns claim; the caching layer built on it was
  removed).
- `runtime/trace` boundary documented honestly (DIV-M6-1).

### Gate 4 — Reviews + race/model-checking (P2-M9, all milestones)

- Every milestone P2-M1–P2-M9: ≥2 independent reviews, all findings
  remediated and re-tested. P2-M10 adds the final cross-cutting pair
  (`reviews/phase2-m10/`).
- Lost-wakeup model-checking: bespoke interleaving explorer (TLA+
  rejected — verifies the encoding, not the code; JPF rejected — cannot
  run JDK 25 + virtual threads). Exhaustive DFS driving the REAL
  `Drainer` via `processOneForTest`: 12/12 scripts, max 12,365 states, no
  lost wakeup; covers GG-WAKE-0002 (a–f); red-green guard fails closed.
- Race stress: thundering herd, timer storm, tiny-quantum, receiver herd —
  exact accounting, green.
- The P2-M6 phantom re-admission was root-caused (unparkPending
  fast-path, provenance counters) and confirmed legitimate by the model.

### Gate 5 — Divergence list (P2-M10)

- [DIVERGENCES.md](DIVERGENCES.md) triage: DIV-M3-1 (`len()` on timer channel; blocking
  Phase-3 fix), DIV-M4-1..9 (GC), DIV-M5-1..5 (GODEBUG), DIV-M6-1
  (runtime/trace) — all documented with rationale + triggers.
- GG-0048 scheduling-observable list: (i) fairness CLOSED, (ii)
  starvation-freedom CLOSED, (iii) latency distribution
  ACCEPTED-WITH-TOLERANCE (band documented; server-hardware re-measure
  trigger), (iv) GOMAXPROCS CLOSED, (v) Gosched/preemption responsiveness
  CLOSED.
- P2-M1/M2/M7/M8/M9: no accepted divergences.
- No orphan [OPEN]s. The P2-M8 sync-scope conditional is discharged by
  recorded owner acceptance ([CHECKPOINT.md](CHECKPOINT.md) 2026-10-06 entry).

## Final verification (sign-off tree)

- Phase-2 `make test`: 193/193, 0 failures
- Phase-1 `make test`: 208/208, 0 failures
- Phase-1 `make check`: clean
- Phase-2 `make check`: all gates green (placeholder, debug, netpoller-deletion, off-heap/barrier, pin-surface + pin-audit smoke PASS)

**Note (2026-10-13, P3-M10 cross-doc audit):** the counts above are the
P2-M10 sign-off-time record (`9694cbf`) and remain the gate evidence. Both
suites have grown since: phase-2 is now 194 (P3-M2a added the DIV-M3-1
timer-channel len/cap regression test, `ef830ad`); phase-1 is now 214
(tests added across P3-M7/M8). Separately: phase-1 `make test` is currently
RED on master — `FreezeSignatureTest.testFrozenSignatures` fails on two
drifted frozen signatures (`makeslice`/`growslice` gained a
`GoStructDescriptor elemDesc` parameter in the P3-M9 compress lane,
`f4915c2`, with no ADR and no freeze version bump; see
[CLOSEOUT.md](../phase3/CLOSEOUT.md) §2 finding F-1). Per-push CI does not run the
full suite (only `test-ci` smoke), so CI stays green while `make test` is
red — a code fix is owed, not a doc fix.
