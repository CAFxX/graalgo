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
