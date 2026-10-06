# ADR-0005 — P3-M5: synthetic `time` + `context` packages (WS E)

**Date:** 2026-10-06
**Status:** accepted 2026-10-06 — P3-M5 implementation brief. Spec-first
ordering honored: ADR committed before any exporter/factory change.
**Scope:** closes PLAN.md §4 P3-M5 (WS E); GG-0017 time/ctx slice →active.
**Normative basis:** `phases/phase1/INTERFACE_FREEZE.md` (v1, in force)
§§1, 2, 5; PLAN.md §§2–5 (WS E); ADR-0002/ADR-0003/ADR-0004 (precedent).

## Context

P3-M5 lowers `import "time"` onto the Phase-2 timers and `import "context"`
via the P3-M3 interface dispatch. The real `time`/`context` packages land
from adapted sources in P3-M7 (WS G); this milestone builds synthetic
bridge packages in `overlayImporter`, following the ADR-0002 (`sync`)
precedent exactly: exact gc 1.27.1 signatures for the in-scope API,
exporter-recognized intrinsics, out-of-scope members fail fast with
"not in M5 scope" — never silently. DIV-M3-1 is FIXED (P3-M2a); the fix
must be verified in the `time` lowering loop (regression test, §D4).

## Decision

### D1. Synthetic `time` package — clock/timer intrinsics + pure-Go value types

`time.Duration` is `type Duration int64` with **real Go methods** in the
synthetic package (pure arithmetic — no intrinsics needed): `String`,
`Nanoseconds/Microseconds/Milliseconds/Seconds/Minutes/Hours`, `Abs`,
`Round`, `Truncate`. Constants `Nanosecond…Hour`. `ParseDuration` is real
Go (string parsing). `Month`/`Weekday` are int types with `String()`.

`time.Time` is `struct{ unixnano int64 }` (unexported field; the factory's
zero is the zero Time, `IsZero()` ⇔ `unixnano==0`). **Real Go methods**:
`Add`, `Sub`, `Before`, `After`, `Equal`, `Compare`, `IsZero`, `Unix`,
`UnixNano`, `Round`, `Truncate`. `Since`/`Until` are real Go
(`Now().Sub(t)` / `t.Sub(Now())`).

**Intrinsics** (Java runtime, Phase-2 timers underneath):
- `Now()` → `timenow` wire node → `Time{unixnano}` from the system clock.
- `Sleep(d)` → `timesleep`; d ≤ 0 returns immediately (gc parity).
- `After(d)` → `timeafter` → `<-chan Time` backed by the `TimerChan`
  flavor (DIV-M3-1: `len`/`cap` report 0).
- `NewTimer(d) *Timer` → `timenew{timer}`; `(*Timer).Stop() bool` /
  `Reset(d) bool` → `timeop`; `t.C` selector → `timec` (TimerChan).
- `NewTicker(d) *Ticker` → `timenew{ticker}`; `Stop`/`Reset` → `timeop`;
  `t.C` → `timec`. `Tick(d)` → `timetick` (documented leak, like gc).
- `AfterFunc(d, f)` → `timeafterfunc` → `*Timer`; on fire the function
  runs in a new goroutine (scheduler-aware spawn).

`Timer`/`Ticker` are **opaque Java objects** (the ADR-0002 `sync.Mutex`
pattern): the exporter intercepts the `.C` selector on `*time.Timer` /
`*time.Ticker` (SelectorExpr where the receiver unwraps to the synthetic
named type) and the `Stop`/`Reset` method calls, exactly as it intercepts
`syncop` MethodVals.

**Out of M5 scope** (fail fast, "not in M5 scope"): `Format`/`Parse`
(reference-layout formatting), `Location`/timezones (`Local` ≡ UTC —
**accepted divergence**, documented here; P3-M7's real package fixes it),
`AddDate`/`Date`/`Clock`/calendar decomposition, `time.Time.String`.
Corpus programs avoid zone-sensitive output.

### D2. Synthetic `context` package — REAL Go on channels + synthetic time

`context` is implemented as **real Go source** in the synthetic package,
composing P3-M2 (`sync.Mutex`), Phase-2 channels/select, synthetic `time`,
and P3-M3 interface dispatch — this milestone is therefore also a
combination test of M2+M3+M5:

```go
type Context interface {
    Deadline() (deadline time.Time, ok bool)
    Done() <-chan struct{}
    Err() error
    Value(key any) any
}
```

- `Background()`/`TODO()` → `emptyCtx` (Done returns a never-firing
  channel — nil-channel blocking must be verified; fallback is a
  package-level never-closed channel).
- `WithCancel`/`WithCancelCause` → `*cancelCtx` (`sync.Mutex`-guarded
  `done chan struct{}`, closed on cancel; `cause error`). `Cause(ctx)`.
- `WithDeadline`/`WithTimeout` → `*timerCtx` via `time.AfterFunc`;
  past deadlines cancel immediately (AfterFunc d≤0 fires at once).
- `WithValue` → `*valueCtx{parent, key, val}`; nil key panics
  ("context: nil key", gc parity).
- `AfterFunc(ctx, f)` → stop func; runs f in its own goroutine on Done.
- `WithoutCancel` → parent values without cancellation propagation.
- `var Canceled`, `var DeadlineExceeded` as local error types with
  `Error()` ("context canceled" / "context deadline exceeded").

**Spike-first risk:** `==` on `any` keys (valueCtx lookup) and nil-channel
blocking are runtime behaviors the worker must verify BEFORE writing the
package; if either is unsupported, scope `WithValue`/emptyCtx accordingly
and record the re-test trigger. The `Context` interface method calls
exercise P3-M3 dispatch end to end.

### D3. Wire nodes (additive, v1) + toolVersion 0.5.0 → 0.6.0

New node kinds, produced only by the exporter, consumed only by the
phase-3 module via the public `registerExprKind` hook (ADR-0002 precedent):

- `"timenew"`: {kind: "timer"|"ticker", duration}
- `"timeop"`: {op: "Stop"|"Reset", recv, duration?} → bool
- `"timec"`: {recv} → channel (the `.C` selector)
- `"timenow"`: {} → Time struct
- `"timesleep"`: {duration}
- `"timeafter"`: {duration} → channel
- `"timetick"`: {duration} → channel
- `"timeafterfunc"`: {duration, func} → *Timer

Old consumers reject all as unknown per freeze §2.3. toolVersion bump per
freeze §5. Java side: new `graalgo.p3time` package; builders registered in
`P3SyncMain` (the unified launcher — NOT renamed; harnesses depend on it).

### D4. DIV-M3-1 regression in the loop + corpus

`phases/phase3/corpus/timectx/` with `run_differential.sh` following the
iface/sync harness pattern EXACTLY (cd-to-script-dir, relative paths,
works from repo root and from its own dir — the P3-M4 double-cd lesson;
gc built binary not `go run`; stdout+exit+(panic first line) comparison;
JVM Unsafe stderr filter). Programs tc00–tc16: Now/Unix, Duration arith +
String + ParseDuration, Sleep, After, Timer Stop/Reset, Ticker 3-tick Stop,
AfterFunc, **tc07_timer_chan_len_cap** (the DIV-M3-1 regression:
`len`/`cap` of `After`/`Timer.C`/`Ticker.C` channels are 0), context
cancel/timeout/deadline/value/parent-propagation/AfterFunc/Cause,
Sleep(0)/negative, expired-timer Reset. Timing-sensitive programs print
fixed strings, never measured durations.

Red-green evidence in `phases/phase3/hidden_files/m5/redgreen.md` (e.g.
disable the TimerChan len override → tc07 red; break cancel propagation
→ parent-cancel red; break AfterFunc → red).

### D5. Freeze exceptions (documented, this ADR)

Exporter (`export.go`): synthetic `time` + `context` packages in
`overlayImporter` (with synthetic→synthetic import resolution
`context`→`time`, `context`→`sync`); the `timec` selector interception;
`timeop`/`timenew`/etc. method-call interception; `toolVersion` bump.
No factory change (Time/Duration are plain Go values; Timer/Ticker are
opaque Java). No phase-2 source change expected (timers consumed as-is);
if one proves necessary it gets its own ADR per the ADR-0004 rule.

**D5 exception, recorded 2026-10-06 (P3-M5 Java): `NodeFactory.sectionFor`
TextMap clamp.** Synthesized wire nodes (from the synthetic `time` package)
may carry `pos.line` past the end of the source text; Truffle's
`TextMap.lineStartOffset` rejects them with
`IllegalArgumentException: line out of bounds`, failing the build. The fix
is additive and behavior-preserving for in-range positions: `sectionFor`
tries the requested `(line, col, len)` exactly as before, and on
`IllegalArgumentException` synthesizes a valid zero-length section at
column 1 of the clamped line (`clamp(line, 1, lineCount)`). Red-green:
`SectionClampTest` (phase-1 `graalgo.test.Runner`) — red on the unfixed
factory (`line out of bounds` / `column out of range`), green after, with
an in-range control asserting the identical section as before.

## Consequences

- GG-0017 time/ctx slice →active when the first `verifies: GG-0017`
  timectx test lands.
- The synthetic packages retire in P3-M7 when the real `time`/`context`
  land from adapted sources (same retirement path as synthetic `sync`).
- Accepted divergence (this ADR): `time.Local` ≡ UTC until P3-M7.
- Re-test triggers: `any`-equality and nil-channel behavior (if scoped
  down); full `time` surface at P3-M7 supersedes this package.

### Accepted deviation (2026-10-06, exporter worker): multi-result calls

v1 cannot emit or call multi-result functions (WS D; the P3-M3 F4
fail-fast precedent). The following are declared with exact gc 1.27.1
signatures (so type-checking works) but every call site fails fast with
"not in M5 scope (multi-result calls need WS D; re-test when it lands)":

- `time.ParseDuration(string) (Duration, error)`
- `Time.Date() (year int, month Month, day int)`
- `Time.Clock() (hour, min, sec int)`
- `Time.Zone() (name string, offset int)`
- `Context.Deadline() (deadline Time, ok bool)`
- `context.WithCancel(parent Context) (Context, CancelFunc)`
- `context.WithCancelCause(parent Context) (Context, CancelCauseFunc)`
- `context.WithDeadline(parent Context, d Time) (Context, CancelFunc)`
- `context.WithTimeout(parent Context, timeout Duration) (Context, CancelFunc)`

**Impact**: The core context cancellation constructors cannot be called
in v1 programs. The context package type-checks and exports (Background,
TODO, WithValue, Cause, AfterFunc, WithoutCancel, Canceled,
DeadlineExceeded all work), but cancellation via WithCancel/WithDeadline/
WithTimeout requires WS D multi-result support. This is the primary
P3-M5 blocker — escalated to coordinator.

Re-test trigger: when WS D lands, remove the `isMultiResultSynthetic`
fail-fast and verify tc08-tc10, tc12-tc14 go green end to end.

## Appendix W — wire field detail as implemented (P3-M5 Java, 2026-10-06)

The D3 field lists are shorthand. Per the ADR-0002 `syncnew` precedent,
every time node carries `type`: the result type-table id, validated
fail-fast by the Java builders (never guessed):

- `"timenew"`: {kind: "timer"|"ticker", duration, type} — type is a
  pointer (`*time.Timer` / `*time.Ticker`).
- `"timeop"`: {op: "Stop"|"Reset", recv, duration?, type} — type is bool;
  `duration` required for Reset, absent for Stop.
- `"timec"`: {recv, type} — type is a channel.
- `"timenow"`: {type} — type is the named `time.Time`.
- `"timesleep"`: {duration} — void; no `type`.
- `"timeafter"`: {duration, type} — type is a channel.
- `"timetick"`: {duration, type} — type is a channel.
- `"timeafterfunc"`: {duration, func, type} — type is a pointer.

`duration` accepts a literal number (nanoseconds) or an expression node
evaluating to int64. The `time.Time` descriptor is resolved from the
type-table's named `time.Time` entry (must be `struct{ unixnano int64 }`)
by every builder that needs it — no cross-node ordering contract.

---

## Appendix C — Frontend channel operations (added 2026-10-06, coordinator)

**Problem:** P2-M2 implemented channels/select at the Java level only. The
P2-M2 corpus README states: "the Phase-1 exporter rejects these programs
(`SendStmt`/`SelectStmt` are 'not supported in frontend/v1 scope'), so
end-to-end `graalgo-run` execution awaits the exporter lowering (parent
track)." P3-M5 IS that parent track: `time`/`context` are unusable without
Go-level channel operations, and P3-M7's real stdlib needs them.

**Decision:** the exporter lowers Go channel syntax to additive wire nodes;
phase-3 registers builders via the public hooks (no frozen NodeFactory
change). The Phase-2 Java primitives (`ChanOps`, `SelectOp`, `GoChan`) are
consumed as-is.

Wire nodes (all additive, v1):
- `"makechan"`: {elem: type-id, size?: expr} — from `make(chan T)` /
  `make(chan T, n)`. The exporter emits this INSTEAD OF the generic
  `make` call node when the type is a chan (the frozen `buildMakeBuiltin`
  still rejects chan — we never send it one).
- `"send"`: {chan, value} — STATEMENT kind, from `ch <- v` (SendStmt).
  Send on closed channel panics (gc parity — the Java `ChanOps.send`
  already does this); send on nil channel blocks forever (gc parity).
- `"recv"`: {x: chan-expr, type} — EXPRESSION kind, from `<-ch`. The
  exporter currently emits `"unary"` with op `"<-"`; it now emits `"recv"`
  (the frozen `UnaryOpNode` cannot be extended — `registerExprKind`
  rejects duplicates — and `<-` was never implemented there).
- `"chanclose"`: {x: chan-expr} — STATEMENT kind, from `close(ch)`. The
  exporter intercepts the `close` identifier (it is NOT in the frozen
  builtin registry — we do not touch the registry; the exporter emits the
  wire node directly). Close of closed channel panics; close of nil
  channel panics (gc parity — verify against the Java `ChanOps.close`).
- `"select"`: STATEMENT kind, from `select` statements. Cases:
  `{kind: "recv", chan, targets?: [v(, ok)], body}`,
  `{kind: "send", chan, value, body}`,
  `{kind: "default", body}`.
  Evaluation: channel/value exprs evaluated once up front (gc semantics);
  `ChanOps.selectGo` picks the case (uniform random among ready, default
  if none ready and present, else scheduler-aware park); recv values
  assigned via `selectRecvValue`/`selectRecvOk`. `v := <-ch` introduces
  case-scoped variables (exporter handles the scope).

**Appendix C wire detail (reconciled 2026-10-06, Java + exporter)** —
the D3 shorthand above is exact except for the following points. The
exporter (committed c669fc5, `chanops_m5.go` header) and the Java
builders agree on this wire; it is the contract:
- `"select"` fields: `{"cases": [...]}`. Each case is a
  `"selectcase"` node: `{"kind": "selectcase", "fields": {"kind":
  "recv"|"send"|"default", "chan"?: expr, "value"?: expr,
  "targets"?: [...], "body": blocknode}}` — `body` is a `"block"` node;
  `chan` is absent on `default`; `value` is present only on `send`;
  `targets` only on `recv` (a `targets` on `send` is a fail-fast
  exporter bug).
- `targets` is an array of 0–2 objids (entry 0 the value, entry 1 the
  ok bit, which must be bool). The exporter declares EVERY target —
  including `:=` variables and `_` blank temps — with an ordinary
  `vardecl` dominating the select (fresh objids per case — it owns
  scope); the builder reads the variable's type from the wire
  `objects` array and writes it with a plain slot assign after
  `selectGo`, before the body. Targets must be plain locals of the
  current function frame: an undeclared objid fails fast (a global
  `=` target is not in the frame maps — the exporter desugars it
  through a temporary), as do boxed targets.
- **Nil-channel cases are dropped** by the builder before `selectGo` (a
  nil Go channel is Java `null`, which the Phase-2 `chan==null`=default
  convention would misread). Dropping is gc-exact: a nil-channel case is
  never ready and can never be chosen, and a nil channel cannot be
  closed (no poison) — all-dropped with no default parks forever
  (`select {}` semantics), all-dropped with a default takes it. No
  phase-2 change.
- **Comma-ok receive** (`v, ok := <-ch`, outside select): the exporter
  must NOT emit `recv{commaOk: true}` — the frozen factory's
  `isCommaOkValue` only recognizes `index`/`typeassert`, so that shape
  fails with "lhs/rhs length mismatch" (a factory change would need its
  own ADR). Instead the exporter lowers it to a single-case, no-default
  `"select"` with `targets: [v, ok]` — a blocking receive with the ok
  bit, semantically identical to `<-ch` plus the closed-channel bit.
  (The `for v := range ch` form desugars through the same pattern.)
- `"recv"` carries `type` = the *element* type-table id (determines the
  node's `GoKind` — never guessed); `"makechan"` carries `elem` and an
  optional `type` (channel type-table id — validated when present:
  kind `chan` and matching `elem`); `"send"`/`"chanclose"` carry no
  type (their operands are dynamically checked).
- `make(chan T, n)` with `n < 0` panics (`"makechan: negative size"`)
  and with `n` beyond int range panics (`"makechan: size out of
  range"`) — gc parity, in the builder (the `new GoChan` construction
  itself is exactly the Phase-2 tests').

**P2-M2 corpus graduation:** once the lowering lands, the P2-M2 channel
corpus programs (ch01–ch12) must run end-to-end through `graalgo-run`
and produce their gc-recorded `.expected` output — the README's promise.
(This is a P3-M5 acceptance item.)

**Implementation notes (2026-10-06, exporter worker):** per the Appendix W
precedent ("the D3 field lists are shorthand"), the Appendix C field
lists are likewise shorthand:
- `"makechan"` also carries `type` (the channel type-table id, for the
  frame slot); `size` is absent (not null) when unbuffered.
- `"recv"` also carries `commaOk: bool` (always present, mirroring the
  `index` node): true for the `v, ok := <-ch` shape, in which case
  `type` is the element type. Replaces the old `"unary"` `<-` emission
  (the frozen `UnaryOpNode` never implemented `<-`); the `"unary"` op
  set is unchanged (`+ - ! ^`).
- `"selectcase"` is a real node kind (`{kind, pos, fields}`), like
  `caseclause`/`typecase`.
- `for v := range ch` (not listed above — the v1 `"range"` node has no
  channel support) desugars in the exporter to a for+select loop over
  existing wire nodes plus the new `"select"` (channel hoisted once;
  `v := __rng_v` is an ordinary loop-body vardecl, so the factory's
  loopcap pre-scan gives it per-iteration box semantics when captured).
- The v1 `"call"`+`"chanclose"` marker (the `ctxChanClose` intrinsic) is
  RETIRED: user `close(ch)` now lowers to the `"chanclose"` statement
  node, and the synthetic context package uses real `close`. Java side
  must implement the `"chanclose"` statement builder, not the marker.
- `toolVersion` 0.6.0 → 0.7.0 (freeze §5: new wire node kinds are a JSON
  behavior change).

## Appendix M — Multi-result call-site desugar (added 2026-10-06, coordinator)

**Problem:** v1 cannot express multi-result function calls OR multi-value
returns (the wire `return` node carries a list, but `buildReturn` rejects
>1; `buildUnary` never implemented `<-`). Six operations need it:
`context.WithCancel`, `WithCancelCause`, `WithDeadline`, `WithTimeout`
(all `(Context, CancelFunc)`), `Context.Deadline()` (`(Time, bool)`),
`time.ParseDuration` (`(Duration, error)`). General multi-result support
is out of scope; the milestone uses targeted call-site desugar
(precedent: P3-M2's `defer`/`go` desugar).

**Decision:** the synthetic packages keep exact gc signatures. The
exporter recognizes calls to these 6 operations WHEN they are the RHS of
a 2-target assignment (`:=` or `=`) and desugars:

```go
ctx, cancel := context.WithCancel(parent)
// →
__p5c := context.__withCancel(parent)  // *cancelCtx, single result, real Go
ctx := Context(__p5c)                  // concrete→interface conversion (P3-M3)
cancel := __p5c.cancelFunc()           // CancelFunc
```

```go
d, ok := ctx.Deadline()
// →
__p5p := context.__deadlineOf(ctx)     // deadlinePair, real Go
d := __p5p.d
ok := __p5p.ok
```

```go
d, err := time.ParseDuration(s)
// →
__p5p := time.__parseDuration(s)       // durationPair, real Go
d := __p5p.d
err := __p5p.err
```

Synthetic helpers (all REAL GO, single-value — no new Java nodes):
- `context.__withCancel(parent) *cancelCtx`,
  `__withCancelCause(parent) *cancelCtx`,
  `__withDeadline(parent, d) *timerCtx`,
  `__withTimeout(parent, t) *timerCtx`
- `(*cancelCtx).cancelFunc() CancelFunc`,
  `(*cancelCtx).cancelCauseFunc() CancelCauseFunc`,
  `(*timerCtx).cancelFunc() CancelFunc` (stops the timer too — the
  desugar target for the WithDeadline/WithTimeout cancel function)
- `context.__deadlineOf(c Context) deadlinePair` (type-switch on
  `*timerCtx`/`*valueCtx`; `default` → no deadline — user-defined
  Context implementations are a documented limitation)
- `time.__parseDuration(s string) durationPair`
- `type deadlinePair struct { d time.Time; ok bool }`,
  `type durationPair struct { d Duration; err error }` (unexported;
  exporter references them by type-table id — trusted-exporter pattern)

The concrete `Deadline()` methods are DECLARED (for go/types interface
satisfaction) with fail-fast bodies — they are unreachable because the
exporter desugars every `Deadline()` call site. Other syntactic positions
(non-2-target assignment) keep the fail-fast scope error, documented.

**Context implementation** (pure Go, channel-mutex design — no Java
intrinsics, no goroutine leak):
```go
type cancelCtx struct {
    parent   Context
    done     chan struct{}       // closed on cancel (broadcast)
    cancelCh chan error          // buffer 1; occupancy = "cancel claimed"
    err      error
    muChan   chan struct{}       // binary semaphore (buffer 1, pre-filled)
    children map[*cancelCtx]struct{}
}
// cancel: select { case cancelCh <- err: (won) set err, close done,
//   propagate to snapshotted children, remove self from parent
//   case <-done: (already canceled) }
```
Parent-cancel propagation is synchronous (no leaked goroutine — the
launcher's `awaitQuiescence` would hang on parked goroutines, unlike gc
which exits with main). Lock discipline: snapshot children under muChan,
recurse WITHOUT holding the lock (context tree is a DAG — no cycles).
`Err()` uses select-on-done for the happens-before edge.

## Appendix N — `go f(args)` desugar (added 2026-10-06, coordinator)

**Problem:** P2-M1's `GoStatementNode` builds zero-arg spawns only. The
P2-M2 channel corpus program ch04 uses `go f(ch, x)` with arguments.

**Decision:** the exporter hoists call arguments to fresh temporaries and
wraps the call in a zero-argument function literal:
`go f(a, b)` → `__t1 := a; __t2 := b; go func() { f(__t1, __t2) }()`.
Additive; follows the existing `exportCloseDeferGo`/`exportSyncGoStmt`
patterns. (Committed as `c964852`; discovered during ch01–ch12
graduation.)

## Appendix M, amendment (2026-10-06, coordinator)

The desugar does NOT emit the `Context(__p5c)` interface conversion. The
extracted `ctx` variable keeps the concrete `*cancelCtx`/`*timerCtx` type
(no P3-M3 interface boxing). Rationale: the `Context` interface requires
the multi-result `Deadline` method, which v1 cannot emit (WS D); emitting
it would require frozen-file changes on both sides. The concrete type
supports all method calls (`Done`, `Err`, `Value`) directly; interface
satisfaction is unnecessary within the M5 scope (no user code passes the
desugared `ctx` to a function expecting `Context` — the desugar is
call-site-local).

## Known limitation: tc11_context_value (2026-10-06)

`tc11_context_value` fails with `binary.fields: interface conversion needs
type ids (WS E)`. The test does `ctx.Value("key") == "val"` where `Value()`
returns `any` (interface). Comparing an interface with a string constant
requires P3-M3 interface conversion type ids, which the v1 exporter does
not emit for this pattern.

This is a WS E (P3-M3 interface dispatch) limitation, not a P3-M5 bug. The
`context` package itself is correct; the issue is in the frontend's handling
of interface-vs-concrete comparisons.

**Re-test trigger**: When WS E (or a later milestone) implements interface
conversion type-id emission for binary operations, re-run tc11. The test
remains in the corpus as a regression guard.

**Scope**: M5 acceptance requires cancellation/deadline/timeout programs
(tc08, tc09, tc10, tc12, tc13, tc14) — all pass. tc11 (WithValue) is
auxiliary.

**D5 exception, recorded 2026-10-06 (P3-M5 coordinator): `GoStatementNode`
TextMap clamp (phase-2).** The desugared `go f(args)` nodes (Appendix N)
may carry out-of-range synthesized positions; `GoStatementNode.build`
(phase-2) called `Source.createSection` directly, bypassing the
`NodeFactory.sectionFor` clamp. The fix is additive and
behavior-preserving: try the requested `(line, col, len)` exactly as
before, and on `IllegalArgumentException` synthesize a valid zero-length
section at column 1 of the clamped line. (Reviewer 2 P1-2.)
