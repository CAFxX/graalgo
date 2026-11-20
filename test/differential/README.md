# diffharness — differential-vs-gc smoke harness

> **Status: shared harness, v1-driven gates.** "The product launcher" below
> is v1's `graalgo-sync-run` ([backends-p3.json](backends-p3.json)); the
> new path's gates are `test/m1*` ([docs/MAP.md](../../docs/MAP.md)). The
> gc snapshot and the exclusion registers carry over to the new path at M3.

The conformance net for graalgo (SPEC §5.1/§5.5): compile and run the same
corpus programs under gc and graalgo, compare observables, report
divergences.

Gates built on it (current):

- `make snapshot-check` (per push, in `make all`): every corpus program
  under the product launcher vs **gc's** committed snapshot
  ([../snapshots/](../snapshots/)); expected divergences are reviewed,
  reason-coded entries in [corpus-exclusions.yaml](corpus-exclusions.yaml)
  and fail as STALE once they match gc.
- `make mutation` (nightly): mutates graalgo itself
  ([../../tools/mutation/](../../tools/mutation/)) and requires the gate
  above to go red — the oracle validation of SPEC §5.7.
- `run_conformance.sh` (layer 2): GOROOT/test via the `srctest` adapter,
  expected divergences in [srctest-exclusions.yaml](srctest-exclusions.yaml).
- `run.sh` (CI): harness self-test on gc-vs-gc only (the `validate`
  corpus-mutation check proves the comparator notices a changed program;
  it says nothing about graalgo).

## Quick start

```bash
./run.sh            # full CI: build, vet, unit tests, green run, mutation validation
```

`run.sh` exits non-zero on any failure and leaves machine-readable reports
in `./.tmp/test-results/` (gitignored). It needs only a Go toolchain: set `DH_GO` to the
toolchain root, or rely on `~/tools/go`, or on `go` in `PATH`.

## Layout

- `cmd/diffharness/` — the harness CLI (`run`, `mutate`, `validate`)
- `internal/harness/` — config, backend runner, normalizer, comparator, corpus
- `internal/mutate/` — AST mutation generator (oracle validation)
- `corpus/` — 45 small Go programs covering the language surface
- `backends.json` — backend declarations (the seam)
- `mutations.txt` — mutation-validation manifest (`CLASS TARGET` lines)
- `testdata/` — fixtures for harness unit tests

## The backend seam (zero-harness-change plug-in)

A backend is a JSON object:

```json
{
  "name": "graalgo-jit",
  "vars": {"GRAALGO": "/opt/graalgo"},
  "build": ["{GRAALGO}/bin/graalgo", "build", "-o", "{BIN}", "{SRC}"],
  "run": ["{BIN}"],
  "env": {"GOMAXPROCS": "1", "GOTOOLCHAIN": "local"}
}
```

`{VAR}` tokens come from `vars` (values undergo `${ENV}` expansion, so
`${DH_GO}` keeps checkouts portable); `{SRC}`/`{BIN}` are substituted per
program by the runner. To plug graalgo in: add one entry to the `backends`
array — no harness code changes. `validate` compares backends[1] against
backends[0] (the reference); `run` compares every backend against the first.

### Registered backends

| name | build | run | notes |
|---|---|---|---|
| `gc-a` | `{GO}/bin/go build -o {BIN} {SRC}` | `{BIN}` | reference backend |
| `gc-b` | same as gc-a | `{BIN}` | oracle twin for green/mutation runs |
| `graalgo` | `{GRAALGO_RUN} --check {SRC}` | `{GRAALGO_RUN} {SRC}` | [backends-p3.json](backends-p3.json): product launcher `${GRAALGO_BACKEND_DIR}/graalgo-sync-run` (P3SyncMain); [backends.json](backends.json): phase-1 `graalgo-run` (`batch: true`, typeparam adapter) |

`GRAALGO_BACKEND_DIR` defaults to `<repo>/internal/backend` in the Makefile and
scripts. JVM backends declare `noiseSets: ["jvm"]`: one harness-owned
definition of JVM stderr noise (Unsafe deprecation and HotSpot VM
warnings). `diffharness run -backends gc-a,gc-b` selects backends.

## What is compared

For each program × backend: build the binary, run it in a fresh empty
working directory with a minimal environment (`PATH`, `HOME`, plus the
backend's `env` overrides — `GOMAXPROCS=1` is pinned, never inherited),
under a per-program timeout. Compared: build success + build log, stdout,
stderr, exit code, timeout. Panic/fatal-error traces and printed addresses
are normalized before comparison — only what is genuinely
nondeterministic ([normalize.go](internal/harness/normalize.go)):

- goroutine ids (`goroutine N [`, `in goroutine N`);
- trace PC offsets (`main.go:12 +0x1d` → `main.go:12`) and register
  annotations (`fp=/sp=/pc=0x…`);
- hex inside trace frame argument lists and on `[signal …]` lines.

Compared exactly: source line numbers in traces, any `0x…` a program
computes and prints, message text, exit codes, timeouts, build outcome.
Stdout is exact unless the program opts in via a `NORMALIZE` file
(`pointers` rule for `%p` output, e.g. `20_fmt_formats`).

## Nondeterminism policy

Tamed, never silent (FEASIBILITY.md §7):

1. `GOMAXPROCS=1` pinned for every run; runs get a fresh empty cwd and a
   scrubbed environment.
2. Corpus programs are written to be deterministic: map output is
   key-sorted, goroutine results are combined order-independently
   (sums, WaitGroups), no wall-clock or address values escape
   unnormalized, no `time.Now`/`rand` without fixed seeds.
3. Expected graalgo-vs-gc divergences live in
   [corpus-exclusions.yaml](corpus-exclusions.yaml) with a SPEC §5.6
   reason code (`backend-specific` / `unspecified-behavior` /
   `deferred-parity`); the list may only grow through review, and listed
   programs still run (STALE once they match gc). Order-unspecified or
   address-printing output is annotated per program (`NORMALIZE` file),
   never normalized globally.
   An entry whose program is a wall-clock check (it times out or fails a
   "too slow" assertion on a slow or loaded host and matches gc on a fast
   one) carries `timing: true`: matching gc is then reported `TIMING` (a
   pass, "passes (timing-dependent)") instead of STALE, while a divergence
   is judged as for any entry (pinned signature, or `unpinned (<why>)`).
   The marker needs a `BUG-NNNN` in the detail and a signature
   (`diffharness registers`), and the number of marked entries is a
   shrink-only ratchet (`timing-excl` in
   [ratchet_budget.json](../../.github/scripts/ratchet_budget.json)).
4. The `validate` rerun control (same program twice under the reference
   backend) mechanically guards against flakiness: any divergence there
   fails validation (kill trigger K3).

## Comparator self-test (gc-vs-gc)

Oracle validation against graalgo is `make mutation` (see above).
`diffharness validate` only self-tests the comparator, and does three things:

1. **Green run** — unmutated corpus, gc-a vs gc-b: expects zero divergences
   (false-positive record).
2. **Rerun control** — each program twice under gc-a: expects zero
   divergences (flakiness check).
3. **Mutation validation** — for each `mutations.txt` entry, the AST
   mutator applies one semantics-changing, still-compiling edit
   (operator swaps, off-by-one, dropped defer, negated condition, changed
   constant, swapped returns, wrong channel op, shrunk slice bound) and the
   harness must report DIVERGE. A MISSED entry fails validation (K1).

Unit tests in `internal/...` are red-green too: every normalization rule
and every comparator branch has a must-change and a must-not-change case.

## Limitations (smoke layer, by design)

- Single-file `package main` programs, stdlib imports only.
- Compares process observables only; sandboxed file/network effects are a
  later layer (FEASIBILITY.md §7 mentions them; not in the smoke net).
- No timing comparison (excluded deliberately).
- Sequential runs; ~45 programs × backends take a few minutes, dominated
  by the cold `go build` cache.
