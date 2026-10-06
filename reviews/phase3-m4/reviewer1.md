# Code review — graalgo P3-M4 (GG-0020)

**Reviewer:** R1 (code review), independent
**Date:** 2026-10-06
**Snapshot reviewed:** `6d690e9` (P3-M4 implementation complete)
**Verdict:** **NO-GO** — 1 P0, 2 P1, 5 P2 below.

**Remediation:** every finding was remediated after this review; see the
"Remediated at" note per finding. This report is committed verbatim as
the review record; nothing in it was softened.

---

## P0 — must fix

### P0-1. Atomic cell cloning severs `*atomic.T` aliasing

`Boxing.java:99,102` (`copyForAssign`) and `GoFuncRootNode.java:139,141`
clone `AtomicLong`/`AtomicInteger` unconditionally, but the D5 carrier
of a `*atomic.Int64` IS the same cell object.

Reproducer (verified vs gc 1.27.1):

```go
p := new(atomic.Int64); setIt(p); p.Load()
// func setIt(p *atomic.Int64) { p.Store(9) }
```

gc prints `9`, graalgo prints `0`. Also:

```go
p := new(atomic.Int64); q := p; q.Store(5); p.Load()
```

gc prints `5`, graalgo prints `0`.

Fix direction: gate both clone sites on `kind == GoKind.STRUCT`
(mirroring the `GoStruct` branch directly above). Add corpus cases:
`*atomic.T` pointer param, pointer copy, interface-held pointer (the
existing `at02` only covers the value form).

**Remediated at:** `c9a4737` — both sites gated on STRUCT; new corpus
`at03_atomic_pointers` (pointer param, pointer copy, interface-held
pointer, by-value-still-clones anchor); red-green R1; atomic corpus
8/8 vs gc 1.27.1.

---

## P1 — should fix

### P1-1. `fastrandn(0)` diverges

`Intrinsics.java:534` throws `IllegalArgumentException` for `n<=0`;
gc 1.27's `randn` has no check — Lemire multiply-shift with n=0
returns 0. gc prints `fastrandn(0) = 0`, exit 0; graalgo dies with a
fatal Java exception, exit 1.

Fix: drop the guard so it returns 0 like gc. Add a corpus case.

**Remediated at:** `6f6b25c` — guard dropped (body change only); new
corpus `fr03_zero`; red-green R2; fastrand corpus 8/8 vs gc 1.27.1.

### P1-2. Innermost runtime panic frame reports the function *definition* line

`IndexWriteNode.java:45`, `IndexNode.java:58` call
`Panic.throwPanic(...)` with no throw-site node → `renderGoStack`
falls back to the frame's funcdecl section.

Verified: nil-map write inside `helper()` → graalgo reports `helper()`
at `main.go:5` (def line), gc reports `main.helper()` at `main.go:7`
(the panicking statement).

Fix: pass throw-site nodes at runtime panic sites. Extend the map
harness to compare full normalized stderr, not just the first line
(this is also P2-4 below — fixing the harness is part of this fix).

**Remediated at:** `81cade7` — throw-site nodes at every runtime
panic site in the frontend nodes (plus phase-3 nodes and the deferred
`panic(x)` path); map harness compares full normalized stderr; new
corpus `mp04_nil_map_inner` (the reproducer); red-green R3; map
corpus 9/9 vs gc 1.27.1.

---

## P2 — fix or justify

### P2-1. `Intrinsics.traceback()`/`itabFor()` are Go-unreachable

No wire node, no exporter support, no Go-callable path. Make them
Go-reachable (wire nodes + exporter support) OR honestly reclassify in
the audit with a re-test trigger. Also fix the latent two-namespace
issue if both paths persist: the intrinsic path mints iface ids from a
process counter starting at 1 while the dispatch path uses wire
type-table ids — two namespaces sharing the `itabs` cache key. Add a
cross-path agreement test with independently-minted ids.

**Remediated at:** `7ac7cde` — honest reclassification chosen
(audit §2c: REAL as Java intrinsics, no Go wire path, explicit
re-test trigger); two-namespace issue fixed (one namespace: wire
type-table ids via `IfaceWireEntry`); cross-path agreement tests with
independently chosen wire ids (42/43); red-green R5 (collision probe
showed `COLLISION: B got A's itab` on the old code).

### P2-2. `Linknames.GO_FUNCS` never cleared

`LinknameIntrinsics.install` claims "a previous program's bindings can
never leak into this run" but never calls `Linknames.clear()`. Fix the
code (call clear) or fix the comment — pick the honest one and say
which.

**Remediated at:** `7ac7cde` — fixed the code (the honest one): the
comment was right, the code was wrong; `install` now calls
`Linknames.clear()` first. Red-green R4 (program B printed A's stale
`42` before the fix; fails fast after). Checked-in test
`LinknameEndToEndTest.testNoLeakAcrossProgramsInOneJvm`.

### P2-3. `callGo` (intrinsic→Go) has no checked-in end-to-end test

Check one in (redgreen #1's GREEN "prints 42" is a candidate).

**Remediated at:** `7ac7cde` — checked in
`LinknameEndToEndTest.testCallGoEndToEndPrints42`: intrinsic→Go with
a concrete arg for an any-typed parameter prints 42.

### P2-4. Map harness compares only the first stderr line for panic programs

Fix: compare full normalized stderr (part of P1-2's fix).

**Remediated at:** `81cade7` — full normalized stderr compared
(gc-vs-graalgo live + graalgo-vs-`.panic` anchor); `.panic.gc` is
gc's full normalized stderr; regen.sh updated.

### P2-5. `GoFuncRootNode` arg-unwrap relies on an implicit contract

Assert the invariant somewhere real (a debug assert or a checked-in
test), don't just document it.

**Remediated at:** `7ac7cde` — fail-fast `AssertionError` in
`GoFuncRootNode.execute` after the unwrap (concrete params must carry
D5 values, never interface boxes); covered by the checked-in callGo
end-to-end test.

---

## Verified honest

- The P0-1 and P1-1 reproducers were verified against gc 1.27.1
  before the fix (graalgo `0`/`0` vs gc `9`/`5`; graalgo fatal
  exception exit 1 vs gc `fastrandn(0) = 0` exit 0).
- The P1-2 line-number divergence was verified against gc
  (`helper()` at def line vs at the panicking statement).
- No finding was redefined or softened during remediation; where the
  review offered "fix the code or fix the comment", the choice taken
  is recorded above (P2-2: fixed the code).
