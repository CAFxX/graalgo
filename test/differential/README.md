# diffharness — differential-vs-gc smoke harness

The day-one conformance net for graalgo (FEASIBILITY.md §7, smoke layer):
compile and run the same corpus programs under two backends, compare
observables, report divergences. Validated by differential testing
gc-vs-gc with injected mutations, and by the gc-vs-graalgo runs in CI
(`run.sh`, per-push smoke layer).

## Quick start

```bash
./run.sh            # full CI: build, vet, unit tests, green run, mutation validation
```

`run.sh` exits non-zero on any failure and leaves machine-readable reports
in `./test-results/`. It needs only a Go toolchain: set `DH_GO` to the
toolchain root, or rely on `~/tools/go`, or on `go` in `PATH`.

## Layout

- `cmd/diffharness/` — the harness CLI (`run`, `mutate`, `validate`)
- `internal/harness/` — config, backend runner, normalizer, comparator, corpus
- `internal/mutate/` — AST mutation generator (oracle validation)
- `corpus/` — 49 small Go programs covering the language surface
- `backends.json` — backend declarations, phase-1 frontend scope (the seam)
- `backends-p3.json` — backend declarations, product launcher scope (what
  `run.sh`, the snapshot gate, and per-push CI use); carries the 11
  quarantined exclusions (see `../snapshots/QUARANTINE.md`)
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

`backends.json` (phase-1 frontend scope) and `backends-p3.json` (product
launcher scope; what `run.sh` and per-push CI use):

| config | name | build | run | notes |
|---|---|---|---|---|
| both | `gc-a` | `{GO}/bin/go build -o {BIN} {SRC}` | `{BIN}` | reference backend |
| both | `gc-b` | same as gc-a | `{BIN}` | oracle twin for green/mutation runs |
| `backends.json` | `graalgo` | `{GRAALGO_RUN} --check {SRC}` | `{GRAALGO_RUN} {SRC}` | phase-1 launcher wrapper; `GRAALGO_RUN=${PHASE1_DIR}/../../internal/backend/graalgo-run` |
| `backends-p3.json` | `graalgo` | `{GRAALGO_RUN} --check {SRC}` | `{GRAALGO_RUN} {SRC}` | product launcher (P3SyncMain); `GRAALGO_RUN=${PHASE3_DIR}/graalgo-sync-run` |

`PHASE3_DIR` is exported by `run.sh` (defaults to
`<repo>/internal/backend`); the root Makefile's snapshot targets export
both `PHASE1_DIR` and `PHASE3_DIR` as `<repo>/internal/backend`. For
manual `diffharness` invocations, export them yourself to
`<repo>/internal/backend`. The `phases/phase1` default this paragraph
used to cite no longer exists. `gc-a` stays the reference: `validate`
still compares gc-b against gc-a only.

Current status (2026-10-31): the smoke layer runs the 49-program corpus
under `backends-p3.json` — 38 programs after the 11 quarantined
exclusions (see `../snapshots/QUARANTINE.md`) — gc-a vs gc-b green plus
the graalgo product launcher. The Truffle `sun.misc.Unsafe`
terminal-deprecation stderr warnings are allowlisted per backend via
`stderrFilters` in `backends-p3.json` (the M7 concern from 2026-10-04 is
closed, not pending).

## What is compared

For each program × backend: build the binary, run it in a fresh empty
working directory with a minimal environment (`PATH`, `HOME`, plus the
backend's `env` overrides — `GOMAXPROCS=1` is pinned, never inherited),
under a per-program timeout. Compared: build success + build log, stdout,
stderr, exit code, timeout. Panic/fatal-error traces and printed addresses
are normalized before comparison:

- `0x…` → `0xADDR` (code/data addresses, signal addrs, `%p` output)
- `goroutine N [` → `goroutine # [`, `in goroutine N` → `in goroutine #`
- `file.go:NNN` → `file.go:LINE` (line attribution may shift with inlining)

Known limitation (R1 P2-1): the `0x…` rule is intentionally broad — it also
matches *computed* `0x`-prefixed hex values a program prints (e.g. `%#x`
output). Two backends printing different computed `0x` values would compare
equal, a false "same". Verified not masking anything in the current corpus
(only `%p` outputs carry `0x`; `%x` outputs have no `0x` prefix). Corpus
discipline: programs must not print computed `0x`-prefixed values; if one
ever needs to, narrow the rule first (e.g. address-length or
pointer-context gating).

Deliberately **not** normalized (a difference here is a real divergence):
message text, exit codes, timeouts, build failures.

## Nondeterminism policy

Tamed, never silent (FEASIBILITY.md §7):

1. `GOMAXPROCS=1` pinned for every run; runs get a fresh empty cwd and a
   scrubbed environment.
2. Corpus programs are written to be deterministic: map output is
   key-sorted, goroutine results are combined order-independently
   (sums, WaitGroups), no wall-clock or address values escape
   unnormalized, no `time.Now`/`rand` without fixed seeds.
3. Documented exclusions live in `backends-p3.json` → `exclusions` with a
   **reason code** (per §7 the list may only grow through review):
   `nondeterministic-output`, `backend-specific`, `flaky`, `other:<text>`.
   Currently 11 of 49 programs (38 run); each exclusion carries a dated
   owner lane and the count is locked by the manifest in
   `../snapshots/QUARANTINE.md` (`make snapshot-check` enforces it).
   `backends.json` itself carries no exclusions.
4. The `validate` rerun control (same program twice under the reference
   backend) mechanically guards against flakiness: any divergence there
   fails validation (kill trigger K3).

## Oracle validation (red-green)

`diffharness validate` does three things:

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
- Sequential runs; 49 programs (38 run after exclusions) × backends take
  a few minutes, dominated by the cold `go build` cache.
