# Spike 7 — testing-harness seed: REPORT

**Date:** 2026-10-03 · **Verdict: PASS** · **Scope:** FEASIBILITY.md §7 smoke
layer (differential-vs-gc harness), validated gc-vs-gc with injected
mutations since graalgo does not exist yet.

## Verdict

**PASS** — the harness is operational, CI-green, and mutation-validated:

1. Unmutated corpus, gc-a vs gc-b: **45/45 SAME, 0 false positives** (K2 not fired).
2. Rerun control (each program twice under gc-a): **45/45 SAME, 0 divergences** (K3 not fired).
3. Mutation validation: **9/9 classes DETECTED, 0 missed** (K1 not fired).
4. Harness unit tests: **all green** (`go test ./internal/...`), `go vet` clean, `gofmt` clean.

Kill triggers (named upfront): K1 — any mutation class undetectable after
reasonable tuning → FAIL. K2 — systematic unmutated-corpus false positives
not tameable by documented normalization → FAIL. K3 — rerun-control
divergence (flakiness) → FAIL. None fired.

## What was built

`~/workspace/graalgo/tools/diffharness/` (Go module `diffharness`, no
external dependencies):

- `cmd/diffharness` — CLI with `run`, `mutate`, `validate` subcommands.
- `internal/harness` — JSON backend config; build+run backend runner
  (minimal scrubbed env, `GOMAXPROCS=1` pinned, fresh empty cwd,
  per-program timeout); trace/address normalizer; outcome comparator.
- `internal/mutate` — go/ast mutation generator, 9 classes, one site per
  mutant, position-tagged descriptions.
- `corpus/` — 45 single-file stdlib-only Go programs covering the language
  surface (control flow, closures, methods, interfaces, slices/maps,
  goroutines+channels, select, defer/panic/recover, generics, type
  switches, reflect, json, range-over-func, …). See `corpus/README.md`.
- `backends.json` — two backends (`gc-a`, `gc-b`), both stock go1.27.1;
  `mutations.txt` manifest; `run.sh` CI script; `README.md` (seam docs).

## Reproduction (exact commands)

```bash
cd ~/workspace/graalgo/tools/diffharness
./run.sh
# exit 0 == all green; reports in ./test-results/
```

`run.sh` needs only a Go toolchain (`DH_GO` → toolchain root, else
`~/tools/go`, else `PATH`; `GOTOOLCHAIN=local` pinned). Steps: build →
vet+gofmt → unit tests → green run (gc-a vs gc-b) → `validate` (green +
rerun control + mutation red-green) → sha256 archive of all harness
sources alongside the logs (measurement honesty).

Manual spot checks:

```bash
./run.sh 2>&1 | tail -3
go run ./cmd/diffharness mutate -corpus corpus -class ChanOp -target 09_pipeline
go run ./cmd/diffharness validate -config backends.json -corpus corpus \
  -mutations mutations.txt -workdir "$TMPDIR/dh-mut" -out "$TMPDIR/v.json"
```

## Mutation-validation evidence (red)

9 classes × 1 manifest instance each. Every mutant compiled under both
backends; every one was DETECTED (harness reported DIVERGE):

| # | Class      | Target                 | Mutation (first eligible AST site)                  | Divergence signature (original vs mutant, same backend) |
|---|------------|------------------------|-----------------------------------------------------|----------------------------------------------------------|
| 1 | OpSwap     | 02_arithmetic          | `a + b` → `a - b` (main.go:7)                      | stdout line 1: `22` → `12`                               |
| 2 | CmpSwap    | 03_control_flow        | `n < 10` → `n <= 10` (main.go:6)                   | stdout line 2: `10 medium` → `10 small`                   |
| 3 | OffByOne   | 04_for_range           | `i < 5` → `i < 6` (main.go:6)                      | stdout line 1: `0 1 2 3 4` → `0 1 2 3 4 5`              |
| 4 | DropDefer  | 06_nested_defer        | removed `defer fmt.Println("first")` (main.go:6)    | stdout line 6: `first` → (missing)                       |
| 5 | CondNegate | 07_two_sum             | `if c` → `if !(c)` (main.go:10)                    | stdout line 1: `0 1` → `0 2`                             |
| 6 | ConstChange| 01_hello               | `"hello, world"` → `"hello, world!"` (main.go:5)    | stdout line 1: `hello, world` → `hello, world!`          |
| 7 | ReturnSwap | 08_multiple_assignment | `return a/b, a%b` → `return a%b, a/b` (main.go:5)   | stdout line 1: `3 2` → `2 3`                             |
| 8 | ChanOp     | 09_pipeline            | `out <- v*v` → `{ _ = v*v; <-out }` (main.go:11)   | exit 0→2; stderr: `fatal error: all goroutines are asleep - deadlock!` |
| 9 | SliceBound | 05_slices              | `s[1:4]` → `s[1:4-1]` (main.go:7)                  | stdout line 1: `[1 2 3]` → `[1 2]`                       |

Every mutant compiled under the backend; every one was DETECTED.

## Bugs found by the spike itself (fixed, re-validated)

The first full validation run exposed three real defects — two in the
validation experiment/mutator, one in the harness — all fixed and re-run
green. They are recorded here rather than hidden because the first one in
particular is the exact failure mode mutation validation exists to catch.

1. **Vacuous validation experiment (experiment design).** The first
   `validate` compared each *mutant* under gc-a vs gc-b — two identical
   backends running the same mutant, which must agree. Result: 8/9
   "MISSED", all bogus. The correct oracle question is whether the
   *comparator* flags a behavioral change, so `validate` now compares each
   mutant against the *original* program under the *same* backend (reusing
   the green-run outcomes). After the fix: 9/9 DETECTED. Lesson: a
   red-green setup must itself be red-green-checked — a validation that
   cannot fail is decorative. The `validate` step now fails loudly (exit 1)
   on any missed class, which is what caught this.
2. **ChanOp mutant did not compile (mutator).** Replacing `out <- v*v`
   with a bare `<-out` left the range variable `v` unused → build failure.
   The mutator now emits `{ _ = <expr>; <-ch }`, keeping the sent value
   used; a regression unit test asserts the `_ = v` preservation. (With
   the old vacuous experiment this also produced a *false* DETECTED: both
   builds failed and the build logs "differed" only in temp paths — see 3.)
3. **Build log leaked per-backend temp paths (harness).** The echoed build
   command embedded `{BIN}` under the run-specific workdir, so two
   identical build failures compared as different. `RunProgram` now
   scrubs the workdir to `{WORK}` in the build log; a unit test pins it.

## False-positive record (green)

- Green run inside `validate` + standalone `run`: 45/45 programs SAME,
  0 divergences — including `27_panic_trace` (trace normalized across
  addresses/goroutine ids/line numbers), `28_exit_code` (exit code 3 +
  stderr), `20_fmt_formats` (`%p` output normalized on the stdout path),
  and the concurrency programs (14, 26, 41) under `GOMAXPROCS=1`.
- Rerun control: 45/45 SAME — no harness or corpus flakiness observed.
- Exclusion list: empty. No program needed a reason-coded exclusion.

## Nondeterminism policy (tamed, never silent)

`GOMAXPROCS=1` pinned (never inherited — unit test sets ambient
`GOMAXPROCS=8` and asserts the probe still sees 1); runs in a fresh empty
cwd with scrubbed env (`PATH`, `HOME` + backend overrides); corpus written
deterministic (sorted map output, order-independent goroutine combines, no
wall-clock prints); addresses/goroutine-ids/line-numbers normalized on
both stdout and stderr; timeouts convert hangs to a compared observable.
Full policy in `README.md` and `corpus/README.md`.

## Backend seam (future graalgo plug-in)

Backends are pure config (`backends.json`): `{VAR}`/`{SRC}`/`{BIN}`
command templates + env overrides. Plugging graalgo in = adding one JSON
entry (example in `README.md`) — zero harness code changes. `run` compares
every backend against backends[0]; `validate` compares backends[1] vs
backends[0]. `${DH_GO}` env expansion keeps toolchain paths portable.

## Self-attack notes (adversarial pass on the oracle)

1. **Always-SAME comparator would still pass green** — countered by the
   mutation validation (9 detected divergences) and per-branch comparator
   unit tests. Red-green is load-bearing, not decorative.
2. **Address normalization blinds pointer-identity changes** (a mutant
   swapping which pointer is printed would be MISSED). Accepted and
   documented: a future backend may legitimately lay out the heap
   differently, so pointer identity is not a conformance observable.
   The normalization applies identically to both sides, so the comparison
   stays a valid differential test, just a weaker oracle for that text.
3. **Mutant that fails to build identically on both sides** → SAME →
   MISSED. Guarded by manifest curation: every manifest entry is verified
   to compile under both backends during validation (a non-compiling
   mutant is a manifest bug, surfaced as MISSED → K1).
4. **Line-number normalization** (`file.go:NNN` → `file.go:LINE`) could
   mask a mutant that only shifts panic lines. Our mutators never produce
   line-only changes; accepted residual risk, recorded here.

## Limitations

- Smoke layer only: single-file `package main`, stdlib imports, process
  observables (stdout/stderr/exit/trace). File/network effects, timing,
  `unsafe`/cgo, and GOMAXPROCS>1 scheduling observables are later layers.
- Sequential runs; full CI takes ~10 min, dominated by `go build`.
- `/tmp` on this VM is a 512M tmpfs shared with other agents and fills up;
  `run.sh` sets `TMPDIR=$HOME/workspace/.tmp` (persistent disk) for all
  build/run scratch.

## Deliverables & paths

- Harness: [diffharness](sandbox://workspace/graalgo/tools/diffharness/) —
  [run.sh](sandbox://workspace/graalgo/tools/diffharness/run.sh),
  [README](sandbox://workspace/graalgo/tools/diffharness/README.md),
  [corpus](sandbox://workspace/graalgo/tools/diffharness/corpus/)
- CI reports: `tools/diffharness/test-results/` (`green.json`,
  `validate.json` with the full per-mutation evidence, `checksums.txt`)
- This report: `spikes/07-harness/REPORT.md`

## Suggested follow-ups (not this spike)

- When a graalgo backend exists: add it as `backends[1]` and re-run; grow
  `mutations.txt` (multiple instances per class, more classes: dropped
  `close`, inverted select cases, wrong method receiver).
- Generative corpus (FEASIBILITY.md §7.5) as a later layer.
