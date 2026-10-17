# graalgo Fuzzing Campaign — Design

**Status:** Phase 1 (local harness + generator + 100-program smoke test).
**Owner:** graalgo-fuzzing-campaign (registry).
**Standing authority:** Carlo 2026-10-06 — continuous adversarial test-finding;
explicit "strong fuzzing campaign" request 2026-10-10.

## 1. Goal

Find real graalgo bugs differentially against gc 1.27.1: crashes, hangs,
wrong output, and frontend rejections of valid programs. The 2026-10-09
silent encoder divergence (flate emitted 6506 bytes where gc emitted 46 —
no crash, just wrong output) is the canonical example of what this must
catch.

## 2. Architecture

```
gen/generator.py  --seed N -->  fuzz_NNNN.go (deterministic Go program)
        |
harness/run_batch.py  (parallel driver)
        +-- diff_one.sh : gc build+run  vs  graalgo P3SyncMain run
        |       compare stdout bytes, exit codes, panic lines
        +-- classify: PASS | SCOPE | WRONG_OUTPUT | CRASH | HANG
                     | GO_PANIC_MISMATCH | EXIT_MISMATCH | GC_BUILD_FAIL
        +-- dedup by (category, signature)
        +-- triage.py : minimize reproducers (delta debugging)
```

### 2.1 Generator (`gen/generator.py`)

Seeded (`random.Random(seed)`), emits self-contained `package main`
programs. Design constraints (all learned from real M9 bugs):

- **Deterministic output**: no map iteration order (sort keys), no
  `time`, no unseeded `math/rand`, no goroutines (gen-1).
- **No panics by construction** (gen-1): divisors guarded non-zero,
  indices reduced modulo length, maps made before write, slices
  bounds-checked. Any panic under graalgo is then a bug signal.
- **Scope-safe**: no `for i := range n` (frontend can't range over
  int), no embedded structs, no generics, no `string(int)` conversion.
- **Bug-targeted literal pool**: integer edge values
  `0, 1, -1, 127, 128, 255, 256, 32767, 32768, 2147483647,
  -2147483648, 4294967295` — aimed at the uint32/Integer/Long boxing
  bugs (a2e3f7b, 487f48c, f05255d).
- **Type coverage**: int, int8..int64, uint..uint64, float32/64,
  string, bool, arrays, slices, maps (incl. uint32 keys — the silent
  flate divergence was a map-lookup miss), structs, pointers.
- **Construct coverage**: arithmetic/logic, if/else, classic for loops,
  for-range over slices, switch, helper functions (with params +
  returns), append/copy/delete/make, struct literals, pointer
  dereference, type conversions between int sizes.

Expression generator is recursive with depth limit and full type
tracking; statement generator emits declarations, assignments,
control flow, and a final deterministic state dump via `fmt.Println`.

### 2.2 Differential runner (`harness/`)

`diff_one.sh <go-file>`: 
1. `go build` (gc 1.27.1) → gc binary; build failure = generator bug.
2. Run gc binary, timeout 10s → stdout/exit.
3. Run `graalgo.p3sync.launcher.P3SyncMain --gmp=1 -- <file>`,
   timeout 120s (JVM startup dominates) → stdout/stderr/exit.
4. Emit a JSON result record.

`run_batch.py`: builds the Java modules once, generates N programs,
runs `diff_one.sh` in parallel (`-P 4` default, respects load-shed
backoff), classifies, dedups, writes `results.jsonl` + summary.

Classification:
| Result | Meaning |
|---|---|
| PASS | stdout byte-identical, exit 0/0 |
| SCOPE | graalgo stderr has "not supported in frontend/v1 scope" — not a bug; tracked as generator scope-hit rate |
| WRONG_OUTPUT | stdout differs (the silent-divergence class) |
| CRASH | JVM exception markers, not a Go panic |
| HANG | timeout |
| GO_PANIC_MISMATCH | panic behavior differs between legs |
| EXIT_MISMATCH | exit codes differ |
| GC_BUILD_FAIL | generator emitted invalid Go (generator bug) |

Dedup signature: `(category, first stderr line)` for crashes/scope,
`(category, sha1(stdout diff))` for wrong output.

### 2.3 Triage (`harness/triage.py`)

Delta-debugging minimizer: given a failing program, repeatedly remove
statements/functions while the failure signature persists. Produces a
minimal reproducer for the bug report.

## 3. Validation (no noise)

The harness must prove it finds real bugs before scaling:

1. **Injected-bug positive control**: flip a comparison/boxing path in
   a scratch copy of the runtime, run 100 generated programs, assert
   the harness reports WRONG_OUTPUT/CRASH. (Mutation evidence.)
2. **Known-bug retro**: the uint32 `a2e3f7b` regression — verify the
   generator's edge-value pool would have produced a failing program
   (re-run generator seeds against the pre-fix tree if available).
3. **Scope-hit rate**: if >20% of programs are SCOPE, tune the
   generator before scaling (noise control).

## 4. GHA scale-out (Phase 2/3)

Tenant `projects/graalgo-fuzz/` in CAFxX/fanout:

- `Dockerfile`: Ubuntu + Go 1.27.1 + GraalVM JDK 25 + graalgo sources
  (slim tarball: Makefile, go.mod, internal/backend/{truffle,runtime,interop},
  internal/frontend, tools/fuzz-soak; ~45MB, baked at image build).
  Driver + generator baked in.
- `driver/fuzz_driver.py`: honors `$SHARD_IDX`/`$SHARD_COUNT`,
  `$OUT_DIR`; each shard generates `N` programs from disjoint seed
  ranges, runs the differential, writes `results.jsonl`.
- `aggregate.py`: fetches shard results, dedups globally, emits
  bug-report candidates.
- Golden canary per skill rules: one known-pass program + one
  injected-bug program per shard batch; quarantine on mismatch.
- Job spec: `--seeds 0..9999 --shards 20` pilot (1000 programs).

Submission is via the PAT Git Data API pattern
(`~/workspace/skills/github-pat/bin/push_fanout.py`), one approval per
campaign. **Not submitted in Phase 1** — files prepared, parent
submits.

## 5. Continuous operation (Phase 3)

- Nightly campaign: N new seeds, dedup against known-bug registry.
- Registry `graalgo-fuzzing-campaign`: findings with reproducers,
  status (open/fixed), seed for repro.
- Every finding filed as a proper bug with minimized reproducer
  (standing directive).
- Generator evolution: add panics-by-design, goroutines/channels,
  interfaces, multi-result functions as frontend scope grows.

## 6. Files

```
fuzzing/
  DESIGN.md            this file
  gen/generator.py     seeded Go program generator
  harness/diff_one.sh  single-program differential
  harness/run_batch.py batch driver (parallel, classify, dedup)
  harness/triage.py    delta-debugging minimizer
  gha/Dockerfile       fanout image
  gha/driver/fuzz_driver.py
  gha/aggregate.py
  gha/README.md
  seeds/               interesting seed programs (from corpus)
```
