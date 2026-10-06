# P3-M2 code review — reviewer 1 (code)

**Snapshot reviewed:** `e0c07a5..39c5b77` (the brief named `35fe8f6..39c5b77`, which
contains only the docs commit `39c5b77`; the 9 substantive commits
`ef830ad..35fe8f6` are its ancestors — the full range was reviewed so the
review covers the described work, never the live tree).
**Normative refs:** ADR-0002 (judged implementation-vs-design deviation, not the
design), INTERFACE_FREEZE.md §§1/2/5, SYNC_DESIGN.md.
**Date:** 2026-10-06. **Verdict: NO-GO** — 3 P0 findings, all empirically
reproduced. All remediated (see §Remediation).

## P0 findings (wrong behavior)

### P0-1: `RWMutex.Unlock` misuse escapes as host exception, not Go panic
`phases/phase3/src/main/java/graalgo/p3sync/nodes/SyncOpNode.java:79` —
`case "RWMutex.Unlock"` not wrapped in `panicOnMisuse` (neighbors are).
Reproducer `var rw sync.RWMutex; rw.Unlock()`: graalgo
`fatal internal error… IllegalStateException: sync: unlock of unlocked RWMutex`,
exit 1; gc: `fatal error: sync: Unlock of unlocked RWMutex`, exit 2.
No corpus program covered the RWMutex *write*-unlock path (sy05 covers
`RUnlock` only). **Remediated `72c4432`** (wrap + sy13 corpus).

### P0-2: `WaitGroup.Done()` on zero counter escapes as host exception
`SyncOpNode.java:83` — `case "WaitGroup.Done"` not wrapped in
`panicOnMisuse`. Reproducer `var wg sync.WaitGroup; wg.Done()`: graalgo
exit 1 host exception; gc `panic: sync: negative WaitGroup counter`, exit 2
(ADR claims full parity here). sy08 covers `Add(-1)` only, not `Done()`.
**Remediated `72c4432`** (wrap + sy14 corpus).

### P0-3: `exportSyncGoStmt` evaluates `go m.Op(args)` args in the new goroutine
`phases/phase1/frontend/export.go:2628` — the desugar `go func(){ …Op(…) }()`
captures argument *variables* by reference. Go spec: "the function value and
parameters are evaluated as usual in the calling goroutine." Reproducer
(`n := 1; go wg.Add(n); n = 2; …`): gc deterministically panics, graalgo
printed `survived` 3/3. The code comment's "race-free indistinguishability"
justification is false. Zero end-to-end corpus coverage (wire-shape unit test
only). **Remediated `3c163d5`**: `exportSyncGoStmt` now emits
`block{vardecl(hoists), gostmt(zero-arg spawn)}`; args hoisted to
`__sync_go_arg<seq>_<i>` temporaries at go-time; pointer/non-ident receivers
hoisted to `__sync_go_recv<seq>`; value-typed ident receivers keep
by-reference capture (gc's `&m` semantics, sy12-proven). `exportSyncOp`
refactored to wrapper + `exportSyncOpNodes`; phase-2 `GoStatementNode`
untouched; false comment corrected. New `TestSyncGoArgsHoisted`,
`TestSyncGoPointerRecvHoisted`; `TestSyncGoUnlockDesugars` updated.

## P1 findings

### P1-1: `TestSyncSyntheticSignatures` is self-referential
`phases/phase3/src/p3frontend/sync_test.go:21` — builds the mirror from
`syncMethodSpecs`, asserts against the same table; signature-shape drift
would not be caught. **Remediated `ab00108`**: new
`TestSyncMirrorMatchesRealGorootSync` type-checks real `$GOROOT/src/sync`
via the go/types source importer, asserts method set + pointer receivers +
signatures order-independently. It immediately flagged a real (documented)
simplification: the mirror's `RLocker` returns anonymous
`interface{Lock();Unlock()}` vs gc's named `sync.Locker`; the oracle
normalizes named interfaces to method sets (structural identity; the frozen
exporter deliberately synthesizes it anonymous). Mutation-verified.

### P1-2: `GoRWMutex.unlock()` message differs from gc (`unlock` vs `Unlock`)
Phase-2 `GoRWMutex.java:89`: `"sync: unlock of unlocked RWMutex"`; gc 1.27.1
`rwmutex.go:212`: `fatal("sync: Unlock of unlocked RWMutex")`. Phase-2 code —
not fixable without a phase-2 change; ADR-0002 D4's "gc's message text" is
inaccurate for this op. **Remediated `2bb8c18`**: message fixed to gc parity
(one word, fatal path, no test pinned the old text); sy13's `.panic` updated.
Signed-off phase-2 file change — authorized under the M2a precedent, noted in
the commit message and the checkpoint entry.

### P1-3: `go m.Op()` has no end-to-end differential coverage
Corpus used only `go func(){...}()`. **Remediated `3c163d5`**: sy15
(`go wg.Add(n)` arg-timing reproducer shape) added as a panic program.

## P2 findings

- **P2-1** — `export.go:2463-2471`: `exportSyncDefer` doc comment pasted 3×.
  **Remediated `5e6c34e`** (deduped).
- **P2-2** — `corpus/sync/regen.sh` header claims it writes `.panic` files;
  it writes only `.expected`/`.panic.gc`. **Remediated `5e6c34e`**.
- **P2-3** — `torture/REPORT.md` discharge cites "SYNC_DESIGN.md §7.5"; §7 has
  items 1–5. **Remediated `5e6c34e`** (§7 item 5).
- **P2-4** — ADR-0002 "Verification" says phase-2 193/193; post-DIV-M3-1-fix
  it is 194/194. **Remediated `5e6c34e`**.
- **P2-5** — "No `phases/phase2/` source is modified" (ADR-0002 §4) and "NEVER
  modifies their sources" (phase-3 Makefile header) are contradicted by M2a's
  phase-2 edits (`GoChan.java` un-final'd, `GoTimer.java`, new `TimerChan.java`
  + phase-2 test). The edits are behavior-preserving (re-grep verified;
  194/194 green). **Remediated `5e6c34e`**: both statements rewritten — freeze
  stands; M2a and P1-2 recorded as the two documented exceptions; future
  phase-2 edits get their own ADR. (Process question for the supervisor:
  answered in the checkpoint entry.)
- **P2-6** — nil-receiver / nil-`Do`-func panics render as
  `panic: GoErrorValue{runtime error: …}` under `P3SyncMain` vs gc's
  `panic: runtime error: …` (pre-existing phase-2 `recordPanic` printer;
  out of this diff's scope). Noted, not fixed.

## Verified good (no findings)

Synthetic signatures exact vs gc 1.27.1 (all types/methods incl. out-of-scope
ones, pointer receivers); D2 single interception choke point (dot-imports,
aliases, parenthesized receivers behave; method values → WS H scope error;
method expressions → gc-identical go/types error); D3 scope errors
(struct/array/slice/map) + `*sync.X` allowed; package-level and local
`var m sync.Mutex` → implicit `syncnew`; wire contract (`syncop.type` null
for void; `kindForType` derived, fail-fast); old consumers reject new kinds
fail-fast verbatim per ADR; D1 value model (`copyForAssign` only deep-copies
`GoStruct`); D5 Once.Do winner-path mirrors `FuncValueCallNode`
(`recoverAllowed` dance; panic-counts-as-done; nil func → nil-dereference);
D5b defer desugar gc-exact (sy12); freeze bounds (all `export.go`/`main.go`
hunks inside ADR-0002's exception; toolVersion 0.2.0→0.3.0; goldens
version-only; wire v1); DIV-M3-1 masking contract (re-grep) + 194/194;
corpus+runner (8 `SyncStressTest` legs ported + 5 extras; runner compares
stdout bytes/exit codes/first stderr line; 22/22 re-run green, spot-runs
byte-match); loader synthetic resolution; `make -C phases/phase3 check`
clean. Two pre-existing, unrelated `go build`/`vet` failures in
`phases/phase1/frontend/go/token` and `javatest` (zero diff files touch them;
phase-1 suite 208/208 green regardless).

## Remediation
All findings remediated in 6 one-change-per-commit commits (`72c4432`,
`3c163d5`, `ab00108`, `2bb8c18`, `f6800ad`, `5e6c34e`), re-tested:
phase-1 208/208, phase-2 194/194, p3frontend `go test` green,
`run_differential.sh` 25/25, `make -C phases/phase3 check` clean.
Reviewer did not fix anything; remediation by a separate worker, verified
below by the coordinator's independent re-runs.
