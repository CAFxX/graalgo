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
