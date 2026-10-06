# P3-M3 methodology review (draft) — independent reviewer

**Reviewer role:** methodology (process, evidence, spec compliance — not line-by-line code correctness).
**Snapshot reviewed:** `1bc1f36..0fe5b6c` (16 commits). Verified `git rev-parse HEAD` ==
`0fe5b6c` at review time: the range is fixed, not the moving live tree.
**Normative basis:** `phases/phase3/adr/ADR-0003-interface-dispatch.md` (spec + 2 amendments),
`phases/phase3/PLAN.md` §§2–5, `phases/phase1/INTERFACE_FREEZE.md` (freeze v1).
**Date:** 2026-10-06.

## Verdict: GO-WITH-FINDINGS

The milestone's process evidence is unusually honest and complete (red-green-first with
copy-isolated mutations, an 11-program differential corpus with disclosed scope notes, an
M9 re-measurement that explicitly disclaims workload representativeness). No finding
below blocks sign-off on substance; findings F1, F3, F6 are documentation/claim-scoping
issues that should be remediated before sign-off per the program's own rules
("docs consistent with the code itself"; "scope verdicts to what was tested").
F5's deferral needs to be carried forward as a recorded open item, not just a sentence
in a hidden file.

---

## 1. Freeze compliance — every Phase-1 touch in the range

`git diff --name-only 1bc1f36..0fe5b6c -- phases/phase1/` returns 10 files.
Coverage against the ADR-0003 documented exception
(exporter `export.go` + `main.go` version bump + the WS-D retirement amendment):

| # | File | Change (verified in diff) | Covered? |
|---|---|---|---|
| 1 | `frontend/export.go` (+443) | `ifacemethod` emission, `methods` population on named/pointer entries, `recvIface` type-param exclusion | **Yes** — exception item 1 (commits `f9d5d56`, `01ac1c5`, `3112d10`, `3cec0fc`, `2727ee4`) |
| 2 | `frontend/main.go` (+1/−1) | `toolVersion` `"0.3.0"` → `"0.4.0"` and nothing else | **Yes** — exception item 3 |
| 3 | `frontend/iface_export_test.go` (new, 607) | exporter tests for the new wire | **Yes** — test-only code is INTERNAL per freeze §8 |
| 4 | `frontend/testdata/straight01.json`, `straight02.json` (+1/−1 each) | only the `generator.version` string 0.3.0 → 0.4.0 | **Yes** — consequence of the version bump; corpus expectations |
| 5 | `docs/frontend-v1.md` (+16/−…) | documents `ifacemethod` kind + populated `methods` on named/pointer | **Yes** — wire-spec companion of exception item 1; required by PLAN.md lesson 5 (docs in the same change). **But see F1**: the doc under-describes the wire (see below) |
| 6 | `src/main/java/graalgo/frontend/NodeFactory.java` (+168/−…) | `maybeConvert` retirement, `adaptArgsForInterfaceParams` named-interface fix, `buildTypeAssert`/`buildCommaOk` retirement, plus supporting helpers (`isInterfaceType`, `interfaceMethodNames`, `dynamicMethodSets`, `BuildCtx.dynamicMethodSets`, NilNode `setTypeId`, `T(nil)` retype) | **Yes** — the amendment names the three builder retirements; the helpers/NilNode retype are the retirement's mechanics (typed-nil boxing, commit `3378aab`). All fail-fasts retired are the ones whose named condition was "WS D" |
| 7 | `TypeAssertNode.java` (+48) | ctor gains `assertedIfaceMethods` + `dynamicMethodSets`; `holds()` gains the implements check | **Yes** — the amendment's "carried in the node — no global state" is exactly this ctor change, installed by the named factory builders |
| 8 | `TypeAssertOkNode.java` (+15) | same pair of fields, delegates to `TypeAssertNode.holds` | **Yes** — same as #7 |
| 9 | `SelectorNode.java` (+18) | new public static `readFieldPath(Object, int[], boolean)`; `readField` refactored to delegate with `derefResult=false` (byte-identical behavior on the old path) | **Partial** — see F1 |

**No Phase-1 touch is wholly uncovered**, but #9 is not *named* anywhere in the
exception or the amendment. It is defensible under freeze §3.3/§8 (individual node
implementations are INTERNAL; the frozen `executeObject` protocol is untouched and the
old `readField` path is behavior-identical), so no freeze violation occurred. The
substantive gap is what #9 exists to serve — F1.

### F1 (low severity for freeze, medium for "the ADR is the record"): the wire `index` field is undocumented

- The exporter emits method-table entries as `{"name", "obj", "recv", "index"}`
  (export.go diff line ~365; `"index"` is the receiver-adaptation path,
  `Selection.Index()` minus its last entry; the Java dispatcher walks it for
  promoted methods — commits `3112d10`, `8be190f`, which is why `SelectorNode`
  gained `readFieldPath`).
- ADR-0003 decision item 2 documents the shape as
  `[{"name": "Write", "obj": 17, "recv": "pointer"}, ...]` — no `index`.
- `frontend-v1.md`, updated **in this range** to describe exactly this change,
  documents `methods`: [{`name`, `obj`, `recv`}] — no `index`. (The only `index`
  in the doc is the unrelated subscript-expression node kind.)
- Freeze-wise this is legal: the field is additive and §2.3's forward-compat
  rule (consumers ignore unknown fields) covers it. But freeze §1(c) designates
  **the ADR as the change record**, and the program rule is "docs consistent with
  the code itself". The change record under-describes the wire it authorizes,
  and the in-range doc update missed a load-bearing field (the Java side reads
  it; it is not vestigial).
- **Recommendation:** amend ADR-0003 item 2 and the `frontend-v1.md` `methods`
  rows to document the `index` field (path semantics + `null` for the
  unsupported value-embedded/pointer-receiver shape) before sign-off. One-line
  class hunt: grep the exporter for other wire fields added in-range that the
  doc omits (I checked `ifacemethod`'s six fields — all six are documented).

## 2. Freeze-doc staleness — correctly handled, no finding

`INTERFACE_FREEZE.md` §2.1's `toolVersion` row still reads `"0.1.0"` while the
binary is at 0.4.0. Per freeze §1(c) ("No mechanism records (c) post-hoc — the ADR
is the record") and the explicit ADR-0002 precedent (§"Version", lines ~90–95:
"the freeze document is itself frozen… This ADR is that record for the 0.2.0→0.3.0
bump; the freeze text is not edited"), ADR-0003 is the record for the 0.3.0→0.4.0
bump. The staleness is intentional and policy-compliant. **No action.**

## 3. Red-green quality

`phases/phase3/hidden_files/m3/redgreen.md` reviewed in full.

- **Copy-first / clean-tree verification: PASS.** End-to-end mutations ran in an
  isolated copy at `~/workspace/mut` (never the live tree), the copy was verified
  clean before baseline (`grep` for mutation markers: none; `if00` byte-identical
  on the copy), each mutation was reverted + rebuilt + re-baselined (`if00`
  byte-identical) before the next, and the live tree was verified marker-free at
  the end. This satisfies the AGENTS.md mutation lesson (copy first, verify
  clean; the byte-identical `if00` run is the functional clean check).
  The earlier `DescriptorNamesTest` mutation was a temporary edit under
  `phases/phase3/` (not frozen), restored afterwards, with `git status`
  verification — acceptable.
- **Non-tautological: PASS for all three.** A breaks the PIC identity check →
  `ClassCastException` on `if05`; B drops pointer-receiver methods from `*T`
  sets → fail-fast "no method table installed" / the designed "unreachable"
  guard fires on `if00`/`if08`; C swaps two methods' `GoFunc`s in one itab →
  `ArrayIndexOutOfBoundsException` on `if09`. Each is capable of failing and
  did fail, for the mechanism it targets.
- **F3 (medium): mutation C overclaims.** The doc concludes: "A wrong-method
  dispatch cannot be silent: the arity mismatch crashes loudly at the call
  boundary." What C actually proves: swapping two methods' `GoFunc`s is
  load-bearing (not dead code) **when the arities differ** — `if09`'s
  `Write([]byte) int` vs `Close() int` differ in frame shape, so invoking
  Close's func with Write's frame crashes. Nothing at the invoke boundary
  checks method identity (`GoCalls.invoke` receives a `GoFunc`; the compiled
  `RootCallTarget` carries no method-name check), so a **same-arity swap would
  be silent** — wrong method, plausible frame, no crash. "No silent
  wrong-method divergence" is therefore not established by C.
  **Recommendation:** scope the claim to what was tested ("a wrong-method
  mapping with mismatched arity crashes loudly at the call boundary") or,
  better, add a same-arity swap mutation: it would still go red via **output
  mismatch** rather than crash, which honestly demonstrates load-bearingness
  through wrong output and correctly labels the silent case. Either way the
  mutation is valid red-green evidence; only the inference needs scoping.

## 4. Differential rigor

`phases/phase3/corpus/iface/` (if00–if10), `run_differential.sh`, `README.md`
reviewed; spot-read if00, if01, if02, if05, if09, if10 sources.

- **Methodology: sound.** Each program runs under stock gc 1.27.1 (built binary,
  so exit codes are real) and under graalgo (`P3SyncMain`, `--gmp=1`);
  comparison is stdout **bytes** (`cmp`), exit codes, and — for the panic program
  — the first stderr line. JVM `WARNING:` noise is filtered with a documented
  justification (`p3frontend/DESIGN.md` §7). `.expected` files are regenerated
  from gc by `regen.sh` (record, not oracle — the runner compares live gc vs
  graalgo, which is stronger).
- **Corpus strength: genuine.** Every program dispatches through interface-typed
  call sites (no concrete-call smuggling spotted). if05 is a true megamorphic
  probe (4 types, 1 site → PIC limit-3 overflow). Coverage is systematic:
  value/pointer receivers (if00), multi-impl + arg adaptation (if01), zero-result
  method `Swap` + bimorphic sites (if02), error values + nil error (if03),
  assertions incl. comma-ok (if04), typed nil (if07), promoted receivers via the
  wire `index` path (if08), iface embedding flattening (if09), interface params
  on plain functions and on interface methods incl. named interface types (if10),
  nil panic + exit 2 (if06).
- **Gaps (disclosed, but note the precision issues):**
  1. stderr is **not compared** for non-panic programs — success-path stderr
     junk on the graalgo side would go undetected. Minor; recommend adding a
     filtered stderr comparison for all programs.
  2. "11/11 byte-identical" (redgreen.md) is loose for if06: its first stderr
     line is compared against the **recorded graalgo rendering**
     (`panic: GoErrorValue{...}`), not against gc. The README scope notes
     disclose this honestly (pre-existing Phase-2 `toString` rendering; the
     `.panic` / `.panic.gc` pair records the divergence). No unrecorded
     divergence — but the "byte-identical" phrasing should carry the same
     qualifier the README does.
  3. **Multi-result interface methods are untested.** The corpus uniformly uses
     single-result methods (`Write([]byte) int`); the wire `type` field's
     multi-result behavior and result adaptation are not differentially
     exercised. This is disclosed as a scope error ("frontend/v1 WS D allows at
     most one result per method"), and fail-fast scoping is legitimate — but
     the accompanying claim "The dispatch mechanics are identical" (if00
     comment, README) is **asserted without evidence**. No corpus program
     verifies that the named scope errors actually fire (multi-result,
     interface method values/expressions, generic methods via interfaces,
     value-embedded + pointer-receiver promotion). Recommend: at minimum, a
     negative-test program asserting the scope errors are raised (fail-fast
     paths deserve red-green too); the sameness claim should be scoped or
     proven.

## 5. M9 honesty — the Phase-2 re-measure trigger

- **Trigger text** (Phase-2 PLAN.md): "M9 polymorphism numbers (170 sites:
  99.4% monomorphic…): interface dispatch does not exist yet, so megamorphism
  at interface sites is unmeasured — the scheduler's PIC strategy assumes the
  Phase-1 distribution; re-measure when interfaces land (Phase 4 input)."
  Recorded as CHECKPOINT.md open item #3 ("fires when interface dispatch lands
  (P3-M3)").
- **What was delivered** (`hidden_files/m3/polymorphism.md` + `run_m9.py` +
  `polymorphism_report.json`): 31 sites over 21 programs (11 corpus + 10
  adversarial probes), 1227 probed dispatches, per-site itab-identity
  histograms via `--dump-polymorphism`: 41.9% mono / 35.5% bi / 22.6% mega.
  The worker explicitly states the distribution "does NOT describe real Go
  workloads — it describes this probe set… An adversarial exercise set measures
  mechanism coverage, not workload shape", and: "That question needs a
  workload-representative program set… deferred to Phase 4, when the frontend
  covers enough of the language to run one. This measurement is the mechanism
  check, not the workload verdict."
- **Assessment: honestly scoped, acceptable discharge.** The trigger fires
  "when interfaces land" — they landed; the re-measurement was performed and
  recorded, satisfying PLAN.md §4's "M9 polymorphism re-measured and recorded"
  acceptance. What it establishes (dispatch correct across the full
  mono/bi/mega spectrum; PIC serves mono/bi; hash fallback serves mega) is
  exactly what an adversarial set can establish, and the doc refuses to claim
  more. This is the standing "scope verdicts to what was tested" principle
  applied correctly. The deferral rationale is sound: no workload-representative
  interface-heavy program can run yet (package waves are P3-M7+).
- **F5 (low, process): the deferral must be carried forward as a recorded open
  item.** "Deferred to Phase 4" currently lives only as a sentence in
  `hidden_files/m3/polymorphism.md`. Phase 4 has no plan doc yet; without a
  carried-forward entry (CHECKPOINT.md open-items list or the P3-M3 sign-off
  entry), the workload-representative re-measurement — the actual content of
  the "(Phase 4 input)" trigger — risks being lost. **Recommendation:** the
  P3-M3 sign-off entry adds: "Open: workload-representative polymorphism
  re-measurement deferred to Phase-4 entry (frontend then runs real
  interface-heavy code); the 41.9/35.5/22.6 split is mechanism coverage, not a
  workload verdict, and must not be cited as one."

## 6. GG activation

- **GG-0014: properly activated.** `verifies: GG-0014` is on the dispatch node
  (`IfaceDispatchNode.java:50`) and on the test class plus 10 test methods —
  first real tests land with the annotation, per the PLAN.md §2 activation
  policy ("exactly when its first real test lands with a `verifies: GG-XXXX`
  annotation; never before").
- **F6 (low, traceability): GG-0017's iface slice lacks its annotation.**
  ADR-0003's scope line and verification list both state "GG-0017 (iface slice)"
  / "GG-0017 iface slice activates", but no `verifies: GG-0017` annotation
  exists anywhere in the iface code or tests — the only `verifies: GG-0017`
  annotations in phase 3 are the P3-M2 sync slice (`SyncNewNode`,
  `SyncOpNode`, `P3SyncMain`, each marked "(sync slice)"). Under the policy's
  letter, the annotation on the first real test *is* the activation event, so
  the iface slice's activation is claimed in prose but not recorded in code.
  **Recommendation:** add `verifies: GG-0017 (iface slice)` annotations to the
  iface tests (mirroring the sync slice's "(sync slice)" convention) before
  sign-off. Substantive risk: none — this is traceability hygiene.

## 7. Review-gate compliance

- **Fixed snapshot: confirmed.** `git rev-parse HEAD` == `0fe5b6c`; the reviewed
  range `1bc1f36..0fe5b6c` is exactly the 16-commit P3-M3 batch. No live-tree
  drift can invalidate this review.
- **≥2 independent reviews: satisfied in structure.** This methodology review
  plus the parallel code review constitute the two required independent
  reviews (PLAN.md §4: "code + methodology, generated artifacts included").
- **No process gap found** in review setup. Standing rule noted for sign-off:
  the supervisor independently re-runs the full suite + `make check` on the
  sign-off tree (the ADR's "phase-1 208/208, phase-2 194/194" is a worker
  claim until re-verified; the 194 vs the Phase-2 closeout's 193 is presumably
  post-P3-M2 test growth, but the number must be re-verified, not trusted).

## 8. Divergence check

- `phases/phase3/corpus/iface/README.md` scope notes reviewed against the
  corpus. The **if06 panic-rendering divergence** (`panic: GoErrorValue{…}`
  vs gc's `panic: runtime error: …`) is recorded in the `.panic`/`.panic.gc`
  pair and disclosed in the README as pre-existing Phase-2 rendering — not new,
  and recorded.
- The single-result limitation, inlined `sort`, synthetic-`fmt` constraints,
  and the implicit-address-of/deref exclusion are **scope errors / out-of-scope
  shapes, not divergences** — fail-fast by design, no silent behavioral delta
  vs gc.
- **No unrecorded new divergence vs gc found** in the range.

## Findings summary

| ID | Severity | Finding | Recommended remediation (pre-sign-off) |
|---|---|---|---|
| F1 | Low (freeze) / Medium (record-keeping) | Wire `methods` entries carry an undocumented `index` field; ADR-0003 item 2 and the in-range `frontend-v1.md` update document only `{name, obj, recv}` | Document `index` (path semantics + `null` case) in ADR-0003 and `frontend-v1.md`; class-hunt other in-range wire fields (done for `ifacemethod` — clean) |
| F2 | — | Freeze-doc `toolVersion` staleness (0.1.0 vs 0.4.0) | None — correctly handled per freeze §1(c) + ADR-0002 precedent |
| F3 | Medium | Mutation C proves load-bearing only for arity-mismatched swaps; "no silent wrong-method dispatch" is not established (same-arity swap would be silent — no method-identity check at the invoke boundary) | Scope the claim, or add a same-arity swap mutation (red via output mismatch) |
| F4 | Low | Differential runner doesn't compare stderr on success paths; "11/11 byte-identical" overstates if06; multi-result methods untested with an unevidenced "mechanics are identical" claim; named scope errors have no negative tests | Filtered stderr comparison for all programs; qualify the byte-identical claim; add negative scope-error programs |
| F5 | Low (process) | M9 workload-representative re-measurement deferred to Phase 4 only in a hidden-file sentence — risks being lost | Record as a carried-forward open item in the P3-M3 sign-off / CHECKPOINT.md |
| F6 | Low (traceability) | GG-0017 iface slice claimed active in ADR but has no `verifies: GG-0017` annotation (activation policy: the annotation is the activation event) | Add `verifies: GG-0017 (iface slice)` annotations mirroring the sync-slice convention |
| F7 | — | Review-gate setup | None — fixed snapshot confirmed, 2-review structure satisfied |
| F8 | — | Divergence check | None — no unrecorded new divergence |

## M9 trigger discharge — honestly scoped: YES

The worker measured what the trigger asked for when it fired (interfaces
landing), recorded per-site machine data, proved mechanism correctness across
the mono/bi/mega spectrum, and explicitly refused the workload-shape inference
the adversarial set cannot support — deferring it to Phase 4 with a named,
checkable reason. That is an acceptable discharge, provided F5 is actioned so
the deferred measurement survives as a recorded commitment rather than a
sentence in a hidden file.
