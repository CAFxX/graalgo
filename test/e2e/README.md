# End-to-end: real published Go modules via graalgo

This directory holds end-to-end proofs: real published Go modules compiled
**as-published** (never patched), with output byte-identical to `go run`
(gc 1.27.1).

## Layout

- [uuid/](uuid/) — `github.com/google/uuid` probe cases (moved here from
  `modprobe/uuid/`, 2026-10-14). Each `caseNN/main.go` is a package-main
  program exercising the module; `go.mod` pins the module for the gc leg.
- [run_module.sh](run_module.sh) — differential runner: `go run` (gc) vs
  graalgo, byte-compare stdout and exit codes. Moved here from
  `modprobe/harness/`, 2026-10-14.
- [BUGS.md](BUGS.md) — module-probe bug log (moved here from
  `modprobe/bugs/`, 2026-10-14). Every entry names the general fix or the
  blocking graalgo gap; module sources are never patched.

## Status

`run_module.sh` targets the product backend: the graalgo leg runs each case
under `P3SyncMain` (the [product launcher](../../internal/backend/graalgo-sync-run),
`--gmp=1` and `--gmp=4`), compared against `go run` (gc 1.27.1) for
byte-identical stdout and equal exit codes. Requires `make build` at the
repo root first (the wrapper never builds). This matches the snapshot
backend documented in [test/snapshots/README.md](../snapshots/README.md):
the product launcher, not the phase-1 `graalgo-run`, is the backend of
record for end-to-end behavior.
