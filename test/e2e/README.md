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

`run_module.sh` invokes the current product launcher
([internal/backend/graalgo-sync-run](../../internal/backend/graalgo-sync-run),
P3SyncMain) with `--gmp=1` and `--gmp=4` legs, after `make build` at the
repo root. Run the whole probe set with `make e2e` (wired into CI's
full-suite job — weekly + manual dispatch — not per-push: ~10–20 min, and
the module cache needs priming over the network on first run via
`go mod download`). `run_module.sh <probe-dir> [case...]` also runs
standalone; `GR` defaults to the repo root derived from the script's
location. Exit code is non-zero iff any case fails any leg; a hung
backend leg is killed after `GR_TIMEOUT` seconds (default 300) and fails
its case as TIMEOUT.
