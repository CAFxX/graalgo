# Phase-3 checkpoint (append-only; newest state at the tail)

## 2026-10-06 — Phase 3 opened (planning)

**Phase-2 handoff state:** Phase 2 COMPLETE, sign-off 9694cbf
(2026-10-06). All five done-gates met; 193/193 phase-2 + 208/208 phase-1
suites independently re-verified by the supervisor. Delivered: scheduler
core, channels+select, timers, GC interop, GODEBUG triage, JFR events,
pinning audit (0 unexpected pins, mechanical gate), preemption-torture
(12/12, kill criteria NOT triggered), lost-wakeup model checking
(bespoke explorer over the real Drainer), divergence closeout.
Integrity repair held throughout (supervisor re-ran every suite).
Hourly Phase-2 report cron retired.

**Phase-3 entry gate (must be true before P3-M1 starts):**
1. Phase-2 sign-off tree clean at 9694cbf — verified (supervisor).
2. Interface freeze v1 in force; Phase-3 code lives under
   `phases/phase3/src/`; Phase-1 files are not modified.
3. Toolchain intact: Oracle GraalVM 25.0.4.1.1 LTS, stock Go 1.27.1 at
   `~/tools/go`, go1.27.1 stdlib source present at
   `~/tools/go-1.27.1/src` (sync/time/context dirs confirmed).
4. Normative inputs read: PHASES.md §5, SPEC.md, INTERFACE_FREEZE.md,
   phases/phase2/DONE-GATES.md + CHECKPOINT.md tail.

**Open items carried forward (Phase 2 → Phase 3):**
1. **P2-M8 deferred `sync` Go bindings — now Phase 3's problem (P3-M2).**
   The four primitives exist as Phase-2 Java classes
   (`graalgo.runtime.sync.{GoMutex,GoRWMutex,GoWaitGroup,GoOnce}`),
   stressed at the Java level. P3-M2 lowers `import "sync"` onto them
   and re-runs the P2-M8 stress as Go programs differential vs gc —
   discharging the recorded re-test trigger. (Owner-accepted at
   P2-M10; conditional discharged.)
2. **DIV-M3-1 (`len()` on buffered timer channel returns 1, gc: 0) —
   BLOCKING for `time` lowering (P3-M5).** Fix red-green-first in
   P3-M2, before `time` is lowered. Documented in
   phases/phase2/DIVERGENCES.md with the trigger.
3. **M9 polymorphism re-measurement** fires when interface dispatch
   lands (P3-M3). 170 sites were 99.4% monomorphic with no dispatch;
   re-measure with dispatch live.
4. **Latency-distribution tolerance** (ACCEPTED-WITH-TOLERANCE at
   P2-M10): server-hardware re-measure trigger stands; Phase-3
   perf-sensitive work must not silently widen the band.
5. **38µs cold-drainer wakeup** (unexplained, carried since Phase 0):
   hypothesis + server-hardware re-test trigger stand.
6. **`context` package** was pattern-level in Phase 2 (needs
   interfaces); P3-M3 dispatch unblocks it for P3-M5.

**Standing owner directives in force:**
- Proceed autonomously to completion of the project (2026-10-06).
- Supervisor re-runs suites + `make check` before accepting any
  milestone sign-off (M8 integrity repair, now habit).
- Continuous adversarial test-finding track: launch as soon as the
  supervisor judges it productive; findings filed as bugs with
  reproducers; layer-4 slot reserved in this phase's testing strategy.
- Hourly Phase-3 reporting cron: installed by the supervisor at phase
  start, retired at independent phase confirmation.

## 2026-10-06 — P3-M1 implementation complete (WS A)

**Delivered:** `phases/phase3/src/p3frontend/` (new Go module `p3frontend`,
go 1.27): `ExportDir`/`MergeDir` compiling a multi-file `package main`
directory into one frontend/v1 JSON document via loader (build-tag
filtering verified file-for-file against the `go/build` oracle on
synthetic corpus + real `src/os`, `src/strings`), graduated `types.Importer`
(synthetic `runtime`/`fmt` with exact frozen signatures reimplemented;
stdlib resolves but fails fast with "P3-M7+ waves" scope error), and
package flattening (`p3i_<sha256hex8>_` prefix, go/types-driven selector
rewrite, one merged file into the frozen `gofrontend` binary — the module
is `package main` and unimportable, so the frozen CLI contract §2.1 is the
consumption surface; recorded as a design deviation in DESIGN.md §8).
Design: `src/p3frontend/DESIGN.md` (spec-first). Tests: 6 (2 cycle
subtests) — bootstrap deep-equal vs `ExportFile`, two-package differential
vs gc (normal: byte-identical stdout, exit 0; panic: identical `panic:`
line, exit 2), no language pinning (go1.27 tag + generics), determinism,
import cycles (fail-fast, timeout-guarded). Red-green-first evidence:
`hidden_files/m1/redgreen.md` (broken flattener → fail-fast; disabled tag
filter → oracle mismatch; both reverted, suite re-run green).
**Empirical finding:** `func init()` is accepted by `gofrontend` but never
executed by the runtime (gc prints INIT RAN, graalgo INIT DID NOT RUN) —
M1 rejects any `func init()` with a clear scope error (documented
limitation, never a silent gap); probe at `hidden_files/m1/initprobe2.go`.
Commits: f3e9b05 (design), b5d3b03 (loader; verifies: GG-0023, GG-0012),
1fc2f3a (importer/flattener/ExportDir; verifies: GG-0012, GG-0013).
Phase-1/Phase-2 trees untouched (nothing outside `phases/phase3/src/`,
`phases/phase3/hidden_files/m1/`, `phases/phase3/CHECKPOINT.md`).

## 2026-10-06 — P3-M1 review remediation (code + methodology reviews)

**Every finding remediated** from the two independent reviews of
7bf5e4e..1664fa9 (code review: 1 P0 + 3 P1 + 4 P2; methodology review:
4 P1 + 7 P2), in one-logical-change-per-commit sequence:

1. `86b0a61` **P0-1 embedded-field silent rebinding** (flatten.go):
   `identObj` prefers the `Uses` type for embedded field Vars; new
   `renameEmbeddedField` helper renames embedded-field selectors (`o.T`,
   promoted) and keyed literal elements (`Outer{T: ...}`) to the renamed
   type name. Regression: `TestEmbeddedFieldRebinding` (asserts exact
   `h.T` output; verified red without the fix — gc `h.T` vs merged
   `main.T`, both exit 0) + colliding-`T` case in the twopkg corpus.
2. `8b87455` **P1-2 build-tag header region** (loader.go):
   `headerBuildLines` tracks `go/build` `parseFileHeader`'s `end` offset;
   `// +build` honored only in the trimmed region (no-blank-line
   `// +build` ignored, as `go list` does); **P1-3 resolve-cache keying**:
   cache keyed by (abs fromDir, path) with nested-module regression test
   (verified red with the old key). Corpus extended with the reviewer's
   adversarial cases — all oracle-agreeing.
3. `f11ba64` **P1-4 exporter timeout** (export.go): `exec.CommandContext`
   with `gofrontendTimeout` (10 min, test-shortenable); named timeout
   error; P2 error-prefix dedup (`p3frontend: gofrontend: <err>`,
   DESIGN.md §8 "verbatim" claim corrected); defensive nil checks in
   `Flatten`; `go.mod` `tidy` interaction documented (durable record:
   DESIGN.md §8).
4. `f8269ee` **Methodology P1-1**: GG-0013/GG-0023 locked→active with
   dated history entries (first real tests with `verifies:`
   annotations); GG-0012 (already active) gained its P3-M1 `verifies:`
   entry. GG-0017 untouched (Phase-2's record, still locked).
5. `ab98a09` **Methodology P1-2/P1-3/P1-4**: twopkg corpus gains a
   main-level `Counter` shadowing `helper.Counter`; third red-green
   proves the byte comparison catches the QUIET divergence (gc
   `main counter: 1000` vs graalgo `main counter: 2`, both exit 0),
   reverted byte-exact; `TestInitRejected` (main + dep);
   `TestStdlibScopeError` (resolves at `~/tools/go-1.27.1/src/strings`,
   fails fast with "P3-M7+ waves"); normal differential leg builds a real
   gc binary; panic-stack-by-design comment.
6. (this commit) **P2 wording**: "GOROOT source tree; vendoring is P3-M6"
   in PLAN.md's M1 scope (phase-objective and later-milestone mentions
   intentionally left — they describe the planned end state) and the
   loader.go comment; init-order non-guarantee sentence in DESIGN.md
   §2.3 (path sort matches OBSERVED gc, not a spec guarantee).

**Re-test results:** `go test -count=1 ./...` — 13 top-level tests
(15 incl. subtests), all PASS; `go vet ./...` and `gofmt -l` clean; no
TODO/placeholder markers. All three red-green mutations (both originals
+ the new quiet-divergence red) re-run with transcripts in
`hidden_files/m1/redgreen.md`, each reverted byte-exact (`git diff`
empty) and the suite re-run green afterwards. Only
`phases/phase3/src/`, `phases/phase3/hidden_files/m1/`,
`phases/phase3/CHECKPOINT.md`, `phases/phase3/PLAN.md`, and
`requirements/GG-0012.yaml`, `requirements/GG-0013.yaml`,
`requirements/GG-0023.yaml` modified.

## 2026-10-06 — P3-M2 sign-off: `sync` Go bindings + DIV-M3-1 fix (WS D)

**Recovery note:** the original coordinator's VM was rebooted mid-milestone;
a recovery coordinator audited the tree against the acceptance criteria,
finished the missing P2-M8 discharge note, ran two independent reviews, and
remediated all findings before sign-off. Sign-off commit: `f1d7de0`.

**M2a — DIV-M3-1 FIXED** (`ef830ad` red + `052c752` fix): `TimerChan extends
GoChan` masks `buffered()`/`capacity()` to 0 while `trySend`/`tryRecv` still
deliver fire values. Red-green evidence: `phases/phase3/hidden_files/m2/
redgreen.md` (red run `0 1`/`1 1`, green `0 0` byte-matching the gc-recorded
`.expected`). `DIV-M3-1` marked FIXED in `phases/phase2/DIVERGENCES.md`;
the Phase-3 blocking trigger is discharged — `time` may now be lowered
(P3-M5). New `TimerDifferentialTest#testTm15TimerChanLenCap`
(`verifies: GG-0048`); phase-2 suite is 194/194.

**M2b — `import "sync"` lowers onto the Phase-2 Java primitives** (ADR-0002,
accepted 2026-10-06): synthetic `sync` package in the frozen exporter's
`overlayImporter` (documented freeze exception, P2-D8 precedent `2e5f012`
verified); additive `syncnew`/`syncop` wire nodes (wire stays v1);
`gofrontend` toolVersion 0.2.0→0.3.0; phase-3 Java module
(`SyncNewNode`/`SyncOpNode`/`P3SyncMain`, `verifies: GG-0017` — sync slice
now active); exporter defer/go desugars (D5b); misuse → Go panic with gc's
message (D4, with the scoped fatal-vs-panic divergence honestly recorded).
Corpus `phases/phase3/corpus/sync/` sy00–sy15 (16 programs):
`run_differential.sh` 25/25 vs gc 1.27.1 (stdout bytes, exit codes, first
stderr line). **P2-M8 re-test trigger discharged** (note in
`torture/REPORT.md` §5, commit `39c5b77`): the `SyncStressTest` legs
re-ran as Go programs (exclusion 32×1000 incl. `--gmp=1` cap-1 leg, RWMutex,
WaitGroup, Once).

**Reviews** (fixed diff `e0c07a5..39c5b77`; reports archived in
`reviews/phase3-m2/` — the P3-M1 process gap is closed):
- Code: **NO-GO** → all remediated. 3 P0 (P0-1 `RWMutex.Unlock` and P0-2
  `WaitGroup.Done` misuse escaping as host exceptions — `72c4432` wraps
  both in `panicOnMisuse`, adds sy13/sy14; P0-3 `go m.Op(args)` arg timing
  — `3c163d5` hoists args/receivers to go-time temporaries, adds sy15) +
  3 P1 (P1-1 independent GOROOT signature oracle `ab00108`; P1-2
  `GoRWMutex` message → gc's `Unlock` `2bb8c18`; P1-3 covered by sy15) +
  6 P2 (docs/hygiene `5e6c34e`; P2-6 render wart noted, out of scope).
- Methodology: **GO-WITH-FINDINGS** → all remediated. 3 P2 (commit-message
  count typos corrected in `reviewer2.md` — commits immutable; nil-safe
  red test `f6800ad`).
- Post-remediation follow-up: sy06/sy07 had racy counters (one flake each
  observed) — de-flaked with mutex guards (`f1d7de0`); per the standing
  "issues get fixed, not just documented" principle.

**Process answers recorded:** (1) Phase-2 source edits post-sign-off
(M2a's `GoChan`/`GoTimer`/`TimerChan`, P1-2's one-word message fix) are
permitted as *documented milestone exceptions* with red-green evidence —
not silent edits; future phase-2 edits should get their own ADR (ADR-0002
§4 + phase-3 Makefile header rewritten to say exactly this). (2) The
freeze exception for the synthetic `sync` package follows the P2-D8
precedent and stays within freeze §§2.3/5; `INTERFACE_FREEZE.md` itself
was not edited.

**Final verification (sign-off tree, independent re-runs):**
`make -C phases/phase1 test` 208/208; `make -C phases/phase2 test`
194/194; `go test -count=1 ./...` in `phases/phase3/src/p3frontend` green;
`bash phases/phase3/corpus/sync/run_differential.sh` 25/25;
`make -C phases/phase3 check` clean. No orphan [OPEN]s; kill criteria not
triggered. GG-0017 sync slice →active.

**P3-M2 is COMPLETE.** Next: P3-M3 (interface dispatch).

## 2026-10-06 — P3-M3 review remediation (code + methodology reviews)

**Every finding remediated** from the two independent reviews of
`1bc1f36..0fe5b6c` (code review verdict: **NO-GO** — 2 P0 silent
divergences, 2 P1, 9 P2; methodology review: **GO-WITH-FINDINGS**, F1–F8;
final reports: `reviews/phase3-m3/reviewer1-code.md`,
`reviews/phase3-m3/reviewer2-methodology.md`), in
one-logical-change-per-commit sequence:

1. `4586f00` **R1 P0-1 signature-aware implements check**: the exporter
   emits `implkey` (method name — unexported names package-qualified — +
   structural signature key with aliases expanded, basics by `BasicKind`)
   on method-table and interface entries; `TypeAssertNode.holds` tests
   implkey inclusion. Red-green: wrong-signature assert prints
   `assert ok` pre-fix vs gc `assert failed` (RED), `assert failed`
   post-fix (GREEN); corpus `if11_assert_signature.go`.
2. `4e6d84f` **R1 P0-2 canonical descriptors**: new public additive
   `NodeFactory.canonicalDescriptorFor` (ADR-0003 amendment, factory
   exception); the P3 builder's `ctx.typeDescs` miss path uses it instead
   of minting a replica. Red-green: callee assert/switch prints `no`
   pre-fix vs gc `yes-*T` (RED), `yes-*T` post-fix (GREEN); corpus
   `if12_callee_type_identity.go`.
3. `98fb39d` **R1 P1-1 alias-of-interface receiver**: `types.Unalias`
   before the iface-id switch (`type W = Writer`). Red-green: export
   error pre-fix, gc-identical output post-fix;
   `TestIfaceMethodCallAliasReceiver`.
4. `2ef6540` **R1 P1-2 generic instantiation via interface**: exporter
   marks `"templateMethodsSkipped": true`; the frontend fails fast with
   the named P3-M3 scope error at all boxing sites and — for the
   `any`-then-assert shape — at assertion runtime via a sentinel (a bare
   `false` there would be a silent divergence vs gc). Red-green:
   misleading runtime error pre-fix, named scope error post-fix;
   `TestIfaceInstantiationSkippedMarker`. Direction chosen: fail fast
   (supporting instantiation method tables is the greppable re-test
   trigger).
5. `9412471` **wire pins + R2 F4**: `TestIfaceImplKey` (alias-expanded
   signatures share the interface's implkey; wrong signatures differ),
   `TestIfaceMultiResultScopeError` (two-result interface methods fail
   fast — the multi-result dispatch mechanics are untested, so the shape
   must not reach dispatch).
6. `b257cfc` **R2 P2-1 M9 classification**: 1=mono, 2–3=poly
   (PIC-resident), ≥4=mega (true PIC overflow, limit 3) — headline
   corrected to 41.9% / 51.6% / 6.5% (was 22.6% "mega").
7. `a4c9383` **R2 P2-2**: `megamorphicTransitions` now `AtomicLong`.
8. `d14100e` **R2 P2-3**: `freshSigID` appends a nil `typeList`
   placeholder (parallelism invariant);
   `TestFreshSigIDKeepsTypeListParallel` (red-green verified).
9. `cf30a67` **R2 P2-4**: `mentionsTypeParam` covers `Alias`/`Union`;
   `TestMentionsTypeParamAliasUnion` (red-green verified).
10. `4a198a2` **R2 P2-5**: dead code removed (unreachable
    interface→interface throw in `maybeConvert`; callerless 2-arg
    `holds`).
11. `a4222cc` **R2 P2-7**: unsupported-promotion fail-fast narrowed from
    whole-itab to per-method dispatch (the documented intent).
    Red-green: supported-only dispatch dies pre-fix, gc-identical
    post-fix; `testSupportedMethodBesideUnsupportedDispatches` (fails
    pre-fix), `testUnsupportedPromotionFailsFastAtDispatch`.
12. `7c1e218` **R2 F3**: Mutation C2 — same-arity GoFunc swap goes RED
    via output mismatch (9/10 vs 11/10), honestly scoping C's claim;
    remediation red-green evidence appended to `hidden_files/m3/redgreen.md`.
13. **R2 F6**: `verifies: GG-0017 (iface slice)` annotations on the iface
    code/tests + dated history entry in `requirements/GG-0017.yaml`.
14. **R2 F1 / R2 P2-6**: wire `index` field documented in ADR-0003 item 2
    and `frontend-v1.md` (path semantics + null case); `implkey` and
    `templateMethodsSkipped` documented alongside.

**Open items carried forward:**

1. **Phase-4 workload-representative polymorphism re-measurement (R2 F5).**
   The M9 re-measurement (`hidden_files/m3/polymorphism.md`: 41.9% mono /
   51.6% poly / 6.5% mega over 31 sites) is mechanism coverage, not a
   workload verdict — the probe set is adversarial by construction. The
   Phase-2 PLAN.md trigger's actual content ("(Phase 4 input)") needs a
   workload-representative interface-heavy program set, which requires
   the P3-M7+ frontend waves. **Re-measure at Phase-4 entry; the
   41.9/51.6/6.5 split must not be cited as a workload distribution.**
2. **R1 P1-2 re-test trigger:** when instantiation method tables are
   supported, delete the `templateMethodsSkipped` marker, the box-site
   checks, and the assertion sentinel (grep `templateMethodsSkipped` +
   `P3-M3 scope`).
3. **Type switches with interface cases** (`case Getter:`) never match a
   concrete dynamic type (identity-only matching in `TypeSwitchNode`) —
   observed during remediation, pre-existing, out of P3-M3 scope; needs
   a P3-M(n) work item if interface-case switches are required.

## 2026-10-06 — P3-M3 sign-off: interface dispatch (WS B)

**Sign-off commit:** `5fde351` (tree clean; only `phases/phase3/`, `phases/phase1/frontend/{export.go,main.go,iface_export_test.go,docs/frontend-v1.md}`, and `phases/phase1/src/main/java/graalgo/frontend/{NodeFactory,TypeAssertNode,TypeAssertOkNode}.java` touched — all covered by ADR-0003 + 2 amendments).

**Delivered:** `ifacemethod` wire nodes (gofrontend 0.4.0) for interface method calls; method tables on named/pointer type entries (incl. promoted methods, `implkey` signature keys, `index` receiver paths); `graalgo.iface` Java package — `IfaceDispatchNode` (PIC on itab identity, limit 3, megamorphic hash fallback, `GoCalls.invoke` protocol, nil-panic parity, `InterfaceDispatchProbes` per dispatch), `IfaceMethodTable` static registry installed by the launcher; three WS-D factory fail-fasts retired (concrete→non-empty-interface boxing, named-interface arg adaptation, interface type assertions); `--dump-polymorphism` launcher flag.

**Reviews:** code reviewer **NO-GO** (2 P0 silent divergences — name-only assertion check, non-canonical descriptors — both proven empirically; 2 P1, 9 P2), methodology reviewer **GO-WITH-FINDINGS** (F1–F8). All remediated in 16 one-change-per-commit commits with red-green evidence; final reports: `reviews/phase3-m3/reviewer1-code.md`, `reviewer2-methodology.md`.

**Final verification (sign-off tree `5fde351`, independent re-runs by coordinator):** phase-1 `make test` 208/208 + `make check` clean; phase-2 `make test` 194/194 + `make check` clean (pinning audit PASS, 0 unexpected); p3frontend `go test` ok; phase-3 `make test` 19/19; iface differential 13/13 byte-identical vs gc 1.27.1; sync differential 25/25. M9 re-measurement: 31 sites, 1227 dispatches — 41.9% mono / 51.6% poly (PIC-resident) / 6.5% mega (mechanism coverage, NOT a workload verdict; workload re-measurement deferred to Phase 4). GG-0014 →active; GG-0017 iface slice →active.

**P3-M3 is COMPLETE.** Next: P3-M4 (intrinsic surface completion).

## 2026-10-06 — P3-M4 sign-off: intrinsic surface completion (WS C)

**Sign-off commit:** `e6cddab` (implementation `6d690e9` + remediation through `1d76b54` + harness fix).

**Delivered:** GG-0020 audit published (`phases/phase3/hidden_files/m4/intrinsic-audit.md`): 286 intrinsic functions enumerated across phase-1/2/3 runtime classes — 248 REAL, 19 STUB routed to real implementations, 10 INTENTIONALLY-REMOVED (Netpoll, Phase-2 netpoller deletion), remainder documented no-ops-by-design (GcInterop ×3, write barrier — JVM owns it, each justified per-function). **0 stubs remain reachable from Go code.** Wiring: `//go:linkname` both directions (`LinknameIntrinsics`, `Linknames`), `Intrinsics.itabFor` → P3-M3 `IfaceMethodTable` (single iface-id namespace), `traceback()` with gc-parity call-site lines, atomics completions, `structSizeBytes` + gc-identical growslice caps. Phase-1/Phase-2 edits under ADR-0004 freeze exceptions (minimal, documented).

**Reviews:** code reviewer **NO-GO** (1 P0 — atomic cell cloning severed `*atomic.T` aliasing, proven vs gc; 2 P1 — `fastrandn(0)` threw vs gc's 0, innermost panic frame reported the definition line; 5 P2), methodology reviewer **GO-WITH-FINDINGS** (5 P2 — audit arithmetic, stale audit, one unverifiable red-green RED, missing re-test triggers, uncommitted evidence). All 13 findings remediated in 8 one-change-per-commit commits with red-green evidence (R1–R5); reports: `reviews/phase3-m4/reviewer1.md`, `reviewer2.md`.

**Supervisor-found defect (independent verification):** the map differential harness had a path bug — `NORM="$(cd "$(dirname "$0")" && pwd)/../traceback/normalize.py"` ran *after* the script's early `cd "$(dirname "$0")"`, so relative invocation resolved NORM to the nonexistent `/../traceback/normalize.py`, silently emptying the normalized output and failing the `.panic` anchor comparison 3/9. The product was correct (byte-identical output verified manually); the harness was not. Fixed in `e6cddab` (`NORM="$(pwd)/../traceback/normalize.py"`); map corpus now 9/9 via relative-path invocation. Lesson: differential harnesses must be exercised from the same working directory and invocation form the supervisor uses — "passes in CI" is not established by passing from the corpus dir.

**Final verification (sign-off tree, independent re-runs by coordinator):** phase-1 `make test` 208/208 + `make check` clean; phase-2 `make test` 194/194; p3frontend `go test` ok; phase-3 `make test` 29/29 + `make check` clean; corpora vs gc 1.27.1 — atomic 8/8, map 9/9, slice 6/6, fastrand 8/8, traceback 4/4, iface 13/13, sync 25/25. GG-0020 stays active (already activated 2026-10-04; 17 new `verifies:` annotations).

**P3-M4 is COMPLETE.** Next: P3-M5 (`time` + `context` lowering).

---

## P3-M5: `time` + `context` lowering (WS E) — IN PROGRESS (2026-10-06)

**Design:** ADR-0005 (`phases/phase3/adr/ADR-0005-time-context.md`, committed `e9b102b` before any code): synthetic `time` package (clock/timer intrinsics onto Phase-2 timers + pure-Go `Duration`/`Time` value types) and synthetic `context` package as **real Go** on channels + synthetic time + P3-M3 interface dispatch (a combination test of M2+M3+M5). Wire nodes additive (v1), toolVersion 0.5.0→0.6.0. DIV-M3-1 regression in the loop (corpus tc07 + Java unit test). Accepted divergence: `time.Local` ≡ UTC until P3-M7's real package.

**Work split:** exporter worker (synthetic packages, exporter interceptions, p3frontend mirrors, timectx corpus tc00–tc16, red-green) + Java worker (`graalgo.p3time`, launcher registration, DIV-M3-1 unit regression). Spike-first risks: `any == any` (valueCtx keys), nil-channel blocking.

**GG-0017 time/ctx slice:** activates when the first `verifies: GG-0017` timectx test lands.

**Scope expansion (coordinator decision 2026-10-06, recorded):** both
implementation workers completed their briefs, but integration exposed two
gaps the plan had assumed away: (a) the Go frontend has NO channel
operations — P2-M2 was Java-level only and its README explicitly deferred
`SendStmt`/`SelectStmt`/`< -ch`/`close` to "the exporter lowering (parent
track)"; P3-M5 is that track, so the milestone now includes the frontend
channel lowering (Appendix C); (b) v1 cannot express multi-result calls,
so the 6 multi-result operations use targeted call-site desugar
(Appendix M; precedent: P3-M2 defer/go desugar). The context
implementation is pure Go on the new channel ops (channel-mutex +
synchronous parent propagation — no leaked goroutines, since the
launcher's `awaitQuiescence` hangs on parked goroutines unlike gc).
Acceptance criteria unchanged. Follow-up workers dispatched for both.
