# Phase 2 plan — Runtime: goroutines, channels, GC interop

**Status:** draft for parent review. For implementation dispatch only after approval.
**Normative:** SPEC.md §3.5 (ACCEPTED 2026-10-03), PHASES.md §4, ADR-0001.
70 GG IDs locked; Phase-2-owned IDs activate per-workstream below.
**Inputs:** spike 2b REPORT (`spikes/02b-userspace-scheduler/REPORT.md`, PASS 2026-10-03);
Phase-1 interface freeze (`phases/phase1/INTERFACE_FREEZE.md`);
M9 polymorphism measurements (`phases/phase1/hidden_files/m9_polymorphism/SUMMARY.md`).

## 1. Objective and scope

Faithful concurrency runtime on the user-space scheduler (option C, spike 2b
PASS). Goroutine = virtual thread + scheduler token; quantum-based preemption
via `Safepoint.poll()` (indefinite park + resume-token enqueue on quantum
expiry, 10 ms default); platform-thread drainer (FIFO `runFifo` + priority
`wakeFifo`/`initFifo`); in-flight cap = `GOMAXPROCS` (hard assert); all
blocking through scheduler-aware primitives (channel/timer/sleep/select park;
the F5 deviation class — Java blocking that bypasses the scheduler —
documented and measured); channels + select; netpoller deletion → blocking
NIO on virtual threads; timers; GC interop (exact roots, write barriers free
from JVM); `GODEBUG` surface triage; JFR goroutine/channel events.

In scope: the Phase-2 GG IDs below (SCHED/WAKE/FAIR families owned here;
GG-0017 core slice, GG-0018, GG-0048, GG-0055, GG-0057, GG-0008 JFR slice).
Out of scope: stdlib bring-up beyond what the stress suites need (Phase 3),
`-toolexec` driver wiring (Phase 4), AOT/JIT modes (Phase 5). The Phase-1
freeze doc's scheduler-glue signatures (D7) are the contract — Phase 2 builds
against them, and any change requires the freeze change policy (ADR + version
bump), not a quiet edit.

**What Phase 2 builds on (Phase-1 deliverables, not to be re-derived):**
- `phases/phase1/INTERFACE_FREEZE.md` — frozen scheduler-glue signatures
  (scheduler token type, park/unpark entry points, channel/timer/timer-wheel
  intrinsic signatures marked stub-vs-real per D7). Phase 2 implements the
  stubs; the signatures are the contract.
- `Safepoint.poll()` seam already in the AST at function entries and loop
  back-edges (WS D/E, D8); Phase-1 impl is a no-op. Phase 2 gives it
  semantics (GG-SCHED-0001).
- 208-test Phase-1 suite + differential corpus: the single-threaded
  regression base. The scheduler must not change single-threaded semantics —
  the Phase-1 corpus stays green throughout (GOMAXPROCS=1 must be
  observationally identical to Phase 1).
- Spike 2b prototype (`spikes/02b-userspace-scheduler/`): reference
  implementation of the design, not a code donor — production re-implements
  against the frozen signatures; the spike's F1–F6 battery is the falsifying
  experiment to reproduce on the production path (P2-M8).
- M9 polymorphism numbers (170 sites: 99.4% monomorphic, 0.6% bimorphic,
  0% megamorphic): interface dispatch does not exist yet, so megamorphism at
  interface sites is unmeasured — the scheduler's PIC strategy assumes the
  Phase-1 distribution; re-measure when interfaces land (Phase 4 input).

## 2. Work breakdown (workstream → GG IDs → done-gate)

| WS | Workstream | GG IDs | Gate |
|---|---|---|---|
| A | Scheduler core: goroutine = vthread + token; quantum park on `Safepoint.poll()` expiry (10 ms default, indefinite `LockSupport.park()` + resume token); single platform-thread drainer (blocking take, lane order per GG-WAKE-0003, non-terminating, swallow-and-continue); hard in-flight cap = `min(GOMAXPROCS, carrier parallelism)`; per-vthread armed ticket + generation counter (ack-before-unpark) | GG-SCHED-0001, GG-SCHED-0002, GG-SCHED-0003, GG-WAKE-0001, GG-0017 (core slice) | 1 (part), 4 (part) |
| B | Channels + select: scheduler-aware send/recv park (buffered + unbuffered), close semantics (waking parked senders via aborted path), select with fairness across cases, select in-flight stale-signal handling | GG-0017 (chan slice) | 1 |
| C | Timers/sleep + netpoller deletion: timer heap, `sleep`; blocking NIO with slot-releasing `ioEnter`/`ioExit` (wakeFifo re-acquire, unpaired-ioEnter fail-fast assert); netpoller deleted per GG-0018 | GG-0018, GG-SCHED-0004 | 1 |
| D | GC interop: exact root enumeration (goroutine stacks, globals, channel/timer structures) via the JVM GC; write-barrier integrity — JVM barriers cover all Go stores, no Truffle-level barrier (ADR-0002 D3); Go-observable GC surface (SetFinalizer/KeepAlive/GC/weak/memstats) on JVM primitives; barrier + root-correctness differential vs gc | [OPEN: heap layout — see §3; closed by ADR-0002] | 1 (part) |
| E | `GODEBUG` surface triage: versioned registry (emulated / no-op / unsupported-with-justification per knob); unknown knobs deny-with-error; `godebug-diff` tooling | GG-0055 | 5 |
| F | JFR events: goroutine lifecycle + channel events flowing; `jdk.VirtualThreadPinned` consumption for the pinning audit | GG-0008 (JFR slice) | 3 |
| G | Pinning audit: `jdk.VirtualThreadPinned` shows zero unexpected pins on the conformance corpus; cgo/FFM + blocking file I/O off the carrier pool or in the budgeted +k carriers | GG-0057 | 2 |
| H | Preemption-torture re-run on the production path: spike 2b F1–F6 reproduced against production (O1 torture, O2–O5 correctness, quiescence, overhead budget, GOMAXPROCS fidelity incl. pinned matrix + mixed I/O+compute, cap-exhaustion); `runtime`/`sync`/`context` stress suites differential vs gc | GG-0048, GG-FAIR-0001/0002/0003, GG-SCHED-0001/0003 (re-verified) | 1 |
| I | Lost-wakeup model-checking + race-detector-equivalent stress: formal protocol-level model scoped to GG-WAKE-0002's six interleavings (a–f); JVM race tools on the runtime (not the guest) | GG-WAKE-0002, GG-WAKE-0003 | 4 |
| J | Scheduling-observable divergence list + closeout: GG-0048 observables (fairness, starvation-freedom, latency distribution, GOMAXPROCS behavior) — every entry closed or explicitly accepted with rationale; final reviews; cross-doc audit | GG-0048, GG-0007, GG-0042 | 4, 5 |

Activation policy (carried from Phase 1): GG ID →active per-workstream, exactly
when its first real test lands with a `verifies: GG-XXXX` annotation; never
before.

## 3. Architecture decisions (recorded)

- **P2-D1 [DECIDED, from Phase 0]: option C.** 1:1 virtual threads as the
  execution vehicle; Loom's ForkJoinPool demoted to a dumb thread pool;
  scheduling discipline in user space. The 1:1-on-FJP design was KILLED in
  Phase 0 (spike 2, 0 latency samples vs 200,000 for stock Go — systematic
  starvation; root causes in SPEC §3.5). Re-test triggers for the killed
  design are recorded in SPEC §3.5 and do not apply to option C.
- **P2-D2 [DECIDED, spike 2b]: indefinite park, not yield.** On quantum expiry
  the vthread parks indefinitely via `LockSupport.park()` and enqueues a
  resume token; the drainer explicitly admits. This is what eliminates the
  starvation pathology (spike 2b F1: 7k–14k samples vs 0 for the killed
  design).
- **P2-D3 [DECIDED, spike 2b]: three-lane token queue.** `runFifo` (spinner
  quanta), `wakeFifo` (woken/channel-ready — Go runnext analog), `initFifo`
  (new goroutines). FIFO alone starved 2,643/10,000 new goroutines behind
  RESUME flood (spike 2b §3.2); lane order per GG-WAKE-0003.
- **P2-D4 [DECIDED, spike 2b]: scheduler-aware blocking only.** Raw Java
  blocking that bypasses the scheduler (e.g. `CountDownLatch.await()`) is
  banned in the adaptation layer — it counts against the in-flight cap and
  deadlocked O5 at cap=1 (spike 2b §3.1). Blocking I/O uses the slot-releasing
  NIO wrapper (`ioEnter` releases the slot; `ioExit` re-acquires via
  `wakeFifo`); the pre-rework scheduler-transparent NIO carve-out is deleted
  (CapExhaustion falsifier red-green validated, spike 2b §3.5).
- **P2-D5 [DECIDED, spike 2b]: 10 ms default quantum.** The 1% overhead budget
  (GG-FAIR-0002) fires at 1 ms (mean ~30 µs = 3.3%) and passes at 10 ms
  (0.30%, 3x margin) — measured, not projected. Re-measure on quiet server
  hardware before any production performance claim.
- **P2-D6 [DECIDED, spike 2b]: effective cap = `min(GOMAXPROCS, carrier
  parallelism)`.** Oversubscription becomes drainer-FIFO time-slicing, never
  untrusted FJP queue discipline. Hard assert in the drainer (GG-SCHED-0003).
- **P2-D7 [DECIDED, spike 2b]: lost-wakeup discipline.** Per-vthread armed
  ticket + generation counter; `acked` set before `unpark` (reversing loses
  wakeups). Invariant: admissions == parks at quiescence (spike 2b F6:
  50,558/50,558, zero lost).
- **[OPEN] GC interop heap layout.** Where the Go heap lives relative to the
  JVM heap (all-Go-objects-in-JVM-heap vs segregated region), how exact roots
  are enumerated from vthread stacks (stack-map format, safepoint cooperation
  with the quantum-park mechanism), and where the Truffle-level write barrier
  is inserted. This is the largest design unknown in Phase 2 — P2-M4 begins
  with a design spike and ADR, not with code.
- **[OPEN] `sync` package scope for gate 1.** Gate 1 names `runtime`/`sync`/
  `context` stress suites; which `sync` primitives (Mutex, RWMutex, WaitGroup,
  Cond, Pool, Once, Map?) are in-scope vs Phase-3 is undecided. Propose:
  Mutex/RWMutex/WaitGroup/Once in Phase 2 (scheduler-adjacent), the rest in
  Phase 3 — needs program-owner decision before P2-M8.
- **[OPEN] Model-checking tool.** GG-WAKE-0002's verification plan names
  "TLA+/JPF-scale" — the actual tool (TLA+, Java PathFinder, or a bespoke
  interleaving explorer over the scheduler state machine) is undecided.
  P2-M9 begins with a tool spike.
- **[OPEN] Repo layout.** Whether the scheduler lives under
  `phases/phase2/src/` (mirroring Phase 1) or in a new top-level module;
  how the Phase-1 tree is consumed (frozen dependency vs in-place extension).
  Decide at P2-M1 start; do not guess.

## 4. Milestones and acceptance

- P2-M1: scheduler core (WS A) — goroutine spawn/park/resume, drainer, cap
  assert, ticket discipline; quiescence probe green at 10k goroutines
  (GG-SCHED-0001/0002/0003, GG-WAKE-0001, GG-0017 core→active)
- P2-M2: channels + select (WS B) — differential vs gc on channel semantics
  corpus incl. close-while-parked and select-fairness (GG-0017 chan→active)
- P2-M3: timers/sleep + blocking NIO (WS C) — timer correctness vs gc;
  slot-releasing NIO with CapExhaustion-style falsifier; netpoller deletion
  (GG-0018, GG-SCHED-0004→active)
- P2-M4: GC interop (WS D) — design spike + ADR first ([OPEN] layout);
  exact roots + Truffle-level write barriers; barrier/root differential vs gc
- P2-M5: GODEBUG triage (WS E) — versioned registry published, unknown-knob
  deny-with-error, `godebug-diff` green (GG-0055→active) → gate 5 (part)
- P2-M6: JFR events (WS F) — goroutine/channel events flowing on the
  conformance corpus (GG-0008 JFR slice→active) → gate 3
- P2-M7: pinning audit (WS G) — `jdk.VirtualThreadPinned` zero unexpected
  pins; FFM/file-I/O off-carrier or budgeted (GG-0057→active) → gate 2
- P2-M8: preemption-torture re-run on the production path (WS H) — spike 2b
  F1–F6 reproduced; `runtime`/`sync`/`context` stress suites differential vs
  gc incl. pinned GOMAXPROCS matrix → gate 1
- P2-M9: lost-wakeup model-checking + race stress (WS I) — protocol model
  covering GG-WAKE-0002 (a–f); JVM race tools on the runtime → gate 4 (part)
- P2-M10: divergence list + closeout (WS J) — GG-0048 observable list closed
  or explicitly accepted with rationale; final reviews; cross-doc audit →
  gates 4, 5 → closeout

**Review gate (program-owner directive 2026-10-04, carried from Phase 1):**
every milestone P2-M1–P2-M9 closes with ≥2 independent peer reviews; every
finding is remediated and re-tested before the milestone sign-off is recorded.
Reviews run against a fixed snapshot or diff of the milestone's scope (never
the moving live tree); review evidence (snapshot checksums, per-reviewer
reports, findings list with remediation at named targets) lives under
`reviews/`. P2-M10 retains the final cross-cutting review: self-attack on the
scheduler (adversarial interleavings, close-while-parked storms, timer storms),
grep gates, cross-doc consistency audit, closeout.

**Phase-1 lessons carried over (non-negotiable):**
1. The supervisor (or a second worker) independently re-runs the full suite
   and `make check` on the sign-off tree before accepting any milestone
   sign-off — a worker's reported suite number is a claim, never evidence
   (M8 closeout incident, AGENTS.md 2026-10-05).
2. No sign-off on stale numbers: the sign-off entry's suite/test/corpus
   numbers must come from runs on the final tree, and every number in the
   entry must cite its evidence file (M8 195/0 incident; M8 corpus-misread
   incident).
3. Differential evidence required: every new semantic (channel op, timer,
   scheduler admission) lands red-green-first vs gc (Phase-1 SPEC §5.2
   discipline).
4. Every escaped bug: fix the instance, hunt the whole error class, patch the
   process (skill/AGENTS.md) — the standing escape protocol.
5. One logical change per commit; `tools/precommit-check.sh`; no untracked
   sources; docs updated in the same commit (Phase-1 worker checklist).

**Worker commit checklist (Phase-1 checklist, adapted):**
1. `bash tools/precommit-check.sh` — fails on untracked sources outside
   scratch dirs;
2. `make -C phases/phase2 test` green on the tree as committed (never commit
   red or untested state); Phase-1 suite (`make -C phases/phase1 test`) stays
   green — GOMAXPROCS=1 must be observationally identical to Phase 1;
3. one logical change per commit, message naming what and why; never
   `--amend` someone else's commit.

## 5. Test strategy

`graalgo-run --gmp=N file.go`: frontend → AST → execute `main` on the
user-space scheduler with in-flight cap N. Differential oracle vs stock gc
with the same GOMAXPROCS. Corpus: (1) Phase-1 corpus (single-threaded
regression base — must stay green, zero new divergences); (2) new concurrency
corpus: goroutine spawn/join, channel semantics (buffered/unbuffered/close/
range), select fairness, timers/sleep, GOMAXPROCS matrix (1/2/N/host),
preemption torture (spike 2b F1–F6 ported to the production path);
(3) `runtime`/`sync`/`context` stress suites (scope per [OPEN] sync decision).
Every new scheduler semantic lands red-green-first. The F5 deviation class
(Java blocking bypassing the scheduler) is documented and measured, never
silent. Fault injection: unpaired `ioEnter`, drainer interrupt, ticket
generation skew — all fail-fast asserted.

## 6. Done-gates (binary pass/fail, from PHASES.md §4)

1. **Stress + torture.** `runtime`/`sync`/`context` stress suites pass
   differentially vs gc, including a pinned GOMAXPROCS matrix AND the
   preemption-torture suite re-run against the 2b design on the production
   path (spike 2b F1–F6 must reproduce on the production path, not just the
   prototype). Evidence: `phases/phase2/hidden_files/torture/REPORT.md` +
   archived logs; differential corpus JSON with 0 unexplained divergences.
   FAIL = any systematic divergence the scheduler cannot close (kill
   criterion 1).
2. **Pinning audit.** `jdk.VirtualThreadPinned` shows zero unexpected pins on
   the conformance corpus. Evidence: `phases/phase2/hidden_files/pinning/AUDIT.md`
   with event counts; every pin classified (expected = off-carrier FFM/file
   I/O or budgeted +k carriers per GG-0057). FAIL = any unexpected pin.
3. **JFR flowing.** Goroutine lifecycle + channel events observable via JFR on
   the conformance corpus. Evidence: `phases/phase2/hidden_files/jfr/EVENTS.md`
   with a recorded JFR file + event counts. FAIL = events absent or malformed.
4. **Reviews + race/model-checking.** ≥2 independent reviews per milestone,
   all findings remediated and re-tested; race-detector-equivalent stress
   (JVM race tools on the runtime, not the guest); lost-wakeup model-checking
   pass covering GG-WAKE-0002 (a–f). Evidence: `reviews/phase2-m*/`, model
   report in `phases/phase2/hidden_files/modelcheck/REPORT.md`. FAIL = any
   unremediated finding or uncovered interleaving.
5. **Divergence list.** The scheduling-observable divergence list (GG-0048:
   fairness, starvation-freedom, latency distribution, GOMAXPROCS behavior)
   is closed or explicitly accepted with rationale per entry. Evidence:
   `phases/phase2/DIVERGENCES.md` (versioned). FAIL = any entry neither closed
   nor accepted.

## 7. Risks and kill triggers

**Kill criteria (PHASES.md §4, verbatim):**
- "preemption-torture shows systematic divergence the user-space scheduler
  cannot close" — falsifying experiment: the preemption-torture suite re-run
  on the production path (P2-M8; spike 2b F1–F6). A single failing leg is a
  bug to fix; *systematic* divergence across legs that survives the
  error-class hunt is the kill trigger → escalate to program owner.
- "in-flight-cap economics make `GOMAXPROCS` fidelity impossible within the
  perf budget (e.g., the F5 under-utilization class proves unfixable and
  observably violates the cap semantics)" — falsifying experiment: the
  pinned GOMAXPROCS matrix incl. mixed I/O+compute (P2-M8, GG-SCHED-0003
  hard assert). If the cap cannot be honored without breaking the overhead
  budget (GG-FAIR-0002), the design is killed → escalate.

**Other risks:**
- GC interop design ([OPEN] layout) may force a scheduler-visible safepoint
  redesign — P2-M4's design spike must land before P2-M8's torture re-run
  depends on it; if the spike fails, replan P2-M4 before building on it.
- 38 µs cold-drainer wakeup unexplained (carried from Phase 0, risk-row-10):
  hypothesis + server-hardware re-test trigger stand; kill trigger only if it
  breaks the overhead budget at the 10 ms quantum on quiet hardware.
- 13× rate gap (prototype vs gc throughput): prototype + structural hop cost;
  no production claims until re-measured on the production path (P2-M8).

## 8. Closeout checklist

[ ] gates 1–5 evidenced (named artifacts in §6) [ ] per-milestone review gates
closed (P2-M1–P2-M9: ≥2 independent reviews each, every finding remediated at
named targets and re-tested) [ ] self-attack report (scheduler adversarial:
interleavings, close storms, timer storms) [ ] grep gates (no placeholders;
scheduler-aware-blocking static audit per GG-SCHED-0004) [ ] cross-doc
consistency audit [ ] numbers table with log-line citations [ ] GG states
recorded (SCHED/WAKE/FAIR →active per workstream as tests land) [ ]
divergence list closed or accepted (`phases/phase2/DIVERGENCES.md`)

## 9. Falsifiers per owned GG ID

- GG-SCHED-0001: quantum park not taken at a `Safepoint.poll()` on expiry →
  torture red; mean hop overhead > 1% of quantum at 10 ms → budget red
- GG-SCHED-0002: drainer exits on interrupt, or lane order violated under
  INIT flood → quiescence red
- GG-SCHED-0003: admitted-but-unparked ever exceeds
  `min(GOMAXPROCS, carrier parallelism)` → hard-assert red (kill-adjacent)
- GG-SCHED-0004: raw Java blocking found in adaptation layer (static audit)
  or unpaired `ioEnter` → red
- GG-WAKE-0001: ack-after-unpark ordering violated in code review → red
- GG-WAKE-0002: model-checker finds a lost wakeup in (a–f), or
  admissions != parks at quiescence → red
- GG-WAKE-0003: wake/init token admitted behind runFifo under flood →
  latency/quiescence red
- GG-FAIR-0001: 10k-spinner quanta max/mean > 2x → red
- GG-FAIR-0002: mean hop overhead > 1% of 10 ms quantum on quiet hardware → red
- GG-FAIR-0003: any runnable goroutine unadmitted after 4N admissions, or
  never_scheduled > 0 in quiescence probe → red
- GG-0017: any scheduler semantic ≠ gc differentially → red
- GG-0018: netpoller code path reachable, or NIO bypassing slot discipline → red
- GG-0048: scheduling observable diverges systematically and unclosably →
  kill criterion 1
- GG-0055: unknown GODEBUG knob not denied, or registry unreviewed → red
- GG-0057: unexpected `jdk.VirtualThreadPinned` event → red
- GG-0008: JFR goroutine/channel events absent on conformance corpus → red
