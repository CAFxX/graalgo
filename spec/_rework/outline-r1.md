# Outline R1 — new §3.5 Concurrency runtime (option C)

**Goal:** rewrite §3.5 (currently "1:1 vthreads + back-edge `Thread.yield()`", falsifier = spike 2)
around the spike-2b PASS verdict: goroutine = vthread + scheduler token; Loom FJP demoted to
untrusted pool; user-space quantum scheduler. Preserve unaffected material (channels, select,
defer, panic, Goexit, panic(nil), netpoller deletion, -Xss limit). Evidence labels on every claim.

## Proposed structure

1. **3.5.1 Execution vehicle: goroutine = virtual thread + scheduler token**
   - `go f(args)` ≡ `Thread.ofVirtual().start(...)`; goroutine-local state in vthread-scoped
     holders `[DECIDED]`.
   - Loom's FJP is demoted to an **untrusted thread pool**: no scheduling discipline assumed —
     spike 2 falsified FJP fairness `[VERIFIED]` (0 samples vs gc 200,000; scoped to JDK 25.0.4,
     2-vCPU VM). Record the kill of the yield-to-FJP design here.
   - Stack story: heap-allocated grow/shrink vthread stacks (JEP 444 `[VERIFIED]`); honest
     `-Xss` limit stays `[DECIDED]`.

2. **3.5.2 Quantum-based preemption via park, not yield**
   - `Safepoint.poll()` at loop back-edges and function entries in the Truffle AST `[DECIDED]`;
     on quantum expiry the vthread parks **indefinitely** via `LockSupport.park()` and enqueues
     a resume token — never re-enters FJP queues unsolicited `[DECIDED]`.
   - Default quantum **10ms** `[DECIDED]`: the F4 scoped fix (prototype: 1ms → mean 26.8µs/hop =
     2.7% over the 1% budget; p50 5µs = 0.5% under; 10ms → 0.27% mean `[VERIFIED]`, prototype
     scope). Tradeoff stated: coarser preemption granularity; 10ms matches Go's sysmon
     granularity `[HIST]`; `runtime.Gosched()` and explicit safepoint points still provide
     fine-grained handoff.

3. **3.5.3 Token queues and the drainer**
   - Three lanes: FIFO `runFifo` (quanta tokens); priority `wakeFifo` (woken/channel-ready,
     the runnext analog); priority `initFifo` (new goroutines — added after spike 2b §3.2
     caught INIT starvation: 2,768/10,000 never scheduled before the fix `[VERIFIED]`) `[DECIDED]`.
   - Drainer: a single platform thread, blocking take → admit → `LockSupport.unpark(vthread)`
     `[DECIDED]`.
   - Self-pacing property: tokens arrive only as vthreads park, so in-flight unparks ≈ carrier
     count; the FJP never floods `[DECIDED]` (design property; observed drainer util 1.34% at
     10k goroutines `[VERIFIED]`, prototype scope).

4. **3.5.4 In-flight cap = GOMAXPROCS**
   - Hard assert in the drainer, never tripped in spike 2b (`maxTrueParallel` never exceeded
     cap at 1 and 2) `[VERIFIED]`, prototype scope; production must preserve the assert `[DECIDED]`.
   - Default cap = available processors, matching Go's default NumCPU `[VERIFIED]` on the mapping.

5. **3.5.5 Park/unpark safety**
   - Per-vthread `armed` ticket + generation counter; `acked` set **before** unpark (reversing
     loses wakeups) `[DECIDED]`.
   - Lost-wakeup invariant: admissions == parks at quiescence; stress: 50,558/50,558, zero lost
     `[VERIFIED]` (prototype scope). Residual: tested interleavings only — formal verification /
     model-checker re-test before production `[OPEN]` (carried as risk, not go/no-go? — check:
     it's a Phase-2 done-gate, not a Phase-0 go/no-go).

6. **3.5.6 Scheduler-aware blocking (the F5 limitation)**
   - Java blocking that bypasses the scheduler (e.g. `CountDownLatch.await()`) still counts
     against the in-flight cap → transient under-utilization; cap=1 deadlocked the O5 barrier in
     the prototype `[VERIFIED]`.
   - **SHALL:** all blocking in the runtime/stdlib adaptation layer goes through scheduler-aware
     primitives (channel park, timer park, sleep, select park, scheduler-token sleep) `[DECIDED]`.
   - Loom-unmounted blocking I/O (NIO): scheduler-transparent — frees the carrier but counts
     in-flight; documented bounded deviation (no starvation risk, throughput-only) `[DECIDED]`,
     with a Phase-2 measurement trigger `[OPEN]` (measurement, not decision).

7. **3.5.7 cgo / pinning under option C**
   - A vthread in a native/FFM call pins its carrier (JEP 491 `[VERIFIED]`); with the FJP as a
     dumb pool this transiently **reduces pool capacity** rather than breaking fairness `[DECIDED]`
     (mechanism implication; measured behavior `[UNVERIFIED]` until the pinning matrix).
   - cgo economics stay budgeted per GG-0057 `[DECIDED]`; the `jdk.VirtualThreadPinned` audit
     stays a Phase-2 done-gate `[DECIDED]`. Verified-vs-assumed table row.

8. **3.5.8 Netpoller — deleted** (unchanged prose from current §3.5; GG-0018).

9. **3.5.9 Channels, select, defer, panic, Goexit** (unchanged prose; GG-0019).

10. **3.5.10 Residual risks carried, not hidden**
    - R-A: F4 overhead — mean 26.8µs/hop over the 1% budget at 1ms quanta (p50 5µs under);
      scoped fix = 10ms quanta → 0.27%; tradeoff = coarser preemption (see 3.5.2). Re-test trigger:
      server hardware.
    - R-B: 38µs cold-drainer wakeup unexplained on the 2-vCPU VM (host-specific per analysis);
      impacts the overhead mean only, not correctness. Re-test trigger: server hardware.
    - R-C: 13× absolute-rate gap vs stock Go is prototype per-hop cost (~40µs vs Go's ~1µs futex),
      not design — but **no production performance claim** until the production-path
      re-measurement trigger fires (Phase 2, production scheduler).

11. **Falsifier (closes §3.5):** Phase-2 preemption-torture + pinning matrix re-run against
    this design; kill trigger = systematic divergence the scheduler cannot close, or a residual
    risk becoming a mechanism blocker.

## Cross-reference note
The scheduler SHALL-requirements (quantum/park, drainer admission, in-flight cap, wakeup
safety, fairness) are being minted as the GG-SCHED-*/GG-WAKE-*/GG-FAIR-* series by the
requirements track (spike 2b §6.2); §3.5 states the decided architecture those IDs will
encode. The GG-PREEMPT-* series is being revised to remove FJP-scheduler assumptions.

## Self-review questions for R2
1. Does 3.5.2's "never re-enters FJP queues unsolicited" overstate — parked vthreads stay
   mounted how? Clarify: parked vthread unmounts (Loom), carrier returns to pool; the *thread*
   is not in any FJP *work queue*; the *token* is in our queue. Fix wording in R2.
2. Should "resume token" vs "scheduler token" be one term? Unify: **scheduler token** = the
   resume token carried by the vthread; the queue holds resume tokens. Pick one name.
3. 3.5.6 NIO wording: "Loom-unmounted blocking I/O (NIO)" — NIO blocking *does* unmount on
   vthreads. Keep "scheduler-transparent": the scheduler neither knows nor needs to know;
   carrier freed, slot still counted. OK.
4. F4 numbers: mean 26.8µs "over the 1% budget" — 26.8µs/1ms = 2.68% ≈ 2.7%, yes over.
   p50 5µs/1ms = 0.5% under. 10ms: 26.8/10000 = 0.268% ≈ 0.27%. Consistent.
5. Label check: is `initFifo` priority `[DECIDED]` or `[VERIFIED]`? The mechanism is decided;
   the fix's effect (never_scheduled=0) is verified in prototype. Use `[DECIDED]` for the
   design, `[VERIFIED]` for the measurement sentence.
6. The "armed ticket" detail — keep at mechanism-name level (no Java code in spec), one line.
