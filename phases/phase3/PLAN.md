# Phase 3 plan — Stdlib bring-up (Linux-first)

**Status:** ACCEPTED by program supervisor 2026-10-06 (independent plan review; no per-phase owner gate per the 2026-10-06 "to completion" directive). Implementation dispatched.
**Normative:** SPEC.md (ACCEPTED 2026-10-03) §§2.8, 3.2, 3.3, 3.6, 3.8, 5;
PHASES.md §5; phases/phase1/INTERFACE_FREEZE.md (v1, still in force);
phases/phase2/DONE-GATES.md + phases/phase2/CHECKPOINT.md tail (Phase 2
complete, sign-off 9694cbf, 2026-10-06).
GG IDs activate per-workstream below (activation policy carried from Phase 2:
`verifies: GG-XXXX` annotation when the first real test lands, never before).

## 1. Objective and scope

The full Go 1.27.1 stdlib (Linux-first) goes green against the intrinsic
surface. Concretely: the frontend learns to compile whole package
directories and resolve imports across a vendored stdlib tree (graduating
the Phase-1/2 synthetic import overlay); interface dispatch lands (PIC on
itab identity — the one load-bearing language feature Phase 1/2 left
unbuilt); the GG-0020 intrinsic surface is completed (~60–70 functions,
audited stub-vs-real); the deferred P2-M8 `sync` Go bindings land; the
P2-M6 `len()`-on-timer-channel divergence is fixed before `time` is
lowered; the adapted stdlib fork is limited to the ~78 coupled packages
with linkname-diff tooling, per-category asm strategy, build-tagged
`syscall`/`os`/`net`, and a versioned reason-coded exclusion list; and all
four testing-strategy layers (§5) are operational.

In scope: the Phase-3 GG IDs below. Out of scope: `-toolexec` driver wiring
(Phase 4), AOT/JIT modes (Phase 5), darwin/Windows (GG-0023 — deferred with
re-test triggers), cgo/Panama (Phase 4, GG-0022). The Phase-1 freeze is the
contract — Phase 3 builds against it; any FROZEN change requires the freeze
change policy (ADR + version bump), not a quiet edit. Phase-1 files are not
modified; Phase 2 is consumed only through its runtime classes and
DIVERGENCES.md.

**What Phase 3 builds on (not to be re-derived):**
- `phases/phase1/INTERFACE_FREEZE.md` — frozen frontend/runtime contract;
  `overlayImporter` (export.go) resolves synthetic `runtime` + `fmt` only;
  every other import is a scope error naming "WS G import overlay".
  Phase 3 *is* that overlay's graduation.
- Phase-2 runtime: scheduler, channels/select, timers, GC interop, JFR,
  `graalgo.runtime.sync.{GoMutex,GoRWMutex,GoWaitGroup,GoOnce}` (Java
  primitives, stressed at Java level — Go bindings are this phase's P3-M2).
- Phase-2 DIVERGENCES.md: DIV-M3-1 (`len()` on buffered timer channel
  returns 1, gc: 0) carries a **blocking** Phase-3 re-test trigger — fix
  before `time` is lowered (P3-M5).
- M9 polymorphism numbers (170 sites, 99.4% monomorphic): interface
  dispatch does not exist yet; the re-measure trigger fires when P3-M3
  lands (Phase-4 input per the Phase-2 plan becomes Phase-3 output).

## 2. Work breakdown (workstream → GG IDs → done-gate)

| WS | Workstream | GG IDs | Gate |
|---|---|---|---|
| A | Frontend package dirs + import resolution: `ExportDir` (multi-file packages, build-tag filtering per GG-0023), import resolution across the vendored stdlib tree, `overlayImporter` → real importer with synthetic bootstrap retained (`runtime`, `fmt` until their real packages land) | GG-0012, GG-0013, GG-0023 (build-tag slice) | 1 (part) |
| B | Interface dispatch: PIC on itab identity (limit ~3, megamorphic hash fallback per SPEC §3.3), type switches via decision trees, `GoInterface` graduates from data carrier to dispatch; polymorphism re-measurement (M9 trigger) | GG-0014, GG-0017 (iface slice) | 1 |
| C | Intrinsic surface completion: GG-0020 audit (stub-vs-real over ~60–70 fns), complete memmove/memequal, makeslice/growslice, map ops, itab machinery, atomics, fastrand, traceback, write barrier; linkname by symbol name both directions | GG-0020 | 1 |
| D | `sync` Go bindings + DIV-M3-1 fix: lower `import "sync"` onto the Phase-2 Java primitives (discharges the P2-M8 re-test trigger); fix `len()` on buffered timer channel to 0 (gc parity) with red-green-first evidence | GG-0017 (sync slice), GG-0048 | 1 |
| E | `time` + `context` lowering: `time` onto Phase-2 timers (after DIV-M3-1 fix); `context` via the new interface dispatch (P3-M3 is a hard prerequisite) | GG-0017 (time/ctx slice) | 1 |
| F | Adapted stdlib fork mechanics: vendoring pipeline (stdlib re-vendored atomically with go/* per SPEC §3.2), linkname-diff tooling operational (GG-0021), asm per-category strategy implemented (evaporate/intrinsify/emulate-then-intrinsify/JVM-covers), build-tagged `syscall`/`os`/`net` for Linux, versioned exclusion list with reason codes + review cap | GG-0009, GG-0021, GG-0023 | 5 (part) |
| G | Package-group bring-up in dependency order: wave 1 (errors, sort, strings, bytes, strconv, math, encoding/*), wave 2 (io, os, syscall, reflect+unsafe, crypto-emulate), wave 3 (net, net/http, compress, archive, remaining) — each wave differentially green vs gc before the next starts | GG-0009, GG-0024 (pprof slice, deferred impl) | 1, 4 |
| H | Testing layers 1–4 operational: layer 1 smoke corpus (per-commit), layer 2 conformance (per-commit on frontend changes), layer 3 `go test ./...` + script suites (per-release gate), layer 4 fuzz soak — interface reserved for the continuous adversarial test-finding track (see §3) | GG-0029, GG-0030, GG-0031, GG-0032, GG-0037 | 2 |
| I | Behavior snapshots + API/ABI diff: record behavior snapshots for Go 1.27.1 (the upgrade baseline); per-release API/ABI diff tooling produces a scoped work list on a trial re-run | GG-0042 (toil slice), GG-0021 | 3, 5 |
| J | Divergence closeout + final reviews: four-way triage per new failure (graalgo bug / upstream change / backend-specific / nondeterminism per SPEC §5.3); peer reviews per package group + self-attack (I/O fault injection, signal storms, fd exhaustion); cross-doc audit | GG-0010, GG-0034 | 4, 5 |

Activation policy (carried from Phase 2): GG ID →active per-workstream,
exactly when its first real test lands with a `verifies: GG-XXXX`
annotation; never before.

## 3. Architecture decisions (recorded)

- **P3-D1 [DECIDED, ADR-0001]: overlay graduation.** Phase-1/2's synthetic
  `overlayImporter` (synthetic `runtime` + `fmt` packages) is a bootstrap
  device, not the stdlib strategy. Phase 3 teaches gofrontend to compile
  whole package directories and resolve imports across a vendored go1.27.1
  tree; synthetic overlays are retained ONLY for `runtime`
  (Goexit/SetFinalizer/KeepAlive/GC) and `fmt` (seven D4 symbols) until
  their real packages land. The ~78 coupled packages are compiled from
  real (lightly adapted) sources, never re-synthesized.
- **P3-D2 [DECIDED, ADR-0001]: interface dispatch lands in Phase 3, not
  Phase 4.** The Phase-2 plan's "(Phase 4 input)" note covered only the
  polymorphism *re-measurement*; dispatch itself is load-bearing for
  `context`, `io`, `sort`, `reflect`, and most of the stdlib, so it must
  precede the package waves (P3-M3 before P3-M7). SPEC §3.3's PIC design
  (limit ~3, megamorphic hash fallback) is the implementation target.
- **P3-D3 [DECIDED, SPEC §2.8]: adapted fork, not clean-room stdlib.**
  229/398 package dirs (58%) compile unchanged against the frontend +
  intrinsic surface; the ~78 coupled packages get a fork with minimal
  diffs; Plan 9 asm per category (runtime-core evaporates, atomics/bytealg
  intrinsified, crypto emulate-then-intrinsify, `math` JVM IEEE754).
- **[OPEN] Exclusion-list review cap.** PHASES.md §5 kill criterion fires
  when "exclusion-list growth exceeds the review cap twice" — the cap
  number itself is undecided. Proposal: 25 packages OR 5% of stdlib test
  binaries, whichever is hit first, with reason codes mandatory per entry.
  Needs program-owner confirmation before P3-M6 (the list's first
  publication).
- **[OPEN] The ~78 coupled-package set.** SPEC §2.8's count is a
  measurement on an earlier tree; re-measure on go1.27.1 at P3-M6 and
  publish the versioned list. If the count drifts >15%, re-examine the
  fork-vs-overlay boundary (re-test trigger on P3-D1).
- **[OPEN] crypto "then-intrinsify" timing.** Phase 3 implements
  emulate-then-intrinsify with the *emulate* leg; the intrinsify leg is
  deferred to Phase 5 (it is a performance optimization, and every
  claimed win needs its falsifying benchmark per GG-0007). Confirm the
  deferral does not break the exclusion-list cap.
- **[OPEN] `net` scope.** Blocking NIO is proven (Phase 2); `net`,
  `net/http` are in scope, but the full `net/*` subtree (e.g.
  `net/http/httptrace` edge cases, `net/rpc`) may exceed the cap —
  triage per package at P3-M9 with the four-way classification.

**Continuous adversarial test-finding track (standing owner directive
2026-10-06, NOT staffed by this plan):** the supervisor will launch a
parallel track whose sole focus is finding failing test cases
(differential vs gc, crashes, hangs, wrong output), running continuously
alongside Phase-3 development, as soon as the supervisor judges it
productive. This plan reserves the interface only: (a) findings arrive as
bugs with reproducers against the §5 differential oracle; (b) minimized
cases become permanent regressions per SPEC §5.4; (c) the track consumes
the layer-4 fuzz-soak slot in the per-release gate. No Phase-3 milestone
depends on it; no Phase-3 worker staffs it.

## 4. Milestones and acceptance

- P3-M1: frontend package dirs + import resolution (WS A) — `ExportDir`
  compiles a multi-file package; imports resolve across the vendored
  tree with build-tag filtering; synthetic bootstrap retained; a
  two-package program (`main` + helper) runs differentially vs gc
  (GG-0012, GG-0013, GG-0023 build-tag slice →active)
- P3-M2: `sync` Go bindings + DIV-M3-1 fix (WS D) — `import "sync"`
  lowers onto the Phase-2 Java primitives; the P2-M8 stress suite
  re-runs as Go programs differential vs gc (discharges the P2-M8
  re-test trigger); `len()` on buffered timer channel returns 0 like gc,
  red-green-first (GG-0017 sync slice →active)
- P3-M3: interface dispatch (WS B) — PIC on itab identity, type switches;
  `io.Writer`-shaped programs + interface-heavy corpus differential vs
  gc; M9 polymorphism re-measured and recorded (GG-0014, GG-0017 iface
  slice →active)
- P3-M4: intrinsic surface completion (WS C) — GG-0020 audit published
  (every one of the ~60–70 functions classified stub-vs-real, then all
  real); map/slice/atomics/fastrand/traceback differential vs gc
  (GG-0020 →active)
- P3-M5: `time` + `context` lowering (WS E) — `import "time"` onto
  Phase-2 timers (DIV-M3-1 fix verified in the loop), `import
  "context"` via P3-M3 dispatch; cancellation/deadline programs
  differential vs gc (GG-0017 time/ctx slice →active)
- P3-M6: stdlib fork mechanics (WS F) — vendoring pipeline green,
  linkname-diff tool operational on a trial pair, asm per-category
  strategy implemented, exclusion list v1 published (versioned,
  reason-coded, under the review cap), coupled-package set re-measured
  (GG-0009, GG-0021, GG-0023 →active)
- P3-M7: package wave 1 (WS G) — errors/sort/strings/bytes/strconv/math/
  encoding/* `go test` green vs gc (exclusion list cited per skip)
- P3-M8: package wave 2 (WS G) — io/os/syscall(linux-tagged)/
  reflect+unsafe/crypto(emulate) `go test` green vs gc
- P3-M9: package wave 3 (WS G) — net/net/http/compress/archive/remaining
  `go test` green vs gc; full `go test ./...` passes with zero
  unexplained divergences (GG-0010 →active)
- P3-M10: layers + snapshots + closeout (WS H/I/J) — all four testing
  layers operational with the per-commit/per-release cadence wired;
  behavior snapshots recorded for go1.27.1; API/ABI diff tooling trial
  re-run produces a scoped work list; per-group reviews + self-attack
  (fault injection, signal storms, fd exhaustion) remediated; cross-doc
  audit → gates 1–5 → closeout (GG-0029/0030/0031/0032/0037, GG-0034,
  GG-0042 toil slice →active)

**Review gate (program-owner directive 2026-10-04, carried from Phases 1–2):**
every milestone P3-M1–P3-M9 closes with ≥2 independent peer reviews
(code + methodology, generated artifacts included per the standing rule);
every finding is remediated and re-tested before sign-off. Reviews run
against a fixed snapshot (never the moving live tree); evidence lives
under `reviews/`. P3-M10 retains the final cross-cutting review:
self-attack, grep gates, cross-doc consistency audit, closeout.

**Lessons carried over (non-negotiable):**
1. The supervisor (or a second worker) independently re-runs the full
   suite and `make check` on the sign-off tree before accepting any
   milestone sign-off — a worker's reported suite number is a claim,
   never evidence.
2. No sign-off on stale numbers: every number in the sign-off entry
   cites its evidence file.
3. Differential evidence required: every new semantic lands
   red-green-first vs gc.
4. Every escaped bug: fix the instance, hunt the whole error class,
   patch the process.
5. One logical change per commit; docs updated in the same commit.

## 5. Test strategy

`graalgo-run` graduates from single-file to package mode: `graalgo-run
--gmp=N ./pkg/...` compiles the vendored tree subset through the new
importer and executes `main`/tests on the user-space scheduler.
Differential oracle vs stock gc 1.27.1 per SPEC §5.5 (stdout/stderr bytes
post-normalization, exit codes, panic messages). Layers (SPEC §5):
(1) smoke — the curated ~200-program corpus, per-commit; (2) conformance —
`src/test/` language suite (356 files), per-commit on frontend changes;
(3) stdlib + scripts — `go test ./...` as the primary corpus, hours-scale,
per-release gate, four-way triage per red (graalgo bug / upstream change /
backend-specific+reason-code / nondeterminism-normalization); (4) fuzz
soak — `testing.F` differential fuzzing + program generation, overnight
per release, slot reserved for the continuous adversarial track.
Fault injection from day one: unpaired `ioEnter`, import-cycle attempts,
build-tag mismatches, linkname-missing symbols — all fail-fast with
named errors. The Phase-1 (208) and Phase-2 (193) suites stay green
throughout; GOMAXPROCS=1 remains observationally identical to Phase 2.

## 6. Done-gates (binary pass/fail, from PHASES.md §5)

1. **`go test ./...` green.** The full stdlib test suite passes with zero
   unexplained divergences vs gc 1.27.1. The exclusion list is versioned,
   reason-coded, and under the review cap. Evidence:
   `phases/phase3/hidden_files/stdlib/GO_TEST_ALL.md` + archived logs;
   differential JSON with 0 unexplained divergences. FAIL = any
   unexplained divergence, or cap exceeded.
2. **Four layers operational.** Smoke → conformance → stdlib+script →
   fuzz soak all wired to their cadence (per-commit / per-release /
   overnight). Evidence: `phases/phase3/hidden_files/testing/LAYERS.md`
   with run receipts. FAIL = any layer unwired or red.
3. **Behavior snapshots recorded.** For go1.27.1: the upgrade baseline
   (stdlib test outcomes, exclusion list, intrinsic coverage, linkname
   map). Evidence: `phases/phase3/hidden_files/snapshots/`. FAIL =
   missing or unversioned snapshot.
4. **Reviews + self-attack.** ≥2 independent reviews per package group,
   all findings remediated and re-tested; self-attack (I/O fault
   injection, signal storms, fd exhaustion) green. Evidence:
   `reviews/phase3-m*/`. FAIL = any unremediated finding.
5. **API/ABI diff tooling.** Per-release diff produces a scoped work list
   on a trial re-run (trial: run the tooling against a synthetic
   next-release delta). Evidence:
   `phases/phase3/hidden_files/apidiff/TRIAL.md`. FAIL = tooling absent
   or unscoping.

## 7. Risks and kill triggers

**Kill criteria (PHASES.md §5, verbatim):**
- "exclusion-list growth exceeds the review cap twice (long-tail
  divergence)" — falsifying experiment: the per-wave `go test ./...`
  triage (P3-M7/M8/M9). Two consecutive cap breaches → escalate to
  program owner; the stdlib is longer-tailed than the fork model
  supports.
- "linkname churn defeats the diff tooling's scoping" — falsifying
  experiment: the linkname-diff trial at P3-M6 plus the P3-M10 trial
  re-run. If skip-list growth > 15 twice (SPEC §3.6 kill trigger), the
  per-release toil bound is broken → escalate.

**Other risks:**
- Interface dispatch (P3-M3) is the largest unbuilt language feature;
  if the PIC design proves unworkable on the value model, P3-M5/M7–M9
  cannot proceed — P3-M3 begins with a design spike + ADR if the
  straight implementation fails, not with silent scope cuts.
- The 58% "compiles unchanged" measurement may not reproduce on
  go1.27.1 — P3-M6 re-measures; drift >15% re-opens P3-D1.
- `reflect`'s 243 `unsafe` uses depend on exact rtype/itab layouts
  (SPEC §3.6); layout drift is a silent-compat killer — the layouts are
  machine-pinned like the Phase-1 freeze.

## 8. Closeout checklist

[ ] gates 1–5 evidenced (named artifacts in §6) [ ] per-milestone review
gates closed (P3-M1–P3-M9: ≥2 independent reviews each, every finding
remediated at named targets and re-tested) [ ] self-attack report
[ ] grep gates (no placeholders; linkname coverage audit) [ ] cross-doc
consistency audit [ ] numbers table with log-line citations [ ] GG
states recorded (→active per workstream as tests land) [ ] exclusion
list versioned + under cap [ ] behavior snapshots recorded [ ]
divergence triage closed or accepted (four-way classification per entry)

## 9. Falsifiers per owned GG ID

- GG-0012/GG-0013: package-dir compile rejects a valid multi-file
  package, or import resolution misresolves across the vendored tree →
  red
- GG-0014: interface call dispatches to the wrong method, or PIC
  megamorphic fallback diverges vs gc → red
- GG-0017 (sync/time/ctx/iface slices): any lowered package semantic ≠
  gc differentially → red
- GG-0020: any intrinsic in the audited surface still stubbed at P3-M4
  sign-off, or intrinsic ≠ gc differentially → red
- GG-0021: linkname-diff misses a real symbol churn on the trial pair →
  red
- GG-0023: a linux-tagged file compiled on the wrong GOOS, or a
  darwin/windows-only path reached → red
- GG-0009: a coupled package outside the fork compiles against the
  surface unchanged (measurement drift) → re-examine P3-D1
- GG-0029/0030/0031/0032/0037: any testing layer unwired at P3-M10, or
  oracle unvalidated by mutation → red
- GG-0034: escaped bug without error-class hunt + process patch → red
- GG-0042: per-release toil accounting missing or P95 bound unchecked →
  red
- GG-0010: testing strategy not built early (layers land after the code
  they should gate) → red
