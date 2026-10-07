# P3-M7 Methodology Review (reviewer 2)

**Scope:** red-green evidence, differential corpus, drift tests, process
compliance for the wave-1 synthetic stdlib packages.
**Tree reviewed:** `~/workspace/graalgo-m7-int`, branch `master`,
HEAD `a3748a5` at review start (HEAD moved `2331e20` → `a3748a5`
*while* I was reviewing — the tree is under concurrent modification;
see §5).
**Verdict: REQUEST_CHANGES.**

The drift tests are genuinely good. Everything else has at least one
load-bearing hole. The single most serious finding: the strconv/json
integration (commit `bcfa29b`) silently dropped working functionality
that was green on the worker branch — the `mrStrconv`/`mrJson`/
`mrJsonMethod` multi-result desugar, the phase-1 export tests, and both
red-green evidence files — and committed differential corpora whose
programs cannot run on the graalgo leg. That is a red corpus committed
as green, plus lost evidence.

---

## 1. Red-green evidence: strong narratives, mostly unverifiable artifacts

### 1.1 What is genuinely good

- **errors+sort** (`hidden_files/m7-errors-sort/redgreen.md`): four
  observed exporter scope rejections with exact error strings, a real
  transcription bug (`reverseRange`: `j++` → `j--`), a real shadowing
  type error, a real runtime NPE with minimal reproducer and root cause
  (`TypeAssertNode` on Java-null interface), and a real stale-binary
  trap (Makefile `GOFRONTEND_SRC` list; binary 02:27 vs source 02:32).
  §4 documents worktree isolation and hunk-by-hunk assembly. This is
  the standard the other packages should have met.
- **math** (`m7-math/redgreen.md`): R1–R4 are observed failures with
  exact diffs (`sin 1e300` → −0.9758 vs −0.8178; `negative shift amount`
  panic; `could not import math`; `call to unknown function
  Float64frombits`). The StrictMath-vs-gc differential (11k–35k
  mismatches/50k, only Sqrt/FMA bit-identical) is the right experiment
  and it correctly killed the JVM-intrinsic shortcut.
- **strings** (`m7-strings/redgreen.md`): a real porting bug caught by
  the differential (`DecodeLastRune` back-scan on `"\xff\x41"`), fixed
  battery (~200 cases) + 4000-iteration adversarial fuzz, all matching
  gc. The harness survives on disk
  (`~/workspace/graalgo/phases/phase3/hidden_files/m7-tmp/strings/synthcheck/`,
  `cmd/` + `cmd/fuzz/main.go`).
- **encoding/xml** (`m7-enc-xml/redgreen.md`): 5 mutations each verified
  to fail ≥1 test, plus 3 real bugs found by the white-box differential
  (`%U` prefix, `autoClose` case-folding + stack discipline, 4096-byte
  encoder buffering). The difftest harness survives on disk
  (`m7-tmp/enc-xml/difftest/`).
- **strconv / encoding/json** (worker branch `m7-wstr-json@76df1fd`,
  files since dropped — see §4): the strconv redgreen records real reds
  (NaN payload `0xfff8000000000000` vs gc's `0x7ff8000000000001`;
  interface-conversion `binary.fields` failure; in-function `const`
  rejection) and extensive differential coverage (20k float fuzz,
  5k FormatFloat fuzz, full IsPrint sweep). This was good evidence.
  It is no longer in the tree.

### 1.2 The evidence that cannot be audited

The redgreen files cite artifacts that do not exist:

- **bytes** cites `m7-tmp/bytes/run6.log` (the "22 passed, 0 failed"
  run), `m7-tmp/bytes/muttest.sh` (the M1/M2 mutation helper), and
  `m7-tmp/bytes/repro/` (the ClassCastException repros). I searched
  `~/workspace/graalgo/phases/phase3/hidden_files/m7-tmp/bytes/` and
  the whole `m7-tmp` tree: **none of these exist**. The M1 (`n++` →
  `n += 2`) and M2 (`'A'` → `'B'`) mutation claims are prose-only.
  The bytes corpus "22 passed" claim has no log behind it.
- **math**: `m7-tmp/math/` is **empty**. No logs for the 50,071-input
  adversarial differential, no logs for R1–R4. Prose-only.
- **encoding/xml**: `m7-tmp/enc-xml/diffrun.out` exists — and it shows
  a **failing** run (`panic: xml.Decoder.Token: multi-result calls need
  WS D … exit status 2`). There is no green difftest log on disk.
- **strings**: no run log for the "byte-identical" end-to-end claim.
  (`t01.gc.out`/`t01.gr.out` survive in `m7-tmp/strings/` from an
  earlier iteration, but nothing ties them to the committed corpus.)

Root cause: commit `d3abc42` gitignores `m7-tmp/` (build artifacts),
so the evidence was deliberately excluded from the tree — but then it
was also never preserved anywhere else for three packages. The
checklist item 7 demands "the exact commands, the failing/passing
diffs, and the commit each side was run against." For bytes and math,
that record does not exist in any form I can find.

**Required:** re-run the bytes M1/M2 mutations and the math corpus on
the integration tree, capture the logs, and commit them (or put them
under `hidden_files/` outside the ignored paths). Until then the
"non-tautological" claims for bytes and math are asserted, not
demonstrated.

---

## 2. Differential corpus

### 2.1 Adversarial quality: good where it exists

Spot-checked programs are genuinely adversarial, not smoke tests:

- `sort/06_adversarial_shapes.go`: drives pdqsort's hint paths
  (sorted → `partialInsertionSort`, reverse → `decreasingHint` →
  `reverseRange`, all-equal → `partitionEqual`), duplicates-heavy,
  sawtooth — byte-compared against gc's pdqsort, so a wrong hint or
  swap count fails.
- `sort/05_nan.go`: NaN-first ordering, `SearchFloat64s` on NaN-prefixed
  slices.
- `bytes/07_adversarial.go`: empty/nil inputs, malformed UTF-8
  (truncated, surrogates, overlong), 100k inputs. Honest comments mark
  which paths crash the v1 runtime.
- `strconv/00_basic.go` + `02_adversarial.go`: MaxInt64±1, base prefixes,
  invalid underscores, subnormal/hex-float parse strings, bit-exact
  comparison via `Float64bits`.
- `encoding/base64/02_adversarial.go`: corrupt input, bad padding,
  `CorruptInputError` values, exactly-sized decode buffers.
- `encoding/json/02_adversarial.go`: deep nesting, key sorting, float
  shortest-repr edges, full control-char escapes, `json.Number`
  literal preservation.
- READMEs (`bytes`, `sort`, `strings`, `encoding/*`) honestly enumerate
  untested surface (WS D multi-result, runtime crashes, pinned
  omissions) with re-test triggers. This is the right discipline.

### 2.2 Three corpus runners are broken (wrong directory name)

The thin wrappers mandated by the checklist fail:

| Wrapper | Passes | Actual dir | Result |
|---|---|---|---|
| `encoding/json/run_differential.sh` | `json` | `encoding/json` | `unknown package dir: json`, exit 2 |
| `encoding/csv/run_differential.sh` | `csv` | `encoding/csv` | `unknown package dir: csv`, exit 2 |
| `encoding/gob/run_differential.sh` | `gob` | `encoding/gob` | `unknown package dir: gob`, exit 2 |

(Verified by executing the json wrapper; csv/gob wrappers, added in
`a3748a5`, repeat the same bug.) The eight `encoding/<name>` wrappers
that pass `encoding/base64`-style paths work. **The documented command
`bash phases/phase3/corpus/wave1/encoding/{json,csv,gob}/run_differential.sh`
fails.** The json corpus — which exists and is non-trivial — has
therefore never been runnable via its documented path.

### 2.3 strconv and json corpora are committed red on the graalgo leg

`bcfa29b` integrated strconv/json wiring **without** the
`mrStrconv`/`mrJson`/`mrJsonMethod` desugar kinds (present on the
worker branch `m7-wstr-json@5e21c5e`, 12 references → 0 on master;
verified by grep). Its own commit message admits: *"multi-result calls
currently fail fast with M7 scope errors."*

But the corpus programs committed in the same integration use exactly
those call shapes:

- `strconv/00_basic.go`: `v, err := strconv.ParseInt(tc.s, tc.base,
  tc.bitSize)` — 2-target `:=`, multi-result → fail-fast, no desugar.
- `encoding/json/00_marshal.go`, `02_adversarial.go`:
  `b, _ := json.Marshal(...)` — same.

So on the integration tree, the graalgo leg of the strconv and json
corpora fails at export with the named M7 scope error. **A red corpus
was committed as part of a "green" integration.** The commit message
discloses the gap as a "follow-up," but the standing principle is
"issues get fixed, not just documented" — and there is no tracked
follow-up item, no re-test trigger in the tree, and no marker in the
corpus README that these programs are expected-red.

**Required:** either land the `mrStrconv`/`mrJson` desugar (it exists
on `m7-wstr-json`, already reviewed and tested there) or mark the two
corpora expected-fail with a tracked re-test trigger. Do not leave a
red corpus that the checklist calls green.

### 2.4 The graalgo leg has no integration-time evidence for 10 packages

- The 8 enc-core packages: `m7-wenc-core/redgreen.md` §4 says the
  graalgo leg "cannot run pre-integration … it runs at integration."
  The integration merge `288c570` reports only "Phase-1 frontend: ok
  (9.2s). p3frontend: ok (34.7s)" — **no corpus run is recorded**.
  The per-package READMEs still say "the graalgo leg runs at
  integration when the p3frontend wiring lands." The wiring has landed;
  the run has not (or was not recorded).
- csv/gob (`a3748a5`): commit message says "gc-clean" — gc leg only,
  and the runners are broken (§2.2) anyway.
- math: `run_differential.sh` supports `GRAALGO_HOME` override, but no
  log ties a run to the integration tree.
- strings: the only surviving run logs (`m7-tmp/strings-verify/run*.log`,
  04:50–05:11 UTC) show **0 passed, 20 failed** — run against a tree
  whose exporter rejected `import "strings"` entirely
  (`WS G import overlay`). Whoever ran the verification ran it against
  the wrong tree state, which leads to §2.5.

### 2.5 The shared runner cannot test the tree under review

`corpus/wave1/run_wave.sh` hardcodes `GR="$HOME/workspace/graalgo"` —
a *different worktree* of the same repo, currently on branch
`m7-enc-xml` with uncommitted changes (verified: `git worktree list`,
`git status`). Every "corpus green" claim in every redgreen file is
therefore a claim about *some* state of *some other* worktree at
*some* earlier time — never about `graalgo-m7-int@HEAD` via the
documented command. Only `math/run_differential.sh` documents a
`GRAALGO_HOME` override; the shared runner has none.

**Required:** parameterize the runner (`GRAALGO_HOME`, defaulting to
the enclosing repo root, not a hardcoded sibling worktree) so the
integration tree can actually be tested by its own documented command —
and then run the full wave-1 corpus against the integration tree and
record the results.

---

## 3. Drift tests: genuinely good (with one inconsistency)

The p3frontend drift tests are the strongest methodology element in
this milestone:

- Each test extracts the **real** phase-1 synthetic source
  (`synth_<pkg>.go` const), parses and type-checks it with `go/types`,
  and compares the mirror declaration-by-declaration: funcs, methods,
  struct fields, vars, consts **including const values**
  (`strconv_test.go`, `json_test.go`).
- `errors_test.go`'s `assertScopeMatchesPhase1` checks **both
  directions** — the mirror must not export anything phase-1 lacks —
  and pins the generic `AsType` shape, the variadic `Join`, and the
  absence of unexported names.
- `enccore_test.go` checks the Appendix M pair/triple structs
  field-by-field, positionally (the desugar extracts by field index —
  the right thing to pin).
- All drift tests pass at HEAD (verified: full `p3frontend` suite
  green, 23.6s; phase-1 frontend suite green, 6.8s).

Non-tautological, verified by me: I mutated the strconv mirror
(`insertFunc("Atoi"` → `insertFunc("AtoiX"`), re-ran
`TestStrconvSyntheticSignatures` → **red** with
`mirror missing "Atoi" (phase-1 has it)`; reverted → green; tree left
clean. The drift tests are not mirror-existence checks.

One inconsistency: the strconv test only iterates phase-1 → mirror
(extra mirror names would pass silently); the errors test checks both
directions. Standardize on the errors pattern.

---

## 4. Process compliance

### 4.1 The integration dropped load-bearing hunks (serious)

Comparing worker branch `m7-wstr-json` against integrated master:

| Artifact | On `m7-wstr-json` | On master |
|---|---|---|
| `mrStrconv`/`mrJson`/`mrJsonMethod` desugar (`mrdesugar_m5.go`) | 12 refs (`5e21c5e`) | **0 refs — dropped** |
| `synth_strconv_json_export_test.go` (`TestSyntheticStrconvM7`, `TestSyntheticJsonM7`, arity fail-fast tests) | present (`76df1fd`) | **absent — dropped** |
| `hidden_files/m7-strconv/redgreen.md` | present (`76df1fd`, 152 lines) | **absent — dropped** |
| `hidden_files/m7-enc-json/redgreen.md` | present (`76df1fd`, 66 lines) | **absent — dropped** |

The content-based integration ("identify definitive file set by
content", `m7-integration.md`) lost these without any completeness
check — e.g., diffing the worker branch's file list or test inventory
against the integrated tree would have caught it instantly. This is
the exact failure mode the repo's own AGENTS.md lessons warn about
(concurrent-edit drops, "check `git diff --stat` before committing a
concurrently-edited living document"). The desugar drop is not just
lost evidence — it is a **functional regression** versus the worker
branch (see §2.3).

### 4.2 Parked known issues

- strconv/json desugar "follow-up": disclosed in a commit message,
  corpus committed red (§2.3), no tracked item, no re-test trigger in
  the tree. Violates "issues get fixed, not just documented."
- The m7-integration.md open questions (csv/gob duplicate
  reconciliation, stash inspection) were resolved in prose; fine.
- Re-test triggers in redgreen files are otherwise good (WS D, P3-M8,
  runtime fixes, Go version changes all named).

### 4.3 Docs (checklist item 8)

`p3frontend/DESIGN.md`'s synthetic-package table documents only
`errors` and `sort` for wave-1 (commit `a4fa2bf`). The other packages —
strings, bytes, strconv, math, encoding/json, encoding/xml,
encoding/csv, encoding/gob, and the 8 enc-core primitives — are not in
the table. The checklist requires updating "every doc that names the
package's status" in the same change. Partially done.

### 4.4 Hunk-by-hunk / shared-tree discipline

- The errors-sort worker documents worktree isolation and hunk-by-hunk
  assembly (`m7-errors-sort/redgreen.md` §4) — good.
- The integration itself was assembled by content cherry-pick; §4.1
  shows the failure mode. Recommend: for each worker branch, a
  mechanical file-list diff (branch vs integrated tree) as an
  integration gate.
- Minor: `zz_debug4_test.go` (references `/tmp/p1.go`) and debug
  `println`s in `export.go` appeared as uncommitted changes during my
  review — a worker debugging live on the integration tree, in
  violation of the wave-1 tmp discipline (`/tmp` is never to be used)
  and of the "clean tree" expectation. Transient, but it means any
  "suites green" claim about this tree is timestamped, not durable.

---

## 5. Review-environment caveat

HEAD moved `2331e20` → `a3748a5` during this review, and uncommitted
debug edits appeared mid-review. I re-verified suite state at the end
(phase-1 frontend: ok; p3frontend: ok, both at `a3748a5` plus the
transient debug dirt). Findings above are against committed state
unless noted.

---

## Required changes (blocking)

1. **Restore the dropped strconv/json desugar** (`mrStrconv`,
   `mrJson`, `mrJsonMethod` from `m7-wstr-json@5e21c5e`) **or**
   mark the strconv/json corpora expected-fail with a tracked re-test
   trigger. A red corpus must not ship as green. (Also restores the
   phase-1 export tests `TestSyntheticStrconvM7` etc.)
2. **Restore `m7-strconv/redgreen.md` and `m7-enc-json/redgreen.md`**
   from `m7-wstr-json@76df1fd` into `hidden_files/`.
3. **Fix the three broken corpus wrappers** (`encoding/json`,
   `encoding/csv`, `encoding/gob` → pass `encoding/<name>`).
4. **Parameterize `run_wave.sh`** (`GRAALGO_HOME`, default = enclosing
   repo root) and **run the full wave-1 corpus against the integration
   tree**, recording per-package results; update the enc-core READMEs'
   "runs at integration" notes with the actual outcome.
5. **Produce auditable red-green artifacts** for bytes (M1/M2 mutation
   logs) and math (corpus + R1–R4 logs), committed under
   `hidden_files/` (not the gitignored `m7-tmp/`), or downgrade the
   claims to "narrative, logs not retained."
6. **Complete DESIGN.md's synthetic-package table** for all wave-1
   packages (checklist item 8).
7. **Add an integration completeness gate**: file-list/test-inventory
   diff between each worker branch and the integrated tree, so dropped
   hunks (§4.1) fail the integration instead of shipping silently.

## Non-blocking (recommended)

8. Standardize drift tests on the both-directions pattern
   (`assertScopeMatchesPhase1`); the strconv test currently misses
   extra-mirror-name drift.
9. Keep evidence artifacts (run logs, repros, harnesses) in the tree
   under `hidden_files/` rather than the gitignored `m7-tmp/`; the
   current split (some on disk in a sibling worktree, some nowhere)
   makes independent review nearly impossible.
10. The json wrapper bug and the csv/gob copies suggest the wrappers
    were never executed once — add a CI/smoke check that every
    `run_differential.sh` at least starts (dir discovery succeeds).

## What I verified myself

- Full `p3frontend` suite green at HEAD (23.6s); phase-1 frontend
  suite green (6.8s, excluding a transient debug test).
- Drift test non-tautological by direct mutation (strconv mirror
  `Atoi`→`AtoiX`: red with precise message; revert: green).
- `encoding/json/run_differential.sh` fails as documented
  ("unknown package dir: json"); csv/gob wrappers share the bug.
- `mrStrconv`/`mrJson` absent on master, present on `m7-wstr-json`.
- `m7-tmp/bytes/{run6.log,muttest.sh,repro/}` and `m7-tmp/math/`
  contents do not exist anywhere on disk.
- `m7-tmp/enc-xml/diffrun.out` shows a failing (not green) difftest run.
- `m7-tmp/strings-verify/run*.log` show 0/20 strings corpus failures
  against a tree lacking strings wiring (wrong-tree verification).
- `run_wave.sh` hardcodes `GR=$HOME/workspace/graalgo` (worktree on
  `m7-enc-xml`, dirty); the integration tree is not testable by the
  documented command.
