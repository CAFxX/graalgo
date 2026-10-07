# P3-M8 patch integration plan (coordinator, 2026-10-07; rev.2 post-methodology-review)

All staged patches are new-file-safe; existing-file edits apply at rebase
time (after M7 remediation lands) in the order below. Pre-tested on
scratch worktrees at 720731d. Rev.2 incorporates methodology-review
findings F1–F6 (reviewer2-methodology.md).

## Patch repairs done by coordinator (pre-integration)

1. os/*.patch: bare filenames -> repo-root-relative a/b paths (6 files).
2. unsafe/01,05,06,07: hunk-header counts corrected (4 headers).
3. unsafe/01: REGENERATED via git diff (was hand-written, content mismatch).
4. unsafe/02: REGENERATED via git diff (was hand-written with literal
   `\t` sequences instead of tabs — completely unappliable as written).
5. unsafe/04: anchor headers resolved to line numbers (was prose headers).

## Known conflicts requiring merge at integration (all mechanical, all additive)

### C1. export.go allow-list + P3-M4 block (THREE patches, one region)
- unsafe/01 hunk 2 DELETES the P3-M4 non-blank-unsafe block (708-713).
- io/02 hunk (allow-list) INCLUDES the P3-M4 block as context.
- os/02 hunk (allow-list) INCLUDES the P3-M4 block as context.
- Resolution: apply unsafe/01 FIRST, then merge the allow-list additions
  (io/02 adds "io"; os/02 adds "os","io/fs") into a single hunk against
  the post-01 state. Final allow-list: ...existing... + io + os + io/fs.
  P3-M4 block gone (unsafe is real now).

### C2. synth_machinery.go syntheticSources map (io/01 vs syscall/01 vs reflect/01 vs crypto/01 vs os/01)
- Five patches add adjacent map entries (io, syscall, os, io/fs,
  reflect, 14 crypto packages; unsafe is programmatic, not in the map).
  Resolution: sequential apply with --3way or manual merge (additive, no
  key overlap — verified: no duplicate keys).

### C3. synth_machinery.go:499 import gate (unsafe/02 + os/01 + reflect/01) [CORRECTED rev.2]
- The three patches each extend the SAME gate line with a different
  `&& path != "…"` clause:
  - unsafe/02: `&& path != "unsafe"`
  - os/01: `&& path != "os" && path != "io/fs"` (new line)
  - reflect/01: `&& path != "reflect" && path != "internal/reflectlite" && path != "iter"` (new line)
- syscall/01 does NOT touch this gate (verified — its hunks are at 66/94).
- Resolution: single merged conjunction (order: unsafe, os, reflect):
  `... && path != "encoding/json" && path != "unsafe" &&`
  `path != "os" && path != "io/fs" &&`
  `path != "reflect" && path != "internal/reflectlite" && path != "iter" {`
- A merge omitting reflect's clause would silently strand reflect at the
  import gate (methodology-review F2).

### C4. mrdesugar_m5.go desugar tables (io/04 vs os/03 vs syscall/03 vs reflect/04 vs crypto/03)
- Five parallel mechanisms, adjacent additions, no semantic overlap
  (different packages; verified: no duplicate (pkg,type,method) triples):
  io/04 (mrM7-style entries), os/03 (new osMultiResultFunc table +
  classifier), syscall/03 (28 Appendix-M entries), reflect/04, crypto/03
  (m8MultiResultFuncs table).
- Resolution: sequential apply, manual merge.

### C5. export.go import overlay (io/02 vs syscall/02)
- Both extend the overlayImporter / import gates. Resolution: sequential
  apply, manual merge (additive).

### C6. P3SyncMain.java triple registration (os/05 + unsafe/05 + reflect/05) [NEW rev.2]
- All three insert registration blocks at the SAME anchor (after the
  `mathfma` registerExprKind, before the P2-M1 go-statement comment):
  - unsafe/05: 8 `factory.registerExprKind("unsafe…")` + `UnsafeTable.install()`
  - reflect/05: `factory.registerExprKind("reflectop", …)`
  - os/05: argv handling (hunks at 23/94/107/255/421 — disjoint from the
    226/227 anchor region)
- Verified: no duplicate kind names (reflect registers only "reflectop").
- Resolution: apply unsafe/05, then reflect/05's block immediately after
  (order between the two is semantically irrelevant); os/05's hunks are
  in disjoint regions and apply independently.

### C7. ParseComments duplicate (os/01 vs syscall/01) [NEW rev.2]
- Both patches make the IDENTICAL behavioral edit:
  `parser.ParseFile(..., parser.AllErrors)` →
  `parser.ParseFile(..., parser.AllErrors|parser.ParseComments)`
  (only the comment text differs).
- Resolution: keep os/01's hunk (owner), DROP the hunk from syscall/01
  at integration. The change affects all synthetic sources; add a
  regression check that doc-comment attachment does not alter export of
  the existing M7 synthetic packages.

### C8. synth_machinery.go 3-way overlap: isMultiResultSynthetic tail + two gates (io/03 × os/01 × reflect/01) [NEW: reviewer1 P1-9]
- Three sites in `synth_machinery.go` are touched by all three patches
  but were not listed in C1–C7:
  1. Tail of `isMultiResultSynthetic` (the package-path allow-list):
     io/03 adds `&& p != "io"`; os/01 and reflect/01 extend the same
     conjunction.
  2. `exportSyntheticPkgCall` gate: os/01 × reflect/01.
  3. `multiResultSyntheticError`: 3-way (milestone-tag logic).
- Verified 2026-10-07: applying io/03 then os/01, the reflect/01 hunk
  FAILS `git apply --check` (stale context — reflect/01 was generated
  against pristine base).
- Resolution (at integration, fixed order io → os → reflect): after
  applying io/03 and os/01, REGENERATE reflect/01 via `git diff` on the
  scratch tree (machine-generated discipline), then apply. Update this
  section if the rebase moves the base.

## Integration order (at rebase)
1. unsafe/01 (stub removal) — first, unlocks C1.
2. Merge C1 (allow-list: io + os + io/fs; P3-M4 block already gone).
3. unsafe/02,04,05,06,07.
4. io/01..08 (merge C2/C5 as encountered).
5. syscall/01..03 (C7: drop the ParseComments hunk; merge C2/C4/C5).
6. os/01..06 (merge C1 remainder, C3, C4, C6).
7. reflect/01..06 (merge C2, C3, C4, C6).
8. crypto/01,03 (merge C2, C4).

## Ordering constraint (F5)
syscall/01's header warns: the synthetic syscall source imports `io/fs`,
so the os worker's io/fs registration (R5) must land first or together.
At rebase (single integration) this is satisfied by the order above
(os/01 before any graalgo-leg execution), but the constraint is stated
here explicitly.

## REBASE GATE (F12 — P0 sign-off precondition)
At rebase, BEFORE merging:
1. Re-run `git apply --check` of all 31 patches against the REMEDIATED
   tree (numbered slots run to 33; unsafe/03 and crypto/02 are documented
   numbering gaps, see §Patch numbering notes). The M7 remediation may
   have touched export.go, synth_machinery.go, mrdesugar_m5.go,
   P3SyncMain.java.
2. Regenerate any patch whose context is stale (git diff, never hand-edit).
3. Re-resolve C1–C7 against the remediated base.
Only then merge, then: full wave-2 corpus on BOTH legs + full phase
suites (1/2/3) + p3frontend + `make -C phases/phase3 check` before any
sign-off proposal. Zero graalgo legs have run to date — no sign-off is
possible until this gate passes.

## Semantic coherence (verified)
- No duplicate syntheticSources keys across workers.
- No duplicate desugar (pkg,type,method) triples across workers.
- No contradictory fail-fast messages for the same API.
- No duplicate linkname symbol registrations.

## Process lesson
Staged patches MUST be machine-generated (`git diff`), never hand-written.
The unsafe worker's hand-written patches had: wrong hunk counts, literal
`\t` instead of tabs, prose `@@` headers, and `#` comments breaking
git apply — all while claiming "git apply --check clean". Added to the
worker brief template: patches are verified by the COORDINATOR on a
scratch worktree, not by the worker's claim.

## Patch numbering notes (F14)

- unsafe/03: skipped — was the R11 const-context pre-pass patch;
  dropped when R11 was superseded (go/types folds via *types.Builtin).
- crypto/02: skipped — renumbering during staging (02 was to be the
  desugar patch, landed as 03).
