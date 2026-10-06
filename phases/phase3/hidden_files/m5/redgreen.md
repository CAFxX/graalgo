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

## Java section (Java follow-up worker, 2026-10-06)

### Channel builders (`ChanBuilderTest`, 18 tests)

Unit suite: `phases/phase3/src/test/java/graalgo/frontend/ChanBuilderTest.java`
(registered in `IfaceTestRunner`; `make -C phases/phase3 test` → 51 tests,
0 failures, 2026-10-06). Each builder is driven on parsed wire fragments
(the exporter's exact JSON shape) through the real `NodeFactory` with the
five kinds registered as in `P3SyncMain`; execution runs on the real
scheduler so park/unpark paths are exercised.

- RED (2026-10-06): the first run caught a genuine builder bug —
  `ChanSendNode`/`SelectNode` read send values via `executeObject`, which
  is null for primitive-kind value nodes (e.g. an int literal), so
  `testSendRecvBuffered`/`testRecvOnClosedDrainedYieldsZero` failed with
  NPE. Fixed with the kind-aware `ChanWire.evalBoxed` (mirrors
  `AssignNode`'s dispatch). Restored → green.
- Semantics pinned: makechan buffered/unbuffered/negative-size panic;
  send/recv round trip; send-on-closed / close-of-closed / close-of-nil
  panics with gc messages; recv on closed+drained yields zero (int and
  struct element); select ready-choice + target assign, default,
  send-case win, poison-before-default (ch11 parity), nil-channel case
  drop (gc-exact), single-case two-value recv `(0, false)` (the comma-ok
  desugar pattern), park-until-send wakeup (deterministic via
  `recvWaitersCount`); wire rejections (targets on send case,
  undeclared target objid).

### TextMap `sectionFor` clamp (`SectionClampTest`, 3 tests)

`phases/phase1/src/test/java/graalgo/frontend/SectionClampTest.java`
(registered in phase-1 `graalgo.test.Runner`). Unblocks tc15 (see
"Exporter worker" note above).

- RED (2026-10-06, fix reverted): `testOutOfRangeLineClamped` FAILS with
  `java.lang.IllegalArgumentException: line out of bounds` and
  `testOutOfRangeColumnClamped` FAILS with `column out of range` — the
  exact reported failure. `testInRangePositionUnchanged` passes.
- GREEN (fix applied): all 3 pass; in-range positions produce the
  identical section as before (byte-identical path); out-of-range
  positions clamp to a zero-length section at column 1 of the nearest
  valid line.

## Wire reconciliation with the committed exporter (2026-10-06, post-build)

The exporter worker committed `chanops_m5.go` (c669fc5) with a wire
shape differing from the Java builder's first cut:

| aspect | exporter (committed) | builder (first cut) |
|---|---|---|
| select cases | `"selectcase"` wrapper nodes | bare `{"kind":"recv"...}` objects |
| case body | a `"block"` node | raw statement array |
| targets | bare objid array `[12,15]` | `[{"obj":12,"type":5}]` |
| makechan | extra `"type"` chan-type-id field | ignored |

Resolution (exporter's wire wins — committed + more wire-idiomatic):
- `SelectNode` now unwraps `selectcase` nodes, builds the body via
  `factory.buildStmt` on the block node, and resolves target types from
  the wire `objects` array; undeclared objids fail fast (this also
  soundly rejects global `=` targets, which must be desugared by the
  exporter through a temporary).
- `MakeChanNode` accepts and lightly validates the optional `type`
  field (kind `chan`, `detail.elem` == `elem` field); RED-proven by
  `testMakechanMismatchedTypeField` (non-chan type id, elem mismatch).
- The exporter's `recv{commaOk:true}` shape is rejected by the FROZEN
  factory (`isCommaOkValue` only recognizes `index`/`typeassert`) — the
  exporter must desugar `v, ok := <-ch` to a single-case no-default
  select (works today, tested); no factory change without its own ADR.
- ADR-0005 Appendix C rewritten to the reconciled contract; the
  earlier "Java Worker Coordination Needed" note asking for
  `registerExprKind("unary")` for `<-` is obsolete (exporter emits
  `"recv"`; the builder handles it).

### Shared-tree stale-build incident (2026-10-06)

The first post-reconciliation `make -C phases/phase3 test` reported
"53 tests, 1 failure" (`testMakechanMismatchedTypeField`) — but the
failure was a STALE CLASS: another process on this shared tree ran
`make -C phases/phase3 main` at 16:52:21 UTC, ~2s after the
`MakeChanNode.java` edit (16:52:19), and javac read the pre-edit
source; the resulting `.built` (16:52:22) was newer than the edited
source, so my `make test` skipped recompilation. Verified via
`javap`/`strings` (no `"must be a chan"` in the .class). Fix:
`rm phases/phase3/out/classes/.built`, full recompile, string-check
the classes, re-run. The 53/53 green run above is the valid evidence.
Process note: timestamp-incremental `make` on a tree shared with other
workers (exporter, survey, acceptance runner — the latter was running
`P3SyncMain ... ch12_select_poison_ready.go` concurrently) is racy;
when a test result contradicts the source on disk, suspect a stale
build first (`javap`/`strings` the class) before suspecting the code.
Companion of the 2026-10-06 worktree lesson in AGENTS.md.

## Go section — comma-ok desugar + test fixes (2026-10-06, exporter worker)

### RED: recv{commaOk:true} rejected by frozen factory
The `v, ok := <-ch` shape emitted `recv{commaOk: true}`. The frozen
factory's `isCommaOkValue` only recognizes `index`/`typeassert`, so
`buildVarDecl` failed with "values/objs length mismatch" (ch08).
Per ADR-0005 Appendix C (reconciled 2026-10-06), the exporter must NOT
emit `recv{commaOk:true}`.

### GREEN: single-case select desugar
`exportRecvCommaOkDesugar` lowers `v, ok := <-ch` / `v, ok = <-ch` to a
single-case, no-default `select` with `targets: [v, ok]`:
- `:=`: defines the targets, emits dominating `vardecl` (values=[]),
  then `select`.
- `=`: uses existing local objs directly (no vardecl); globals desugar
  through temps + copy-back assign (ADR-0005 App C: globals not in frame
  maps); blank `_` uses a synthetic temp.
The `recv` node no longer carries `commaOk`. `exportCommaOk` drops the
`UnaryExpr` case (now unreachable).

### RED: MainTest.testCheckRejectsScopeError
Phase-1 `MainTest.testCheckRejectsScopeError` used `select {}` as the
scope-error example. After c669fc5, `select` lowers instead of
scope-erroring, so the test failed.

### GREEN: test updated to WS D example
The test now uses `func f() (int, int)` (multi-result function, still
out of scope) and expects "WS D" in the error. Comment notes that
`select` is now in scope (P3-M5).

### RED: TestChannelOpsM5 expected commaOk
`TestChannelOpsM5` asserted both plain and comma-ok `recv` nodes exist.
After the desugar, no `recv` carries `commaOk`.

### GREEN: test updated to desugar
The test now asserts: (1) no `recv` node has `commaOk`, (2) plain `recv`
nodes exist, (3) a single-case `select` with 2 targets exists for the
`v, ok := <-ch` shape.

### GREEN: ch01-ch12 graduation (24/24)
`phases/phase2/corpus/channels/run_e2e.sh`: all 12 programs at gmp=1 and
gmp=4 produce their `.expected` output through the full pipeline
(gofrontend → P3SyncMain). Notes:
- ch04 required `go producer(ch)` (go with args). The exporter now
  desugars `go f(args)` to hoisted temps + `go func() { f(temps) }()`
  (GoStatementNode P2-M1 only builds zero-arg spawns).
- ch12 is racy in gc (select between panic-on-closed-send and recv);
  the script prioritizes `.expected` over fresh gc runs for such cases.

## 2026-10-06: BOOL->UNTYPED_BOOL fix (coordinator)

**RED**: timectx differential 0/27 — all programs failed with
`IllegalArgumentException: illegal conversion: BOOL -> UNTYPED_BOOL` at
`NodeFactory.buildUnary` → `maybeConvert`.

**Root cause**: go/types infers `untyped bool` (not `bool`) for the unary
`!` in `if !(s[0] == '.' || ('0' <= s[0] && s[0] <= '9'))` within the
synthetic `__parseDuration` (verified via standalone type-check: the full
package context yields untyped, a minimal snippet yields typed — a
go/types quirk). The exporter emitted type 101 (`untyped bool`); the
frozen `ConvertNode.checkCombo` rejects `BOOL -> UNTYPED_BOOL`. Since the
synthetic `time` package is compiled whole, this broke every
`import "time"` program.

**Fix** (commit 8692da5): rewrote as De Morgan equivalent
`if s[0] != '.' && (s[0] < '0' || s[0] > '9')` — no `!(...)`, no untyped
bool. P3-M5's own synthetic source (not frozen Phase-1 interface).

**GREEN**: tc00 runs end-to-end (output: non-zero, unix-positive,
unixnano-positive, monotonic, sub-nonnegative). Full differential
re-running.
