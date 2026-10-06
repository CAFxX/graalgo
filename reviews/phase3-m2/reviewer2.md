# P3-M2 methodology review — reviewer 2 (methodology)

**Snapshot reviewed:** commits `ef830ad..39c5b77` (M2a DIV-M3-1 red+green;
M2b sync bindings; docs).
**Normative refs:** PLAN.md §4 P3-M2 acceptance, ADR-0002, INTERFACE_FREEZE.md
§1(c)/§5, reviews/phase3-m1/README.md (review-report archiving requirement).
**Date:** 2026-10-06. **Verdict: GO-WITH-FINDINGS** — 3× P2 (cosmetic/process).
Every normative claim re-executed was independently verified. All remediated
(see §Remediation).

## 1. Red-green evidence — verified genuine, both halves
**M2a:** re-ran in scratch worktrees — at `ef830ad`,
`testTm15TimerChanLenCap` FAILED (`AssertionError: tm15`, output matches
`redgreen.md` byte-for-byte); at HEAD it PASSED. Two commits, no amends.
**M2b:** all 9 exporter tests fail on pre-change trees (5 from `936b003`
fail on `0740c62`; 4 D5b tests from `c0aedef` fail on `936b003`) and pass at
HEAD — red-capable confirmed empirically.

## 2. Differential claims — confirmed by independent re-run
`run_differential.sh` → 22 passed, 0 failed (since extended to 25/25, see
remediation). Comparator meaningful: `cmp -s` on stdout bytes, exit-code
equality, first-stderr-line for panic programs. `.expected` files gc-recorded
(`regen.sh` pins `~/tools/go-1.27.1/bin`; spot-ran sy03 under gc 1.27.1 —
first stderr line matches `sy03.panic.gc`). Panic first-line comparison
adequate given the disclosed ADR-0002 D4 scoped divergence; `.panic.gc`
keeps gc's line for the record.

## 3. Freeze-exception process — sound
Precedent claim accurate (`2e5f012`: P2-M1 phase-1 exception, additive
`gostmt` kind, toolVersion 0.1.0→0.2.0). Freeze §2.3 respected (additive
kinds, wire v1, version-only golden diffs); §5 respected (toolVersion bump).
`INTERFACE_FREEZE.md` untouched; the ADR records the §2.1 staleness per
§1(c). Live forward-compat proof: pre-0.3.0 `NodeFactory` rejects new JSON
with `unsupported expression kind "syncnew" (WS D hook:
NodeFactory.registerExprKind)`. M2b touched zero `phases/phase2/src` files
(M2a's phase-2 edits are the documented exception — see code review P2-5).

## 4. Docs consistency — consistent, two count errors (P2)
ADR-0002 ↔ SYNC_DESIGN.md ↔ corpus README ↔ REPORT.md discharge agree on the
divergence table, re-test triggers, and 25/25. DIVERGENCES.md marks DIV-M3-1
FIXED with evidence pointers.
- **P2-1 (docs):** `398c166` message says "12 Go programs (sy00-sy12)" —
  there are 13 `.go` files (sy00–sy12). **Correction (no amend possible):**
  the corpus is 13 programs; the runner's 22-run arithmetic (4 panic ×1 +
  9 non-panic ×2) is consistent with 13. Now 16 programs (sy00–sy15), 25
  runs (7 panic ×1 + 9 non-panic ×2).
- **P2-2 (docs):** `c0aedef` message says "(8 sync tests total, all pass)" —
  the file had 9 test funcs at that commit. **Correction:** 9.

## 5. GG activation — satisfied
`verifies: GG-0017` on `SyncNewNode`, `SyncOpNode`, `P3SyncMain`; PLAN.md's
activation policy met (differential corpus + exporter tests are the landing
tests).

## 6. Scope-error honesty — verified live
`struct { mu sync.Mutex }` → `sync.Mutex as a struct field is not in M2
scope`; `m.TryLock()` → `sync.Mutex.TryLock is not in M2 scope`; `&m` on a
sync var → pre-existing WS D scope error (honestly documented in
SYNC_DESIGN.md §7.3). All fail-fast, named, none silent.

## 7. Old-gofrontend behavior — no silent change
Pre-0.3.0 tree rejects `import "sync"` with the WS G scope error
(demonstrated in the red run). ADR's claim holds.

## Findings
- **P2-1** (docs): commit-message count "12" vs 13 programs. **Remediation:**
  corrected in this report (§4); runner math already consistent.
- **P2-2** (docs): commit-message count "8" vs 9 tests. **Remediation:**
  corrected in this report (§4).
- **P2-3** (test robustness): `TestSyncDeferArgsAreParams` panicked (nil
  interface conversion) instead of failing cleanly on the pre-D5b tree —
  still red, but aborts the test binary and hides siblings. **Remediated
  `f6800ad`**: navigation nil-safe via `mustNodeField` (clean `t.Fatal`);
  verified a bad shape FAILs without panicking.

## Remediation
P2-3 remediated in `f6800ad`; P2-1/P2-2 corrected in this archived report
(commits are immutable — no amend). Re-tested: phase-1 208/208, phase-2
194/194, p3frontend green, differential 25/25.
