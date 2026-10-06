# P3-M5 red-green evidence

## Java section (Java runtime worker)

Unit suite: `phases/phase3/src/test/java/graalgo/p3time/TimeChanLenCapTest.java`
(registered in `IfaceTestRunner`; `make -C phases/phase3 test` → 33 tests,
0 failures, 2026-10-06, stable across 4 consecutive runs).

### DIV-M3-1 unit regression (`testTimerChanLenCapAllStates`)

Asserts `buffered()==0 && capacity()==0` on the user-visible `TimeChan`
in every state: pre-fire, fired-undrained, drained, stopped — while the
fire value remains deliverable (`recv` yields a `GoStruct` Time whose
unixnano is ~now). The corpus tc07 covers this end to end; this pins it
at the unit level.

- RED (2026-10-06): temporarily changed `TimeChan` to extend plain
  `GoChan(1)` (1-slot buffer, no `TimerChan` masking — i.e. the pre-M2a
  shape) → `testTimerChanLenCapAllStates` and
  `testTickerThreeTicksAndStop` FAIL (`buffered()==1` after fire).
  Restored → green.

### Stop/Reset gc-parity table (same test + ticker/stop tests)

- Stop on fired-undrained → true (Go 1.23+ `timerchandrain` parity);
  second Stop → false; Reset on stopped → false (re-arms); Stop on
  fired+drained → false; Stop before fire → true with no later delivery;
  ticker: 3 Time ticks then Stop → true, no 4th tick.
- RED (2026-10-06): removed the translated-channel drain from
  `GoTimerT.stop()` (heap truth only) →
  `testTimerChanLenCapAllStates` FAILS on "stop on fired-undrained ->
  true" (the heap had already handed the value to the translator, so
  without the user-channel drain Stop wrongly reports false).
  Restored → green.

### Quiescence-safety (no parked-translator hang)

`awaitQuiescence` waits for *every* spawned goroutine, so each test
asserts `P3TimeRuntime.backgroundLive()==0` after `stop()`/shutdown:
`testStopBeforeFirePreventsDelivery`, `testTickerThreeTicksAndStop`,
`testShutdownBackgroundEndsLiveTicker` (a never-stopped ticker is shut
down via `P3TimeRuntime.shutdownBackground()`, the launcher's phase-1
protocol — then `awaitQuiescence` completes).

- BUG FOUND BY THE TESTS (2026-10-06, fixed before commit):
  `testStopBeforeFirePreventsDelivery` hung: `stop()` installed a *fresh*
  `done` channel, but a translator that had not yet read the old `done`
  picked up the fresh (open, never-to-be-closed) one and parked forever.
  Fix: `stop()` closes `done` without replacing it; `reset()` does
  close-then-replace atomically under the translator lock, so a parked
  translator is always on the current done or a closed one it is about to
  wake from — never on an open, abandoned done. Regression covered by the
  `backgroundLive()==0` assertions (they timed out pre-fix).

### AfterFunc

The `AfterFunc` fire-spawns-goroutine path needs a `GoFunc` (Truffle call
target) — not constructible at unit level. Covered end to end by the
corpus (exporter worker's section below).

## Go section (exporter worker — owner: exporter worker)

### Spike Results (2026-10-06)

**1. `any == any`: WORKS**
`var a, b any = "x", "x"; println(a == b)` gives true/false/true/true,
byte-identical to gc 1.27.1 through `P3SyncMain`. The valueCtx key lookup
using `==` on `any` is safe.

**2. Nil-channel blocking: FAILS (ADR fallback used)**
`select`/`send`/`close` are NOT in gofrontend's v1 scope
(`*ast.SelectStmt`/`*ast.SendStmt` rejected; `close` not in the 16 frozen
builtins). Could not test via launcher. Java source check: `ChanOps.recv(null)`
throws `IllegalArgumentException` via `requireGoChan`. So nil does NOT block
forever. Per ADR: **emptyCtx.Done() returns a package-level never-closed
channel** (`ctxNeverFire`), not nil.

**Bonus**: gofrontend EMITS `<-ch` as a `"unary"` node with op `"<-"`, but no
launcher builds it yet (`UnaryOpNode` throws "unsupported unary operator" —
Java worker must register `registerExprKind("unary",...)` in `P3SyncMain`).

### Red-Green Tests

**Time package (exporter)**
- **Green**: tc00 (Now/Unix), tc01 (Duration arith), tc02 (Sleep),
  tc03 (After), tc04 (Timer Stop/Reset), tc05 (Ticker), tc06 (AfterFunc),
  tc07 (chan len/cap), tc15 (Sleep zero/negative), tc16 (Reset expired)
  all export successfully (exit 0, valid JSON with correct wire nodes).
- **Red (by design)**: Multi-result calls fail fast with named M5 scope
  error: `time.ParseDuration`, `Time.Date`/`Clock`/`Zone`.

**Context package (exporter)**
- **Green**: tc11 (WithValue) exports successfully. Background/TODO/WithValue/
  Cause/AfterFunc/WithoutCancel all type-check and export.
- **Red (by design, WS D blocker)**: `WithCancel`, `WithCancelCause`,
  `WithDeadline`, `WithTimeout` all fail fast with "not in M5 scope
  (multi-result calls need WS D)". These are the CORE context constructors —
  without WS D multi-result support, the context package cannot be used
  for cancellation in v1 programs. **ESCALATED to coordinator.**

**Intrinsic/method collision (bug found and fixed)**
- **Red**: `Time.After` method was not indexed because `isSyntheticIntrinsic`
  skipped it (name collision with `time.After` function).
- **Green**: After fix (only skip if `d.Recv == nil`), `t.After(u)` works.
  Commit 41ba910.

### Differential Results (2026-10-06)

Corpus `phases/phase3/corpus/timectx/` (tc00-tc16) differential vs gc 1.27.1:

**Exporter-level** (gofrontend exit 0):
- tc00, tc01, tc02, tc03, tc04, tc05, tc06, tc07, tc11, tc15, tc16: PASS
- tc08, tc09, tc10, tc12, tc13, tc14: FAIL (by design — multi-result
  constructors blocked, see WS D blocker above)

**End-to-end** (via P3SyncMain, Java worker's domain):
- 0 passed, 27 failed (2026-10-06 run)
- Failures:
  - Multi-result fail-fast (expected, exporter by design): tc08-tc10,
    tc12-tc14
  - Java `TextMap.lineStartOffset: line out of bounds`: tc15
    (exporter emits valid JSON; Java source-position handling bug —
    reported to Java worker)
  - Java `unsupported unary operator "<-"`: tc16 (exporter emits
    `"unary"` node per v1; Java needs `registerExprKind("unary",...)` —
    reported to Java worker)

### ADR Deviations

1. **Multi-result deferral**: `Context.Deadline()`, `time.ParseDuration`,
   `Time.Date`/`Clock`/`Zone`, AND `context.WithCancel`/`WithCancelCause`/
   `WithDeadline`/`WithTimeout` are declared with exact gc signatures but
   fail fast at call sites (WS D limitation). The ADR §D2 describes these
   as working; they do not in v1. **This blocks the primary context
   use case (cancellation).**
2. **Nil-channel fallback used**: emptyCtx.Done() returns `ctxNeverFire`
   (package-level never-closed channel), not nil, because nil-recv throws
   instead of blocking.
3. **Wire `type` fields**: All seven typed time nodes carry `type` per
   Appendix W (added to ADR by Java worker, commit 0ca2dce). Exporter
   was already compliant.

### Java Worker Coordination Needed

1. Register `registerExprKind("unary",...)` for `"<-"` operator in
   `P3SyncMain` (blocks tc16 and all channel receives).
2. Fix `TextMap.lineStartOffset` "line out of bounds" (blocks tc15).
3. Implement `chanclose` marker handling for `ctxChanClose` intrinsic.
4. Implement the seven time wire nodes per Appendix W (already done
   per commit 0ca2dce, but end-to-end not yet verified).
