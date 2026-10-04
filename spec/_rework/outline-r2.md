# Outline R2 — new §3.5 Concurrency runtime (option C)

Incorporates R1 self-review: unified token terminology, unmount semantics clarified,
0.27% marked computed ([ESTIMATE], not measured), plus the exact §7.2/§7.4/§8.2/§10
edit targets so writing is mechanical.

## Terminology (used consistently in §3.5)
- **Scheduler token** — the per-goroutine token pairing a virtual thread to the scheduler.
  On quantum expiry the vthread parks indefinitely and *offers its scheduler token* to the
  token queue; the drainer *admits* a token by unparking the owning vthread. (No separate
  "resume token" term.)
- **Park** = `LockSupport.park()` indefinitely; **unpark** = drainer admission.

## §3.5 structure (final)

**3.5.1 Execution vehicle: goroutine = virtual thread + scheduler token** `[DECIDED]`
- `go f(args)` ≡ `Thread.ofVirtual().start(...)`; defer chain, panic context, other
  goroutine-local state in vthread-scoped holders.
- Loom's FJP is demoted to an **untrusted thread pool**: no scheduling discipline is assumed
  from it. Spike 2 (1:1 vthreads *yielding* back to the FJP) was **KILLED**: preemption torture
  gave 0 latency samples vs 200,000 for stock Go — systematic starvation the yields could not
  close `[VERIFIED]` (scoped: JDK 25.0.4, 2-vCPU VM, 1ms/100µs quanta; the kill indicts the
  yield-to-FJP design, not 1:1 vthreads as the execution vehicle).
- Mechanism difference that matters: a parked vthread unmounts and its carrier returns to
  the pool; the parked thread is held by the *scheduler's* token queue, never by an FJP work
  queue — Loom never sees it as schedulable work, so the FJP cannot starve it `[DECIDED]`.
- Stacks: heap-allocated, grow/shrink (JEP 444 `[VERIFIED]`) = Go's growable-stack semantics
  from the JVM. Honest limit: max depth bounded by `-Xss`; pathological deep recursion that
  gc survives may `StackOverflowError` here (quantified by the §8.2 stack-depth experiment)
  `[DECIDED]` (unchanged).

**3.5.2 Quantum-based preemption via park** `[DECIDED]`
- `Safepoint.poll()` at loop back-edges and function entries in the Truffle AST (mirroring
  gc's own preemption checks); on quantum expiry the vthread parks indefinitely and offers
  its scheduler token. Back-edge *yields* are gone — parking is the preemption primitive.
- Default quantum **10ms** `[DECIDED]` — the F4 scoped fix: prototype measured mean
  26.8µs/hop = 2.7% of a 1ms quantum, over the 1% budget (p50 5µs = 0.5%, under); at 10ms
  the measured hop cost projects to 0.27% mean, comfortably under budget `[VERIFIED]` for the
  measured hop, `[ESTIMATE]` for the 10ms projection (computed 26.8µs/10ms — a model, not a
  measurement; re-measured on server hardware in Phase 2).
- Tradeoff stated explicitly: coarser preemption granularity at 10ms; 10ms matches Go's
  sysmon granularity `[HIST]`; fine-grained handoff still available via `runtime.Gosched()`
  and explicit safepoint polls.

**3.5.3 Token queues and the drainer** `[DECIDED]`
- Three lanes, admission order: priority `initFifo` (new goroutines), priority `wakeFifo`
  (woken/channel-ready — the runnext analog), FIFO `runFifo` (quanta tokens).
- `initFifo` exists because spike 2b §3.2 caught INIT starvation: unbounded FIFO growth let
  RESUME tokens flood past INIT tokens (2,768/10,000 never scheduled before the fix;
  `never_scheduled=0`, fairness max/mean 1.01 after) `[VERIFIED]` (prototype scope).
- Drainer: a single platform thread, blocking take → admit → `LockSupport.unpark(vthread)`.
- Self-pacing: tokens arrive only as vthreads park, so in-flight unparks ≈ carrier count and
  the FJP never floods; observed drainer utilization 1.34% at 10,000 goroutines `[VERIFIED]`
  (prototype scope) — the drainer is not the bottleneck.

**3.5.4 In-flight cap = GOMAXPROCS** `[DECIDED]`
- Enforced as a hard assert in the drainer; spike 2b `maxTrueParallel` never exceeded the cap
  (caps 1 and 2, incl. mixed I/O+compute) `[VERIFIED]` (prototype scope). Production preserves
  the assert — it is the mechanism, not a debug aid.
- Default cap = available processors, matching Go's default NumCPU with no work `[VERIFIED]`
  on the default mapping.

**3.5.5 Park/unpark safety** `[DECIDED]`
- Per-vthread `armed` ticket + generation counter; `acked` set **before** unpark (reversing
  the order loses wakeups).
- Lost-wakeup invariant: admissions == parks at quiescence. Stress result: 50,558 parks /
  50,558 admissions, 40,617/40,617 wakeups won, zero lost `[VERIFIED]` (prototype scope).
- Residual, stated: tested interleavings only; no formal verification — model-checker
  re-test is a Phase-2 done-gate `[OPEN]` (measurement gate, not a go/no-go item).

**3.5.6 Scheduler-aware blocking (the F5 limitation)** `[DECIDED]`
- Java blocking that bypasses the scheduler (e.g. `CountDownLatch.await()`) still counts
  against the in-flight cap → transient under-utilization; with cap=1 it deadlocked the O5
  test barrier in the prototype `[VERIFIED]`.
- **SHALL:** all blocking in the runtime/stdlib adaptation layer goes through scheduler-aware
  primitives (channel park, timer park, sleep, select park, scheduler-token sleep).
- Loom-unmounted blocking I/O (NIO) is **scheduler-transparent**: it frees the carrier but
  still counts in-flight — a documented, bounded deviation (no starvation risk; throughput
  only), with a Phase-2 measurement trigger `[OPEN]` (measurement).

**3.5.7 cgo / pinning under option C** `[DECIDED]`
- A vthread in a native/FFM call pins its carrier (JEP 491 `[VERIFIED]`); with the FJP as a
  dumb pool this transiently **reduces pool capacity** rather than breaking scheduling
  fairness — the mechanism implication is `[DECIDED]`; measured pool-capacity behavior is
  `[UNVERIFIED]` until the Phase-2 pinning matrix.
- cgo economics stay budgeted per GG-0057 (separate platform-thread pool or budgeted +k
  carriers, as before). The `jdk.VirtualThreadPinned` audit stays a Phase-2 done-gate.
- Verified vs assumed stated in the falsifier, not hidden.

**3.5.8 Netpoller — deleted** (GG-0018; prose carried over unchanged.)

**3.5.9 Channels, select, defer, panic, Goexit** (GG-0019; prose carried over unchanged —
"parked virtual threads" in the wait-queues now means scheduler-parked vthreads per 3.5.5.)

**3.5.10 Residual risks, carried explicitly**
- R-A overhead: mean 26.8µs/hop over the 1% budget at 1ms quanta (p50 5µs under); scoped fix
  10ms → projected 0.27%. Both numbers kept; tradeoff kept. Re-test trigger: server hardware.
- R-B cold-drainer wakeup: 38µs p50 when the drainer was parked (vs 4.3µs minimal repro, 5µs
  hot p50); unexplained on the 2-vCPU VM, judged host-specific; correctness unaffected —
  impacts the overhead mean only. Re-test trigger: server hardware.
- R-C absolute rate: 15,603 samples/5s vs stock Go's 200,000 (13×); RTT p50 37µs vs <1µs —
  prototype per-hop cost (~40µs vs Go's ~1µs futex), not a design flaw; **no production
  performance claim** until the production-path re-measurement trigger fires (Phase 2).

**§3.5 falsifier:** Phase-2 preemption-torture suite + pinning matrix re-run against this
design. Kill trigger: systematic divergence the user-space scheduler cannot close, or a
residual risk (R-A/R-B/R-C) hardening into a mechanism blocker. Supersedes spike 2's
yield-based falsifier.

**Requirements-track note** (end of §3.5): the scheduler SHALLs are being minted as the
GG-SCHED-*/GG-WAKE-*/GG-FAIR-* series; GG-PREEMPT-* is being revised to remove
FJP-scheduler assumptions. §3.5 states the decided architecture those IDs will encode.

## Other edits (exact targets)

**§7.2** — keep the five observables verbatim; change: "the yields are semantically safe" →
"the scheduler's parks are semantically safe"; falsifier → "the preemption-torture suite
re-run against the spike-2b design (Phase 2) showing systematic divergence the user-space
scheduler cannot close → kill trigger."

**§7.4** — falsifier: "spike 2's differential run" → "the Phase-2 differential run of the
`net`/`os/signal` suites against the §3.5 scheduler". Keep "adapt instead of delete (scoped
workaround, not a program kill)."

**§8.2 row 2** — retired-risk record: claim "User-space scheduler over 1:1 vthreads delivers
M:N scheduling observables (option C; spike 2 KILLED, spike 2b PASS)"; falsifying experiment
"spike-2b battery (F1–F6) + Phase-2 preemption-torture re-run at server scale"; kill trigger
"systematic divergence the scheduler cannot close; residuals R-A/R-B/R-C hardening into
mechanism blockers → re-scope". Retired residual risks recorded in the row.

**§8.2 row 10** — claim "Park/quantum overhead stays under the 1% budget (10ms default)";
falsifying experiment "park/resume-hop microbenchmark at 10ms quanta on server hardware";
kill trigger "mean over 1% with no scoped quantum fix, or cold-drainer p99 dominating the
mean beyond the scoped fix."

**§8.2 row 11** — claim "cgo/pinning transiently consumes FJP pool capacity without breaking
fairness (GG-0057)"; falsifying experiment "cgo-heavy workload under (c) with JFR
`jdk.VirtualThreadPinned` events + pool-capacity accounting"; kill trigger "pinning breaks
fairness or transient capacity loss unbounded → the dumb-pool model dies; `jdk.VirtualThreadPinned`
audit is a Phase-2 done-gate."

**§10.1** — replace the §3.5 row: "Goroutine = vthread + scheduler token; user-space
quantum scheduler (option C); netpoller deleted; panic/defer model | this spec §3.5 (spike
2b PASS) | [DECIDED]". Add: "Spike 2 (1:1 on FJP, yields): KILLED — 0 samples, systematic
starvation | spikes/02-loom | [VERIFIED]"; "Spike 2b (option C): PASS — 15,603 vs 0 samples,
cap assert never tripped, 50,558/50,558 wakeups, scoped fixes (initFifo, 10ms quantum) |
spikes/02b-userspace-scheduler | [VERIFIED]"; "This change: §3.5/§7.2/§7.4/§8.2/§10 rewritten
around option C | SPEC rewrite 2026-10-03 | [DECIDED]".

**§10.2** — add the four Appendix-B verdict-scope rows: (1) spike-2 kill scoped to JDK
25.0.4 / 2-vCPU / 1ms-100µs quanta — kill indicts yield-to-FJP, not 1:1 vthreads;
(2) 2b fairness scoped to 2-vCPU VM, drainer, 1ms/100µs — re-test: different CPU count/JDK;
(3) 2b overhead scoped to 2-vCPU high-wakeup-latency VM — re-test: server hardware;
(4) lost-wakeup freedom scoped to tested interleavings, no formal verification — re-test:
model checker before production; GOMAXPROCS cap is a prototype hard assert — production
must preserve.

## R2 review (clean)
- Every target-content bullet from the task is placed; labels assigned per claim.
- 0.27% is `[ESTIMATE]` (computed), 26.8µs/5µs/50,558/15,603 `[VERIFIED]` (measured).
- Unverified-until-Phase-2 items marked `[OPEN]` (measurement gates) or `[UNVERIFIED]`
  (pinning behavior) — never `[DECIDED]`.
- §2.7 needs no change (neutral "the goroutine mapping, §3.5" reference); verified no other
  SPEC sentence assumes the killed design (grep: yield refs only at 298/299/326/730/732/856/864/913,
  all in the edit set).
- PHASES.md/FEASIBILITY.md left untouched: FEASIBILITY is the historical study record
  (SPEC §1.1: "this spec is the normative record ... replacing the study's recommendations");
  consistency is maintained via the §10 ledger, the established pattern. PHASES.md Phase-2
  re-estimate is the coordinator's separate track (per memory, already on its list). Flag both
  in the final report.
