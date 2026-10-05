# Phase-3 checkpoint (append-only; newest state at the tail)

## 2026-10-06 — Phase 3 opened (planning)

**Phase-2 handoff state:** Phase 2 COMPLETE, sign-off 9694cbf
(2026-10-06). All five done-gates met; 193/193 phase-2 + 208/208 phase-1
suites independently re-verified by the supervisor. Delivered: scheduler
core, channels+select, timers, GC interop, GODEBUG triage, JFR events,
pinning audit (0 unexpected pins, mechanical gate), preemption-torture
(12/12, kill criteria NOT triggered), lost-wakeup model checking
(bespoke explorer over the real Drainer), divergence closeout.
Integrity repair held throughout (supervisor re-ran every suite).
Hourly Phase-2 report cron retired.

**Phase-3 entry gate (must be true before P3-M1 starts):**
1. Phase-2 sign-off tree clean at 9694cbf — verified (supervisor).
2. Interface freeze v1 in force; Phase-3 code lives under
   `phases/phase3/src/`; Phase-1 files are not modified.
3. Toolchain intact: Oracle GraalVM 25.0.4.1.1 LTS, stock Go 1.27.1 at
   `~/tools/go`, go1.27.1 stdlib source present at
   `~/tools/go-1.27.1/src` (sync/time/context dirs confirmed).
4. Normative inputs read: PHASES.md §5, SPEC.md, INTERFACE_FREEZE.md,
   phases/phase2/DONE-GATES.md + CHECKPOINT.md tail.

**Open items carried forward (Phase 2 → Phase 3):**
1. **P2-M8 deferred `sync` Go bindings — now Phase 3's problem (P3-M2).**
   The four primitives exist as Phase-2 Java classes
   (`graalgo.runtime.sync.{GoMutex,GoRWMutex,GoWaitGroup,GoOnce}`),
   stressed at the Java level. P3-M2 lowers `import "sync"` onto them
   and re-runs the P2-M8 stress as Go programs differential vs gc —
   discharging the recorded re-test trigger. (Owner-accepted at
   P2-M10; conditional discharged.)
2. **DIV-M3-1 (`len()` on buffered timer channel returns 1, gc: 0) —
   BLOCKING for `time` lowering (P3-M5).** Fix red-green-first in
   P3-M2, before `time` is lowered. Documented in
   phases/phase2/DIVERGENCES.md with the trigger.
3. **M9 polymorphism re-measurement** fires when interface dispatch
   lands (P3-M3). 170 sites were 99.4% monomorphic with no dispatch;
   re-measure with dispatch live.
4. **Latency-distribution tolerance** (ACCEPTED-WITH-TOLERANCE at
   P2-M10): server-hardware re-measure trigger stands; Phase-3
   perf-sensitive work must not silently widen the band.
5. **38µs cold-drainer wakeup** (unexplained, carried since Phase 0):
   hypothesis + server-hardware re-test trigger stand.
6. **`context` package** was pattern-level in Phase 2 (needs
   interfaces); P3-M3 dispatch unblocks it for P3-M5.

**Standing owner directives in force:**
- Proceed autonomously to completion of the project (2026-10-06).
- Supervisor re-runs suites + `make check` before accepting any
  milestone sign-off (M8 integrity repair, now habit).
- Continuous adversarial test-finding track: launch as soon as the
  supervisor judges it productive; findings filed as bugs with
  reproducers; layer-4 slot reserved in this phase's testing strategy.
- Hourly Phase-3 reporting cron: installed by the supervisor at phase
  start, retired at independent phase confirmation.
