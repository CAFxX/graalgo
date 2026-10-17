# Scheduler torture scripts

Three scripts that torture the P2 userspace scheduler on the production
path (spike 2b F1–F6), differential against `gc` where Go source is
involved. Remediation of review findings QN-12/T13 (stale scripts) and
T17 (shell hygiene).

| Script | What it does |
|---|---|
| [run_battery.sh](run_battery.sh) | Runs every `TortureBattery` leg (F1/F3/F4/F5/F5b/F6) with several scheduler configs; exit 0 iff all legs pass |
| [gmp_matrix.sh](gmp_matrix.sh) | GOMAXPROCS × workload matrix (`{1,2,4,N}` × `{compute,mixed}`): each [corpus](corpus/) program runs under graalgo (`--gmp=N`) and under `gc` (`GOMAXPROCS=N`); stdout compared byte-for-byte |
| [stress_differential.sh](stress_differential.sh) | Runs each [corpus](corpus/) program under graalgo and `gc`; `exact` mode compares stdout byte-for-byte, `completion` mode only requires clean completion on both |

## Layout

- Scripts: this directory. Logs: `internal/backend/runtime/out/torture/<name>/`
  (gitignored via `out/` — per the repo [no-debris rule](../../AGENTS.md#5-no-debris-rule),
  run evidence never lands in the tree).
- Corpus: [corpus/](corpus/) (committed). Each program is deterministic by
  construction (order-independent reductions), so `exact` comparisons are
  schedule-independent.

## Prerequisites

- GraalVM at `$GRAALVM_HOME` (default `~/tools/graalvm-jdk-25`).
- The backend runtime module built: `make -C ../../internal/backend/runtime main`
  (or run the make targets below, which build it first).
- For the two differential scripts: the `gofrontend` binary built
  (see [internal/frontend/build.sh](../../internal/frontend/build.sh)) and
  stock Go at `$GO` (default `~/tools/go/bin/go`).

## Make targets

In [internal/backend/runtime](../../internal/backend/runtime/):

- `make torture` — the battery
- `make gmp-matrix` — the GOMAXPROCS matrix
- `make stress-differential` — the stress corpus differential
