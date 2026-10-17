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
- The backend modules built:
  `make -C ../../internal/backend/truffle main` (runtime + truffle classes),
  `make -C ../../internal/backend/interop main` (product-launcher classes)
  — or run the make targets below, which build what they need.
- For the two differential scripts: the `gofrontend` binary built
  (see [internal/frontend/build.sh](../../internal/frontend/build.sh)) and
  stock Go at `$GO` (default `~/tools/go/bin/go`).
- Go programs run through the product launcher
  (`internal/backend/graalgo-sync-run`, P3SyncMain): it drives the same
  `SchedulerCore` under test with channels/select and the full stdlib
  surface. The raw scheduler entry point
  (`graalgo.sched.launcher.SchedMain`) cannot run real Go programs on the
  current base (its Truffle `NodeFactory` lacks the `ifacemethod`/`makechan`
  hooks), so the differential scripts deliberately do not use it.

## Make targets

In [internal/backend/runtime](../../internal/backend/runtime/):

- `make torture` — the battery
- `make gmp-matrix` — the GOMAXPROCS matrix
- `make stress-differential` — the stress corpus differential
