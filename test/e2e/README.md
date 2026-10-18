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

[run_module.sh](run_module.sh) was rewritten 2026-10-18 (review lane P1-4)
against the current backend: it invokes
[internal/backend/graalgo-sync-run](../../internal/backend/graalgo-sync-run)
directly — the product launcher (the same `P3SyncMain` backend the
`graalgo` driver routes to) — with `--gmp=1` and `--gmp=4` legs. The
launcher preserves the raw program exit code (panic = 2, like gc), so the
differential stays byte-identical against the gc binary. `make test-e2e`
runs every probe in this directory (currently just [uuid/](uuid/)).

Not wired into CI yet, for two measured reasons:

1. Speed: ~1m40s per case (two JVM legs), ~14 min for the 8 uuid cases —
   too slow for per-push CI. The weekly full-suite job is the natural home.
2. Red: all 8 uuid cases currently FAIL on an open backend gap —
   `graalgo-sync-run: internal error building AST: assign.fields: lhs/rhs
   length mismatch` (triaged as BUG-MODPROBE-008 in [BUGS.md](BUGS.md)).
   Wiring a known-red gate into CI would be noise, not signal.

CI wiring lands when the probe goes green.
