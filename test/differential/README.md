# diffharness — differential-vs-gc smoke harness

The day-one conformance net for graalgo (FEASIBILITY.md §7, smoke layer):
compile and run the same corpus programs under two backends, compare
observables, report divergences. Validated *now* by differential testing
gc-vs-gc with injected mutations (graalgo doesn't exist yet).

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
| `graalgo` | `{GRAALGO_RUN} --check {SRC}` | `{GRAALGO_RUN} {SRC}` | graalgo-run launcher; `GRAALGO_RUN=<repo>/internal/backend/graalgo-run` |

`PHASE1_DIR` is the backend dir (`<repo>/internal/backend`, as set by
`run_conformance.sh`); `backends.json` expands it to the launcher as
`${PHASE1_DIR}/../../internal/backend/graalgo-run`. Export it yourself
for manual `diffharness` invocations. `gc-a` stays the
reference: `validate` still compares gc-b against gc-a only.

M2 status (2026-10-04): the frontend path is real — graalgo's `--check`
accepts straight-line programs (exit 0) and rejects bad ones (exit 2 with
gofrontend's message as the build log). Verified: gc-a vs graalgo on
`sl06_multiassign` diverges ONLY on the Truffle `sun.misc.Unsafe`
terminal-deprecation stderr warnings (build, exit code, stdout identical).
Known M7 concern: the HotSpot Truffle runtime prints those warnings to
stderr on every JVM launch; the stderr normalizer will need an allowlist
entry for the graalgo backend before full-corpus differential runs.

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
3. Documented exclusions live in `backends.json` → `exclusions` with a
   **reason code** (per §7 the list may only grow through review):
   `nondeterministic-output`, `backend-specific`, `flaky`, `other:<text>`.
   Currently empty — every corpus program passes unmutated gc-vs-gc.
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
- Sequential runs; ~45 programs × backends take a few minutes, dominated
  by the cold `go build` cache.
