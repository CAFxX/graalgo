# Phase 3 closeout — Stdlib bring-up (Linux-first)

**Date:** 2026-10-13 · **Lane:** m10-doc-audit (WS J) · **Base:** master `855f8b03`
**Status:** M1–M9 delivered and merged. P3-M10 (WS H/I/J) in progress:
cross-doc audit COMPLETE (this document + the fixes listed in §6);
gates 1–5 adjudicated below (§4) — 0 of 5 fully met, 1 partial, 4 open
with lanes in flight or unassigned.

This is the Phase-3 closeout record. The per-milestone evidence lives in
[CHECKPOINT.md](CHECKPOINT.md) (append-only); the plan in
[PLAN.md](PLAN.md); the five gate definitions in PLAN.md §6 (from
PHASES.md §5).

---

## 1. Delivered per milestone

| Milestone | Delivered | Sign-off |
|---|---|---|
| P3-M1 (WS A) | Frontend package dirs + import resolution: `ExportDir`/`MergeDir`, graduated `types.Importer`, build-tag filtering verified file-for-file vs `go/build` oracle, package flattening; `func init()` fail-fast scope error | 2026-10-06, 2 reviews remediated |
| P3-M2 (WS D) | `sync` Go bindings onto Phase-2 Java primitives; DIV-M3-1 fixed red-green-first (`len()` on buffered timer channel = 0, gc parity); P2-M8 stress re-run as Go programs differential vs gc | 2026-10-06, 2 reviews remediated |
| P3-M3 (WS B) | Interface dispatch: PIC on itab identity (limit ~3, megamorphic hash fallback), type switches via decision trees; M9 polymorphism re-measured (31 sites, 1227 dispatches: 41.9% mono / 51.6% poly / 6.5% mega) | 2026-10-06, 2 reviews remediated |
| P3-M4 (WS C) | Intrinsic surface completion: GG-0020 audit published (stub-vs-real over the ~60–70 fn surface, all real); map/slice/atomics/fastrand/traceback differential vs gc; linkname by symbol name; freeze exceptions via ADR-0004 | 2026-10-06, 2 reviews remediated |
| P3-M5 (WS E) | `time` onto Phase-2 timers (DIV-M3-1 verified in the loop), `context` via P3-M3 dispatch; cancellation/deadline programs differential vs gc | 2026-10-06/07, 2 reviews (reports never archived — process gap, §5 F-4) |
| P3-M6 (WS F) | Stdlib fork mechanics: vendoring pipeline, linkname-diff tooling + trial, asm per-category strategy (238 evaporate / 61 intrinsify / 71 emulate-then-intrinsify), exclusion list v1→v2 (versioned, reason-coded, cap 13), coupled-package set re-measured (142 dirs; P3-D1 STANDS per ADR addendum) | 2026-10-07, 2 reviews remediated |
| P3-M7 (WS G) | Package wave 1: errors/sort/strings/bytes/strconv/math/encoding/* `go test` green vs gc (76 passed, 3 scope-limited, 0 failed + encoding legs 14/18); wordfreq demo byte-identical to gc | 2026-10-07, 4 reviews (2 rounds) remediated |
| P3-M8 (WS G) | Package wave 2 — M8a: io + WS-D multi-result dispatch + D2 desugar (accepted 2026-10-07); M8b: os, syscall, reflect/unsafe, crypto (accepted 2026-10-09). Differentials byte-identical vs gc both gmp legs; crypto/hash,hmac,rand scope-limited (VM __hashWrite dispatch bug) | M8a/M8b accepted, supervisor verification |
| P3-M9 (WS G) | **Scope re-planned 2026-10-07** (was: full wave-3 `go test`): netpoller runtime (3 P0s found/fixed in review), io/fs + path/filepath, x509/TLS (GMP=1/2 handshakes + app-data echo byte-identical vs gc 1.27.1), 19 P0 bugs fixed/verified, wave-3 corpus baselined under gc (net/http, archive/tar/zip, mime/*, compress/*), 3 independent reviews + acceptance | Merged to master 2026-10-13 (`64705cc`) |
| P3-M10 (WS H/I/J) | In progress: 5 lanes (doc-audit, testing-layers, snapshots, diff-tooling, self-attack). This lane: audit + gate adjudication + closeout | — |

**Not delivered (deferred with record):** the full `go test ./...` run
(no `go test` trial infrastructure exists — M9 acceptance: "NOT
IMPLEMENTED"); crypto intrinsify leg (deferred to Phase 5+, ADR-0007);
`net/*` edge packages beyond triage scope; darwin/Windows (GG-0023).

---

## 2. Key metrics (all cited to evidence)

- **Phase-1 suite:** 214 test methods in tree. **Currently RED on master**
  — `FreezeSignatureTest.testFrozenSignatures` fails: the P3-M9 compress
  lane (`f4915c2`, 2026-10-11) added a `GoStructDescriptor elemDesc`
  parameter to the frozen `Intrinsics.makeslice`/`growslice` signatures
  with no ADR and no freeze version bump (freeze policy §1 violation;
  finding F-1 below). Was 214/214 green at P3-M7 acceptance (2026-10-07).
  **Empirically verified 2026-10-13 on master `855f8b03`:** compiled
  `Intrinsics.class` and ran the test's exact reflection checks —
  `makeslice(GoKind,int,int)` MISSING (actual:
  `makeslice(GoKind,GoStructDescriptor,int,int)`),
  `growslice(GoSlice,GoKind,int,long,boolean)` MISSING (actual:
  `growslice(GoSlice,GoKind,GoStructDescriptor,int,long,boolean)`); the
  3-arg `growslice` overload is present. The test deterministically fails.
- **Phase-2 suite:** 194/194 green (M9 acceptance log). 193 at P2-M10
  sign-off; +1 from P3-M2a's DIV-M3-1 regression test (`ef830ad`).
- **Phase-3 suite:** 95/95 at P3-M8b acceptance.
- **M9 acceptance differentials:** 70 wave samples (wave1 31/31, wave2
  32/32, wave3 7 passed + 1 timeout reclassified as harness limit).
  **Correction:** the acceptance report's "Phase 1: 180/182" is wrong —
  its own log says "214 tests, 3 failures" (it missed the
  FreezeSignatureTest failure); see §5 F-2.
- **x509/TLS:** GMP=1/2 handshakes + application-data echo byte-identical
  vs gc 1.27.1. The reported "TLS hang" was a harness-timeout false alarm
  (90s timeout vs ~135s frontend JSON generation).
- **P0 fixes:** 19 P0 bugs fixed/verified in the M9 batch (36 P0-tagged
  commits on master across Phase 3).
- **Exclusion list:** v2, 2 entries, under the approved cap (13 =
  5% of 274 test binaries). Enforced by `stdlib-excl validate` in
  `make -C phases/phase3 check`.
- **Coupled packages:** 142/399 dirs re-measured on go1.27.1 (was ~78
  from an imprecise earlier check); P3-D1 STANDS.
- **CI:** green on per-push (`make all`: generate + build + check +
  test-ci smoke). The full `make test` (30+ min) runs weekly/on-demand —
  it is the suite that is red (F-1).

---

## 3. Gates 1–5 adjudication (PLAN.md §6)

| # | Gate | Verdict | Evidence / what's missing |
|---|---|---|---|
| 1 | `go test ./...` green, zero unexplained divergences; exclusion list versioned, reason-coded, under cap | **NOT MET** | No `phases/phase3/hidden_files/stdlib/GO_TEST_ALL.md`. No `go test` trial infrastructure exists (M9 acceptance: "Upstream go test Trial: NOT IMPLEMENTED"). **No lane assigned** — flagged to parent. The exclusion-list half is met (v2, 2 entries, cap 13, mechanically enforced). |
| 2 | Four testing layers operational (smoke → conformance → stdlib+script → fuzz soak) at their cadence | **NOT MET** | No `phases/phase3/hidden_files/testing/LAYERS.md`. Lane `m10-testing-layers` in flight (no commits yet as of 2026-10-13). |
| 3 | Behavior snapshots recorded for go1.27.1 (upgrade baseline) | **NOT MET** | No `phases/phase3/hidden_files/snapshots/`. Lane `m10-snapshots` in flight (no commits yet). |
| 4 | ≥2 independent reviews per package group, all findings remediated + re-tested; self-attack (I/O fault injection, signal storms, fd exhaustion) green | **PARTIAL** | Reviews on master: m1 (supervisor reconstruction — originals never archived), m2, m3, m4, m6, m7. Gaps: m5 reports never archived (checkpoint falsely claimed them — corrected, §5 F-4); m8 reviews live under `hidden_files/m8-reviews/` + supervisor direct review (4 dispatched reviewers died in VM reboots); m9 reviews (`reviews/correctness-findings.md`, `generality-findings.md`, `robustness-findings.md`) + `acceptance/` exist only on unmerged lane branches. Self-attack: lane `m10-self-attack` in flight (no commits yet). |
| 5 | API/ABI diff tooling produces a scoped work list on a trial re-run | **NOT MET** | No `phases/phase3/hidden_files/apidiff/TRIAL.md`. Lane `m10-diff-tooling` in flight (no commits yet). |

**Bottom line:** Phase 3's implementation milestones (M1–M9) are delivered
and merged, but the phase's done-gates are not yet met. The gates are
mechanical and evidence-shaped; four have lanes in flight, gate 1 has none.

---

## 4. Open items for Phase 4 (with owners where known)

1. **F-1 — `FreezeSignatureTest` red on master (CODE BUG, reported not
   fixed per lane mandate).** `Intrinsics.makeslice(GoKind,int,int)` and
   `growslice(GoSlice,GoKind,int,long,boolean)` are frozen (freeze v1 §3.4,
   `docs/freeze-stub.md`, checked by `FreezeSignatureTest`); the P3-M9
   compress lane changed them to 4- and 6-param forms (`elemDesc`) with no
   ADR and no version bump. Fix options (Phase-4 owner's call): (a) record
   the change properly — ADR + freeze v1→v2 + update the test's frozen
   list; or (b) revert the signatures and thread `elemDesc` differently.
   Either way the test must be green before any Phase-4 sign-off claims a
   green tree. **Phase-1 `make test` is red until this is fixed; per-push
   CI does not catch it** (only `test-ci` smoke runs per push).
2. **Gate 1 needs a lane.** The `go test` trial infrastructure (p3test)
   does not exist. Recommend: staff it as the first Phase-4 work item per
   the repo-restructure proposal (it is Phase 4's primary conformance
   corpus anyway).
3. **M9 review/acceptance docs are on unmerged branches.**
   `wt/f7467b85-7da6-4da0-b9ec-94631a60dafe/m9-review-correctness`
   (`reviews/correctness-findings.md`), `/m9-review-generality`
   (`reviews/generality-findings.md`), the robustness review
   (`reviews/robustness-findings.md`), and `/m9-acceptance`
   (`acceptance/`, incl. the raw logs). Integrate or archive them — the
   findings are remediated on master but the evidence is not.
4. **Acceptance-report accuracy (F-2).** The M9 acceptance report's
   numbers contradict its own logs (180/182 vs 214/3 fails; missed the
   FreezeSignatureTest failure; wave-3 7/0 vs 7+1 timeout). Future
   acceptance runs must cite log lines, not hand-copied totals (standing
   lesson: "no sign-off on stale numbers" — extend to "no sign-off on
   hand-copied numbers").
5. **P3-M5 review reports are unrecoverable (F-4).** The findings and
   remediation stand as recorded in CHECKPOINT.md; the original reports
   are gone. Process fix (for Phase 4): review reports are committed
   before sign-off, mechanically checked (the m1 gap got the same note;
   it recurred at m5 — the check was never built).
6. **Carried re-test triggers:** 38µs cold-drainer wakeup (unexplained,
   since Phase 0); latency-distribution server-hardware re-measure;
   crypto intrinsify leg (Phase 5); `net/*` edge-package triage;
   wave-3 `.scopelimited` corpus retests (synthetic net/http, archive,
   socket/netpoll).
7. **M10 sibling lanes** (testing-layers, snapshots, diff-tooling,
   self-attack) must land their gate evidence and integrate before the
   phase can be declared complete.

---

## 5. Cross-doc audit findings (fixed this lane)

All fixed by updating stale docs to match reality (code + verified
evidence). The frozen `INTERFACE_FREEZE.md` v1 was intentionally NOT
edited — the makeslice/growslice drift is a code bug (F-1), and the freeze
doc is correct as written.

**F-1. Frozen-signature drift (code bug, reported).** See §4.1. Docs
affected: none changed (freeze doc stands); [DONE-GATES.md](../phase2/DONE-GATES.md)
annotated with the red state.

**F-2. M9 acceptance report numbers wrong.** `acceptance/FINAL-REPORT.md`
(on the unmerged m9-acceptance branch) claims "Phase 1: 180/182, 2 FAIL";
its own `phase1-test.log` says "214 tests, 3 failures" (missed
`FreezeSignatureTest`). Wave-3 "7 PASS, 0 FAIL" vs log "7 passed,
1 failed" (timeout). Not fixed (branch unmerged) — recorded in
CHECKPOINT.md and here; the closeout uses the log numbers.

**F-3. Suite-count drift across docs.** DONE-GATES.md "193/193" (phase-2)
and "208/208" (phase-1) are sign-off-time records; the trees now have 194
and 214. CHECKPOINT entries variously report 193/194 and 208/211/213/214
at different times — all correct *at their time*, but no doc said so.
Fixed: DONE-GATES.md annotated (193→194 via P3-M2a `ef830ad`); PLAN.md
§4 P3-M9 cites the acceptance-time numbers with their basis.

**F-4. False review-evidence claims.** CHECKPOINT.md claimed
`reviews/phase3-m5/reviewer1.md` + `reviewer2.md` were committed — no
commit on any branch ever added them. Fixed: both claims corrected with
an explicit process-gap note (same class as the P3-M1 gap, which *was*
honestly documented in [reviews/phase3-m1/README.md](../../reviews/phase3-m1/README.md)).

**F-5. Stale PLAN status lines.** phase1/PLAN.md ("for parent review") and
phase2/PLAN.md ("draft for parent review") described pre-kickoff states;
both phases are complete. Fixed: statuses now COMPLETE with sign-off
pointers. phase3/PLAN.md status updated (M10 in progress).

**F-6. Stale [OPEN] items in phase3/PLAN.md §3.** All four resolved during
P3-M6/M9 (exclusion cap approved; coupled set re-measured, P3-D1 stands;
crypto intrinsify deferred to Phase 5+; net scope triaged) but still
marked [OPEN]. Fixed: each marked [RESOLVED] with date + evidence pointer.

**F-7. P3-M9 scope mismatch.** PLAN.md §4 still described the original
wave-3 "`go test` green" scope; the supervisor re-planned M9 on 2026-10-07
(M9 absorbs crypto/tls) and the full `go test` was deferred to gate 1.
Fixed: §4 records actual delivered scope + the deferral.

**F-8. SPEC.md §2.8 stale count.** "~78 coupled packages" — re-measured
142 on go1.27.1 (P3-M6). Fixed: updated with the measured number and a
pointer to the versioned list + ADR addendum.

**F-9. CHECKPOINT.md missing M9/M10.** The checkpoint ended at 2026-10-09
tier-5 worker notes; M9 acceptance, merge, and M10 start were unrecorded.
Fixed: entries appended (reviews, acceptance with accuracy notes, merge,
M10 lane map).

**Checked and consistent (no fix needed):** PHASES.md §5 gates ≡ PLAN.md
§6 gates; SPEC §5 testing strategy ≡ PLAN §5 layers; phase2/DIVERGENCES.md
has no orphan [OPEN]s (matches DONE-GATES.md gate-5 claim); exclusion
list v2 under cap (matches PLAN); [reviews/phase3-m1/README.md](../../reviews/phase3-m1/README.md) honestly
documents its reconstruction.

---

## 6. Files changed by this lane (docs only, no code)

- [phase1/PLAN.md](../phase1/PLAN.md) — status → COMPLETE (F-5)
- [phase2/PLAN.md](../phase2/PLAN.md) — status → COMPLETE (F-5)
- [DONE-GATES.md](../phase2/DONE-GATES.md) — sign-off-time annotation + F-1 red-state note (F-1, F-3)
- [PLAN.md](PLAN.md) — status, [OPEN]→[RESOLVED] ×4, P3-M9 actual scope (F-5, F-6, F-7)
- [CHECKPOINT.md](CHECKPOINT.md) — P3-M5 review-claim corrections, M9 + M10 entries appended (F-4, F-9)
- [SPEC.md](../../SPEC.md) §2.8 — coupled-package count re-measured (F-8)
- [CLOSEOUT.md](CLOSEOUT.md) — this document (new)

**Verification:** no code touched (`git status` shows only the files
above). Markdown links: every file/dir reference in new/edited prose is a
relative link or an inline code path that exists on disk (spot-checked).

---

## 7. End state

Per the end-state contract: **CONTINUING WITHOUT HIM** — the audit and
closeout are DONE with evidence above; the remainder (gates 1, 2, 3, 5
evidence; gate 4 self-attack + m9 review integration; F-1 code fix) is
dispatched or flagged: four M10 lanes in flight (`m10-testing-layers`,
`m10-snapshots`, `m10-diff-tooling`, `m10-self-attack`), gate 1 lane
unassigned (flagged to parent), F-1 code fix owned by Phase 4.
